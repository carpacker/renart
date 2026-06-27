package pyintelligence

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"

	"renart/internal/web/profiling"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:embed ty_wasi.wasm
var tyWASM embed.FS

type Request struct {
	Root               string         `json:"root"`
	Path               string         `json:"path"`
	Content            string         `json:"content"`
	Options            map[string]any `json:"options,omitempty"`
	Files              []VirtualFile  `json:"files,omitempty"`
	Line               int            `json:"line,omitempty"`
	Column             int            `json:"column,omitempty"`
	Snippets           bool           `json:"snippets,omitempty"`
	CompletionDetails  bool           `json:"completion_details"`
	CompletionDocs     bool           `json:"completion_documentation"`
	SessionID          string         `json:"session_id,omitempty"`
	SessionFingerprint string         `json:"session_fingerprint,omitempty"`
}

type VirtualFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type FormatResponse struct {
	Status string  `json:"status"`
	Result *string `json:"result,omitempty"`
	Error  string  `json:"error,omitempty"`
}

type CheckResponse struct {
	Status      string       `json:"status"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	Error       string       `json:"error,omitempty"`
}

type CompletionResponse struct {
	Status string       `json:"status"`
	Result []Completion `json:"result,omitempty"`
	Error  string       `json:"error,omitempty"`
}

type HoverResponse struct {
	Status string `json:"status"`
	Result *Hover `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

type SignatureHelpResponse struct {
	Status string         `json:"status"`
	Result *SignatureHelp `json:"result,omitempty"`
	Error  string         `json:"error,omitempty"`
}

type GotoDefinitionResponse struct {
	Status string       `json:"status"`
	Result []GotoTarget `json:"result,omitempty"`
	Error  string       `json:"error,omitempty"`
}

type Diagnostic struct {
	ID       string `json:"id"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
	Range    *Range `json:"range,omitempty"`
	Display  string `json:"display,omitempty"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type Completion struct {
	Label               string     `json:"label"`
	Kind                string     `json:"kind,omitempty"`
	Detail              string     `json:"detail,omitempty"`
	InsertText          string     `json:"insert_text,omitempty"`
	InsertTextFormat    string     `json:"insert_text_format"`
	Documentation       string     `json:"documentation,omitempty"`
	ModuleName          string     `json:"module_name,omitempty"`
	AdditionalTextEdits []TextEdit `json:"additional_text_edits,omitempty"`
}

type TextEdit struct {
	Range Range  `json:"range"`
	Text  string `json:"text"`
}

type Hover struct {
	Contents string `json:"contents"`
	Range    *Range `json:"range,omitempty"`
}

type SignatureHelp struct {
	Signatures      []Signature `json:"signatures"`
	ActiveSignature *int        `json:"active_signature,omitempty"`
	ActiveParameter *int        `json:"active_parameter,omitempty"`
}

type Signature struct {
	Label           string               `json:"label"`
	Documentation   string               `json:"documentation,omitempty"`
	Parameters      []SignatureParameter `json:"parameters"`
	ActiveParameter *int                 `json:"active_parameter,omitempty"`
}

type SignatureParameter struct {
	Label         string `json:"label"`
	Name          string `json:"name"`
	Type          string `json:"ty"`
	Documentation string `json:"documentation,omitempty"`
}

type GotoTarget struct {
	Path       string `json:"path"`
	FocusRange Range  `json:"focus_range"`
	FullRange  Range  `json:"full_range"`
}

type runtimeState struct {
	runtime  wazero.Runtime
	compiled wazero.CompiledModule
	err      error
}

var state struct {
	once sync.Once
	data runtimeState
}

var (
	executionMu     sync.Mutex
	tyModule        api.Module // guarded by executionMu; recycled to bound memory
	tyInstanceCount uint64
)

// currentTyModuleLocked returns the shared ty module instance, instantiating it
// on first use (or after a Recycle). The caller must hold executionMu.
func currentTyModuleLocked(ctx context.Context, initialized runtimeState) (api.Module, error) {
	if tyModule != nil {
		return tyModule, nil
	}
	instanceID := atomic.AddUint64(&tyInstanceCount, 1)
	module, err := initialized.runtime.InstantiateModule(ctx, initialized.compiled, wazero.NewModuleConfig().WithName(fmt.Sprintf("ty-%d", instanceID)))
	if err != nil {
		return nil, fmt.Errorf("instantiate ty wasm: %w", err)
	}
	tyModule = module
	return tyModule, nil
}

// Recycle closes the shared ty module so the next call instantiates a fresh one,
// reclaiming the WASM linear memory that only ever grows across calls. Any
// per-session state cached in the module (mounted package stubs) is lost, so
// callers must also reset their session tracking so those files are re-sent.
func Recycle() {
	executionMu.Lock()
	defer executionMu.Unlock()
	if tyModule != nil {
		_ = tyModule.Close(context.Background())
		tyModule = nil
	}
}

func Format(ctx context.Context, req Request) (FormatResponse, error) {
	var resp FormatResponse
	if err := call(ctx, "ty_format_python", req, &resp); err != nil {
		return FormatResponse{}, err
	}
	return resp, nil
}

func Check(ctx context.Context, req Request) (CheckResponse, error) {
	var resp CheckResponse
	if err := call(ctx, "ty_check_python", req, &resp); err != nil {
		return CheckResponse{}, err
	}
	return resp, nil
}

func Complete(ctx context.Context, req Request) (CompletionResponse, error) {
	var resp CompletionResponse
	if err := call(ctx, "ty_complete_python", req, &resp); err != nil {
		return CompletionResponse{}, err
	}
	return resp, nil
}

func HoverAt(ctx context.Context, req Request) (HoverResponse, error) {
	var resp HoverResponse
	if err := call(ctx, "ty_hover_python", req, &resp); err != nil {
		return HoverResponse{}, err
	}
	return resp, nil
}

func SignatureHelpAt(ctx context.Context, req Request) (SignatureHelpResponse, error) {
	var resp SignatureHelpResponse
	if err := call(ctx, "ty_signature_help_python", req, &resp); err != nil {
		return SignatureHelpResponse{}, err
	}
	return resp, nil
}

func GotoDefinition(ctx context.Context, req Request) (GotoDefinitionResponse, error) {
	var resp GotoDefinitionResponse
	if err := call(ctx, "ty_goto_definition_python", req, &resp); err != nil {
		return GotoDefinitionResponse{}, err
	}
	return resp, nil
}

func call(ctx context.Context, functionName string, req Request, response any) error {
	trace := profiling.New("RENART_PYINTELLIGENCE_PROFILE", "ty."+functionName)
	requestBytes := 0
	responseBytes := 0
	defer func() {
		trace.Done(
			"request_bytes="+strconv.Itoa(requestBytes),
			"response_bytes="+strconv.Itoa(responseBytes),
			"files="+strconv.Itoa(len(req.Files)),
		)
	}()

	initialized := initRuntime(ctx)
	trace.Step("init_runtime")
	if initialized.err != nil {
		return initialized.err
	}

	executionMu.Lock()
	trace.Step("lock_wait")
	defer executionMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	execCtx := context.WithoutCancel(ctx)

	// Reuse a long-lived module instance: ty caches per-session state (mounted
	// package stubs) inside the module across calls, so each call cannot get a
	// fresh instance. Its WASM linear memory only grows, so callers recycle it
	// periodically via Recycle() to bound memory (see the service's session
	// reset). Guarded by executionMu, held for the whole call.
	module, err := currentTyModuleLocked(execCtx, initialized)
	if err != nil {
		return err
	}

	memory := module.Memory()
	alloc := module.ExportedFunction("ty_alloc")
	dealloc := module.ExportedFunction("ty_dealloc")
	freeResult := module.ExportedFunction("ty_result_free")
	fn := module.ExportedFunction(functionName)
	if memory == nil || alloc == nil || dealloc == nil || freeResult == nil || fn == nil {
		return fmt.Errorf("ty wasm missing required exports")
	}

	body, err := json.Marshal(req)
	requestBytes = len(body)
	trace.Step("marshal")
	if err != nil {
		return fmt.Errorf("marshal ty request: %w", err)
	}
	ptr, length, err := writeRequest(execCtx, memory, alloc, body)
	trace.Step("write_request")
	if err != nil {
		return err
	}
	defer dealloc.Call(execCtx, ptr, length)

	result, err := fn.Call(execCtx, ptr, length)
	trace.Step("wasm_call")
	if err != nil {
		return fmt.Errorf("call ty wasm %s: %w", functionName, err)
	}
	if len(result) != 1 {
		return fmt.Errorf("ty wasm %s returned %d values", functionName, len(result))
	}
	packed := result[0]
	defer freeResult.Call(execCtx, packed)

	bytes, err := readPacked(memory, packed)
	responseBytes = len(bytes)
	trace.Step("read_response")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(bytes, response); err != nil {
		trace.Step("unmarshal")
		return fmt.Errorf("unmarshal ty response: %w", err)
	}
	trace.Step("unmarshal")
	return nil
}

var (
	tyCacheOnce sync.Once
	tyCache     wazero.CompilationCache
)

// tyRuntimeConfig returns a runtime config backed by a shared on-disk
// compilation cache, so compiling the 18MB ty wasm with the optimizing compiler
// is paid once per machine instead of once per process.
func tyRuntimeConfig() wazero.RuntimeConfig {
	config := wazero.NewRuntimeConfig()
	tyCacheOnce.Do(func() {
		base, err := os.UserCacheDir()
		if err != nil || base == "" {
			base = os.TempDir()
		}
		dir := filepath.Join(base, "renart", "wazero-cache")
		if mkErr := os.MkdirAll(dir, 0o755); mkErr == nil {
			if cache, cacheErr := wazero.NewCompilationCacheWithDir(dir); cacheErr == nil {
				tyCache = cache
			}
		}
	})
	if tyCache != nil {
		config = config.WithCompilationCache(tyCache)
	}
	return config
}

func initRuntime(ctx context.Context) runtimeState {
	state.once.Do(func() {
		runtime := wazero.NewRuntimeWithConfig(ctx, tyRuntimeConfig())
		if _, err := wasi_snapshot_preview1.Instantiate(ctx, runtime); err != nil {
			state.data = runtimeState{err: fmt.Errorf("instantiate WASI: %w", err)}
			_ = runtime.Close(ctx)
			return
		}
		wasm, err := tyWASM.ReadFile("ty_wasi.wasm")
		if err != nil {
			state.data = runtimeState{err: fmt.Errorf("read ty wasm: %w", err)}
			_ = runtime.Close(ctx)
			return
		}
		compiled, err := runtime.CompileModule(ctx, wasm)
		if err != nil {
			state.data = runtimeState{err: fmt.Errorf("compile ty wasm: %w", err)}
			_ = runtime.Close(ctx)
			return
		}
		// Modules are instantiated per call (and closed) to bound linear-memory
		// growth; the runtime and compiled module are the shared, reused state.
		state.data = runtimeState{runtime: runtime, compiled: compiled}
	})
	return state.data
}

func writeRequest(ctx context.Context, memory wazeroMemory, alloc wazeroFunction, body []byte) (uint64, uint64, error) {
	result, err := alloc.Call(ctx, uint64(len(body)))
	if err != nil {
		return 0, 0, fmt.Errorf("allocate ty request: %w", err)
	}
	if len(result) != 1 {
		return 0, 0, fmt.Errorf("ty_alloc returned %d values", len(result))
	}
	ptr := uint32(result[0])
	if !memory.Write(ptr, body) {
		return 0, 0, fmt.Errorf("write ty request to wasm memory")
	}
	return uint64(ptr), uint64(len(body)), nil
}

func readPacked(memory wazeroMemory, packed uint64) ([]byte, error) {
	ptr := uint32(packed >> 32)
	length := uint32(packed & 0xffff_ffff)
	bytes, ok := memory.Read(ptr, length)
	if !ok {
		return nil, fmt.Errorf("read ty response from wasm memory")
	}
	return bytes, nil
}

type wazeroMemory interface {
	Write(offset uint32, v []byte) bool
	Read(offset uint32, byteCount uint32) ([]byte, bool)
}

type wazeroFunction interface {
	Call(context.Context, ...uint64) ([]uint64, error)
}
