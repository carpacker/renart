#![expect(unsafe_code, reason = "raw WASM ABI uses linear-memory pointers")]

use std::any::Any;
use std::cell::RefCell;
use std::collections::HashMap;
use std::slice;
use std::sync::Once;

use ruff_diagnostics::Edit;
use ruff_db::diagnostic::{self, DisplayDiagnosticConfig};
use ruff_db::files::{File, FileRange, system_path_to_file};
use ruff_db::source::{line_index, source_text};
use ruff_db::system::walk_directory::WalkDirectoryBuilder;
use ruff_db::system::{
    CaseSensitivity, DirectoryEntry, MemoryFileSystem, Metadata, System, SystemPath, SystemPathBuf,
    SystemVirtualPath, WhichError, WhichResult, WritableSystem,
};
use ruff_notebook::Notebook;
use ruff_python_formatter::formatted_file;
use ruff_source_file::{LineIndex, OneIndexed, PositionEncoding, SourceLocation};
use ruff_text_size::{Ranged, TextSize};
use serde::{Deserialize, Serialize};
use ty_ide::{
    CompletionCapabilities, CompletionInsertTextFormat, CompletionKind, CompletionSettings,
    MarkupKind, NavigationTarget, completion, goto_definition, hover, signature_help,
};
use ty_project::metadata::options::Options;
use ty_project::metadata::value::ValueSource;
use ty_project::watch::{ChangeEvent, CreatedKind};
use ty_project::{CheckMode, Db, ProjectDatabase, ProjectMetadata};
use ty_python_core::program::FallibleStrategy;

static INIT: Once = Once::new();

thread_local! {
    static SESSIONS: RefCell<HashMap<String, WorkspaceSession>> = RefCell::new(HashMap::new());
}

#[derive(Debug, Deserialize)]
struct PythonRequest {
    #[serde(default = "default_root")]
    root: String,
    #[serde(default = "default_path")]
    path: String,
    content: String,
    #[serde(default)]
    options: serde_json::Value,
    #[serde(default)]
    files: Vec<VirtualFile>,
    #[serde(default)]
    line: usize,
    #[serde(default)]
    column: usize,
    #[serde(default)]
    snippets: bool,
    #[serde(default)]
    completion_details: bool,
    #[serde(default)]
    completion_documentation: bool,
    #[serde(default)]
    session_id: String,
    #[serde(default)]
    session_fingerprint: String,
}

#[derive(Debug, Deserialize)]
struct VirtualFile {
    path: String,
    content: String,
}

#[derive(Debug, Serialize)]
struct TyResponse<T> {
    status: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    result: Option<T>,
    #[serde(skip_serializing_if = "Option::is_none")]
    diagnostics: Option<Vec<TyDiagnostic>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<String>,
}

#[derive(Debug, Serialize)]
struct TyDiagnostic {
    id: String,
    message: String,
    severity: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    range: Option<TyRange>,
    display: String,
}

#[derive(Debug, Serialize)]
struct TyCompletion {
    label: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    kind: Option<&'static str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    detail: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    insert_text: Option<String>,
    insert_text_format: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    documentation: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    module_name: Option<String>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    additional_text_edits: Vec<TyTextEdit>,
}

#[derive(Debug, Serialize)]
struct TyTextEdit {
    range: TyRange,
    text: String,
}

#[derive(Debug, Serialize)]
struct TyHover {
    contents: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    range: Option<TyRange>,
}

#[derive(Debug, Serialize)]
struct TySignatureHelp {
    signatures: Vec<TySignature>,
    #[serde(skip_serializing_if = "Option::is_none")]
    active_signature: Option<usize>,
    #[serde(skip_serializing_if = "Option::is_none")]
    active_parameter: Option<usize>,
}

#[derive(Debug, Serialize)]
struct TySignature {
    label: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    documentation: Option<String>,
    parameters: Vec<TySignatureParameter>,
    #[serde(skip_serializing_if = "Option::is_none")]
    active_parameter: Option<usize>,
}

#[derive(Debug, Serialize)]
struct TySignatureParameter {
    label: String,
    name: String,
    ty: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    documentation: Option<String>,
}

#[derive(Debug, Serialize)]
struct TyGotoTarget {
    path: String,
    focus_range: TyRange,
    full_range: TyRange,
}

#[derive(Debug, Serialize)]
struct TyRange {
    start: TyPosition,
    end: TyPosition,
}

#[derive(Debug, Serialize)]
struct TyPosition {
    line: usize,
    column: usize,
}

struct Workspace {
    db: ProjectDatabase,
    system: WasmSystem,
}

struct FileHandle {
    file: File,
}

struct WorkspaceSession {
    fingerprint: String,
    path: String,
    workspace: Workspace,
    file: FileHandle,
}

#[unsafe(no_mangle)]
pub extern "C" fn ty_alloc(len: usize) -> *mut u8 {
    let mut buffer = Vec::<u8>::with_capacity(len);
    let ptr = buffer.as_mut_ptr();
    std::mem::forget(buffer);
    ptr
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn ty_dealloc(ptr: *mut u8, len: usize) {
    if !ptr.is_null() {
        drop(unsafe { Vec::from_raw_parts(ptr, 0, len) });
    }
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn ty_result_free(packed: u64) {
    let (ptr, len) = unpack_result(packed);
    unsafe { ty_dealloc(ptr, len) };
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn ty_format_python(ptr: *const u8, len: usize) -> u64 {
    run_json(ptr, len, |request| {
        let mut workspace = Workspace::new(&request.root, request.options, request.files)?;
        let file = workspace.open_file(&request.path, &request.content)?;
        let formatted = workspace.format(&file)?;
        Ok(TyResponse {
            status: "ok",
            result: Some(formatted),
            diagnostics: None,
            error: None,
        })
    })
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn ty_check_python(ptr: *const u8, len: usize) -> u64 {
    run_json(ptr, len, |request| {
        let diagnostics = with_workspace(request, |workspace, file| workspace.check_file(file))?;
        Ok(TyResponse::<()> {
            status: "ok",
            result: None,
            diagnostics: Some(diagnostics),
            error: None,
        })
    })
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn ty_complete_python(ptr: *const u8, len: usize) -> u64 {
    run_json(ptr, len, |request| {
        let line = request.line;
        let column = request.column;
        let snippets = request.snippets;
        let completion_details = request.completion_details;
        let completion_documentation = request.completion_documentation;
        let completions = with_workspace(request, |workspace, file| {
            workspace.complete(
                file,
                line,
                column,
                snippets,
                completion_details,
                completion_documentation,
            )
        })?;
        Ok(TyResponse {
            status: "ok",
            result: Some(completions),
            diagnostics: None,
            error: None,
        })
    })
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn ty_hover_python(ptr: *const u8, len: usize) -> u64 {
    run_json(ptr, len, |request| {
        let line = request.line;
        let column = request.column;
        let hover = with_workspace(request, |workspace, file| workspace.hover(file, line, column))?;
        Ok(TyResponse {
            status: "ok",
            result: hover,
            diagnostics: None,
            error: None,
        })
    })
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn ty_signature_help_python(ptr: *const u8, len: usize) -> u64 {
    run_json(ptr, len, |request| {
        let line = request.line;
        let column = request.column;
        let signature_help = with_workspace(request, |workspace, file| {
            workspace.signature_help(file, line, column)
        })?;
        Ok(TyResponse {
            status: "ok",
            result: signature_help,
            diagnostics: None,
            error: None,
        })
    })
}

#[unsafe(no_mangle)]
pub unsafe extern "C" fn ty_goto_definition_python(ptr: *const u8, len: usize) -> u64 {
    run_json(ptr, len, |request| {
        let line = request.line;
        let column = request.column;
        let targets = with_workspace(request, |workspace, file| {
            workspace.goto_definition(file, line, column)
        })?;
        Ok(TyResponse {
            status: "ok",
            result: Some(targets),
            diagnostics: None,
            error: None,
        })
    })
}

fn with_workspace<T, F>(request: PythonRequest, f: F) -> Result<T, String>
where
    F: FnOnce(&mut Workspace, &FileHandle) -> Result<T, String>,
{
    if request.session_id.is_empty() || request.session_fingerprint.is_empty() {
        let mut workspace = Workspace::new(&request.root, request.options, request.files)?;
        let file = workspace.open_file(&request.path, &request.content)?;
        return f(&mut workspace, &file);
    }

    SESSIONS.with(|sessions| {
        let mut sessions = sessions.borrow_mut();
        let recreate = sessions
            .get(&request.session_id)
            .is_none_or(|session| {
                session.fingerprint != request.session_fingerprint || session.path != request.path
            });
        if recreate {
            let mut workspace = Workspace::new(&request.root, request.options, request.files)?;
            let file = workspace.open_file(&request.path, &request.content)?;
            sessions.insert(
                request.session_id.clone(),
                WorkspaceSession {
                    fingerprint: request.session_fingerprint.clone(),
                    path: request.path.clone(),
                    workspace,
                    file,
                },
            );
        } else {
            let session = sessions
                .get_mut(&request.session_id)
                .expect("session exists after recreate check");
            session
                .workspace
                .update_open_file(&request.path, &request.content)?;
        }

        let session = sessions
            .get_mut(&request.session_id)
            .expect("session exists before callback");
        f(&mut session.workspace, &session.file)
    })
}

fn run_json<T, F>(ptr: *const u8, len: usize, f: F) -> u64
where
    T: Serialize,
    F: FnOnce(PythonRequest) -> Result<TyResponse<T>, String>,
{
    init();
    let response = unsafe { read_request(ptr, len) }
        .and_then(f)
        .unwrap_or_else(|error| TyResponse {
            status: "error",
            result: None,
            diagnostics: None,
            error: Some(error),
        });
    write_result(&serde_json::to_vec(&response).expect("response serialization to succeed"))
}

unsafe fn read_request(ptr: *const u8, len: usize) -> Result<PythonRequest, String> {
    if ptr.is_null() {
        return Err("request pointer is null".to_string());
    }
    let bytes = unsafe { slice::from_raw_parts(ptr, len) };
    serde_json::from_slice(bytes).map_err(|error| error.to_string())
}

fn write_result(bytes: &[u8]) -> u64 {
    let mut output = bytes.to_vec();
    let len = output.len();
    let ptr = output.as_mut_ptr();
    std::mem::forget(output);
    pack_result(ptr, len)
}

fn pack_result(ptr: *mut u8, len: usize) -> u64 {
    ((ptr as u64) << 32) | (len as u64)
}

fn unpack_result(packed: u64) -> (*mut u8, usize) {
    ((packed >> 32) as *mut u8, (packed & 0xffff_ffff) as usize)
}

fn init() {
    INIT.call_once(|| {
        before_main();
        let _ = ruff_db::set_program_version("ty-wasi-probe".to_string());
    });
}

#[cfg(target_family = "wasm")]
fn before_main() {
    unsafe extern "C" {
        fn __wasm_call_ctors();
    }
    unsafe { __wasm_call_ctors() };
}

#[cfg(not(target_family = "wasm"))]
fn before_main() {}

fn default_root() -> String {
    "/".to_string()
}

fn default_path() -> String {
    "/asset.py".to_string()
}

impl Workspace {
    fn new(root: &str, options: serde_json::Value, files: Vec<VirtualFile>) -> Result<Self, String> {
        let options = Options::deserialize_with(ValueSource::Cli, options)
            .map_err(|error| error.to_string())?;
        let root = SystemPath::new(root);
        let system = WasmSystem::new(root);
        for file in files {
            let path = SystemPath::absolute(&file.path, root);
            system
                .fs
                .write_file_all(&path, file.content)
                .map_err(|error| error.to_string())?;
        }
        let project = ProjectMetadata::from_options(
            options,
            SystemPathBuf::from(root.as_str()),
            None,
            &FallibleStrategy,
        )
        .map_err(|error| error.to_string())?;
        let mut db = ProjectDatabase::fallible(project, system.clone()).map_err(|error| error.to_string())?;
        db.set_check_mode(CheckMode::OpenFiles);
        Ok(Self { db, system })
    }

    fn open_file(&mut self, path: &str, contents: &str) -> Result<FileHandle, String> {
        let path = SystemPath::absolute(path, self.db.project().root(&self.db));
        self.system
            .fs
            .write_file_all(&path, contents)
            .map_err(|error| error.to_string())?;
        self.db.apply_changes(
            &[ChangeEvent::Created {
                path: path.clone(),
                kind: CreatedKind::File,
            }],
            None,
        );
        let file = system_path_to_file(&self.db, &path).map_err(|error| error.to_string())?;
        self.db.project().open_file(&mut self.db, file);
        Ok(FileHandle { file })
    }

    fn update_open_file(&mut self, path: &str, contents: &str) -> Result<(), String> {
        let path = SystemPath::absolute(path, self.db.project().root(&self.db));
        self.system
            .fs
            .write_file_all(&path, contents)
            .map_err(|error| error.to_string())?;
        self.db
            .apply_changes(&[ChangeEvent::file_content_changed(path)], None);
        Ok(())
    }

    fn format(&self, file: &FileHandle) -> Result<Option<String>, String> {
        formatted_file(&self.db, file.file).map_err(|error| error.to_string())
    }

    fn check_file(&self, file: &FileHandle) -> Result<Vec<TyDiagnostic>, String> {
        let config = DisplayDiagnosticConfig::new("ty").color(false);
        Ok(self
            .db
            .check_file(file.file)
            .into_iter()
            .map(|diagnostic| diagnostic_to_json(&self.db, file, diagnostic, &config))
            .collect())
    }

    fn complete(
        &self,
        file: &FileHandle,
        line: usize,
        column: usize,
        snippets: bool,
        completion_details: bool,
        completion_documentation: bool,
    ) -> Result<Vec<TyCompletion>, String> {
        let source = source_text(&self.db, file.file);
        let index = line_index(&self.db, file.file);
        let line = OneIndexed::new(line).unwrap_or(OneIndexed::MIN);
        let column = OneIndexed::new(column).unwrap_or(OneIndexed::MIN);
        let offset = index.offset(
            SourceLocation {
                line,
                character_offset: column,
            },
            &source,
            PositionEncoding::Utf16,
        );
        let settings = CompletionSettings::default();
        let capabilities = CompletionCapabilities::default().snippets(snippets);
        Ok(completion(&self.db, &settings, capabilities, file.file, offset)
            .into_iter()
            .map(|item| {
                completion_to_json(
                    &self.db,
                    file,
                    item,
                    completion_details,
                    completion_documentation,
                )
            })
            .collect())
    }

    fn hover(&self, file: &FileHandle, line: usize, column: usize) -> Result<Option<TyHover>, String> {
        let offset = self.offset(file, line, column);
        Ok(hover(&self.db, file.file, offset).map(|item| TyHover {
            contents: item.display(&self.db, MarkupKind::Markdown).to_string(),
            range: Some(range_from_file_range(&self.db, item.file_range())),
        }))
    }

    fn signature_help(
        &self,
        file: &FileHandle,
        line: usize,
        column: usize,
    ) -> Result<Option<TySignatureHelp>, String> {
        let offset = self.offset(file, line, column);
        Ok(signature_help(&self.db, file.file, offset).map(|help| {
            let active_parameter = help
                .active_signature
                .and_then(|index| help.signatures.get(index))
                .and_then(|signature| signature.active_parameter);
            TySignatureHelp {
                signatures: help
                    .signatures
                    .into_iter()
                    .map(|signature| TySignature {
                        label: signature.label,
                        documentation: signature.documentation.map(|doc| doc.render_markdown()),
                        parameters: signature
                            .parameters
                            .into_iter()
                            .map(|parameter| TySignatureParameter {
                                label: parameter.label,
                                name: parameter.name,
                                ty: parameter.ty.display(&self.db).to_string(),
                                documentation: parameter.documentation,
                            })
                            .collect(),
                        active_parameter: signature.active_parameter,
                    })
                    .collect(),
                active_signature: help.active_signature,
                active_parameter,
            }
        }))
    }

    fn goto_definition(
        &self,
        file: &FileHandle,
        line: usize,
        column: usize,
    ) -> Result<Vec<TyGotoTarget>, String> {
        let offset = self.offset(file, line, column);
        let Some(targets) = goto_definition(&self.db, file.file, offset) else {
            return Ok(Vec::new());
        };
        Ok(targets
            .into_iter()
            .map(|target| goto_target_to_json(&self.db, target))
            .collect())
    }

    fn offset(&self, file: &FileHandle, line: usize, column: usize) -> TextSize {
        let source = source_text(&self.db, file.file);
        let index = line_index(&self.db, file.file);
        let line = OneIndexed::new(line).unwrap_or(OneIndexed::MIN);
        let column = OneIndexed::new(column).unwrap_or(OneIndexed::MIN);
        index.offset(
            SourceLocation {
                line,
                character_offset: column,
            },
            &source,
            PositionEncoding::Utf16,
        )
    }
}

fn goto_target_to_json(db: &ProjectDatabase, target: NavigationTarget) -> TyGotoTarget {
    TyGotoTarget {
        path: target.file().path(db).as_str().to_string(),
        focus_range: range_from_file_range(db, FileRange::new(target.file(), target.focus_range())),
        full_range: range_from_file_range(db, FileRange::new(target.file(), target.full_range())),
    }
}

fn completion_to_json(
    db: &ProjectDatabase,
    file: &FileHandle,
    completion: ty_ide::Completion,
    include_detail: bool,
    include_documentation: bool,
) -> TyCompletion {
    let detail = include_detail.then(|| completion.ty.map(|ty| ty.display(db).to_string())).flatten();
    let additional_text_edits = completion
        .import
        .as_ref()
        .map(|edit| vec![text_edit_to_json(db, file, edit)])
        .unwrap_or_default();
    TyCompletion {
        label: completion.label.to_string(),
        kind: completion.kind.map(completion_kind_name),
        detail,
        insert_text: completion.insert.map(String::from),
        insert_text_format: match completion.insert_text_format {
            CompletionInsertTextFormat::PlainText => "plaintext",
            CompletionInsertTextFormat::Snippet => "snippet",
        },
        documentation: include_documentation
            .then(|| completion.documentation.map(|doc| doc.render_markdown()))
            .flatten(),
        module_name: completion.module_name.map(ToString::to_string),
        additional_text_edits,
    }
}

fn text_edit_to_json(db: &ProjectDatabase, file: &FileHandle, edit: &Edit) -> TyTextEdit {
    TyTextEdit {
        range: range_from_file_range(db, FileRange::new(file.file, edit.range())),
        text: edit.content().map(ToString::to_string).unwrap_or_default(),
    }
}

fn completion_kind_name(kind: CompletionKind) -> &'static str {
    match kind {
        CompletionKind::Text => "text",
        CompletionKind::Method => "method",
        CompletionKind::Function => "function",
        CompletionKind::Constructor => "constructor",
        CompletionKind::Field => "field",
        CompletionKind::Variable => "variable",
        CompletionKind::Class => "class",
        CompletionKind::Interface => "interface",
        CompletionKind::Module => "module",
        CompletionKind::Property => "property",
        CompletionKind::Unit => "unit",
        CompletionKind::Value => "value",
        CompletionKind::Enum => "enum",
        CompletionKind::Keyword => "keyword",
        CompletionKind::Snippet => "snippet",
        CompletionKind::Color => "color",
        CompletionKind::File => "file",
        CompletionKind::Reference => "reference",
        CompletionKind::Folder => "folder",
        CompletionKind::EnumMember => "enum-member",
        CompletionKind::Constant => "constant",
        CompletionKind::Struct => "struct",
        CompletionKind::Event => "event",
        CompletionKind::Operator => "operator",
        CompletionKind::TypeParameter => "type-parameter",
    }
}

fn diagnostic_to_json(
    db: &ProjectDatabase,
    file: &FileHandle,
    diagnostic: diagnostic::Diagnostic,
    config: &DisplayDiagnosticConfig,
) -> TyDiagnostic {
    let range = diagnostic.primary_span().and_then(|span| {
        let text_range = span.range()?;
        let span_file = span.expect_ty_file();
        Some(range_from_file_range(db, FileRange::new(span_file, text_range)))
    });
    TyDiagnostic {
        id: diagnostic.id().to_string(),
        message: diagnostic.concise_message().to_string(),
        severity: severity_name(diagnostic.severity()),
        range: range.or_else(|| Some(range_from_file_range(db, FileRange::new(file.file, ruff_text_size::TextRange::default())))),
        display: diagnostic.display(db, config).to_string(),
    }
}

fn severity_name(severity: diagnostic::Severity) -> &'static str {
    match severity {
        diagnostic::Severity::Info => "info",
        diagnostic::Severity::Warning => "warning",
        diagnostic::Severity::Error => "error",
        diagnostic::Severity::Fatal => "fatal",
    }
}

fn range_from_file_range(db: &ProjectDatabase, file_range: FileRange) -> TyRange {
    let index = line_index(db, file_range.file());
    let source = source_text(db, file_range.file());
    range_from_text_range(file_range.range(), &index, &source)
}

fn range_from_text_range(text_range: ruff_text_size::TextRange, line_index: &LineIndex, source: &str) -> TyRange {
    TyRange {
        start: position_from_text_size(text_range.start(), line_index, source),
        end: position_from_text_size(text_range.end(), line_index, source),
    }
}

fn position_from_text_size(text_size: TextSize, index: &LineIndex, source: &str) -> TyPosition {
    let location = index.source_location(text_size, source, PositionEncoding::Utf16);
    TyPosition {
        line: location.line.get(),
        column: location.character_offset.get(),
    }
}

#[derive(Debug, Clone)]
struct WasmSystem {
    fs: MemoryFileSystem,
}

impl WasmSystem {
    fn new(root: &SystemPath) -> Self {
        Self {
            fs: MemoryFileSystem::with_current_directory(root),
        }
    }
}

impl System for WasmSystem {
    fn path_metadata(&self, path: &SystemPath) -> ruff_db::system::Result<Metadata> {
        self.fs.metadata(path)
    }

    fn canonicalize_path(&self, path: &SystemPath) -> ruff_db::system::Result<SystemPathBuf> {
        self.fs.canonicalize(path)
    }

    fn read_to_string(&self, path: &SystemPath) -> ruff_db::system::Result<String> {
        self.fs.read_to_string(path)
    }

    fn read_to_notebook(&self, path: &SystemPath) -> Result<Notebook, ruff_notebook::NotebookError> {
        let content = self.read_to_string(path)?;
        Notebook::from_source_code(&content)
    }

    fn read_virtual_path_to_string(&self, _path: &SystemVirtualPath) -> ruff_db::system::Result<String> {
        Err(not_found())
    }

    fn read_virtual_path_to_notebook(&self, _path: &SystemVirtualPath) -> Result<Notebook, ruff_notebook::NotebookError> {
        Err(ruff_notebook::NotebookError::Io(not_found()))
    }

    fn path_exists_case_sensitive(&self, path: &SystemPath, _prefix: &SystemPath) -> bool {
        self.path_exists(path)
    }

    fn case_sensitivity(&self) -> CaseSensitivity {
        CaseSensitivity::CaseSensitive
    }

    fn which(&self, _name: &str) -> WhichResult {
        Err(WhichError::CannotFindBinaryPath)
    }

    fn current_directory(&self) -> &SystemPath {
        self.fs.current_directory()
    }

    fn user_config_directory(&self) -> Option<SystemPathBuf> {
        None
    }

    fn cache_dir(&self) -> Option<SystemPathBuf> {
        None
    }

    fn read_directory<'a>(&'a self, path: &SystemPath) -> ruff_db::system::Result<Box<dyn Iterator<Item = ruff_db::system::Result<DirectoryEntry>> + 'a>> {
        Ok(Box::new(self.fs.read_directory(path)?))
    }

    fn walk_directory(&self, path: &SystemPath) -> WalkDirectoryBuilder {
        self.fs.walk_directory(path)
    }

    fn as_writable(&self) -> Option<&dyn WritableSystem> {
        None
    }

    fn as_any(&self) -> &dyn Any {
        self
    }

    fn as_any_mut(&mut self) -> &mut dyn Any {
        self
    }

    fn dyn_clone(&self) -> Box<dyn System> {
        Box::new(self.clone())
    }
}

fn not_found() -> std::io::Error {
    std::io::Error::new(std::io::ErrorKind::NotFound, "No such file or directory")
}
