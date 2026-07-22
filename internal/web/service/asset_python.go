package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/afero"

	"renart/internal/pysdk"
	"renart/internal/web/profiling"
	"renart/internal/web/pyintelligence"
)

func (s *AssetService) FormatPython(ctx context.Context, assetID string, req FormatPythonAssetRequest) (FormatPythonAssetResponse, *APIError) {
	relAssetPath, absAssetPath, err := s.resolvePythonAssetPath(assetID)
	if err != nil {
		return FormatPythonAssetResponse{}, err
	}
	fs := s.fs()
	originalBytes, readErr := afero.ReadFile(fs, absAssetPath)
	if readErr != nil {
		return FormatPythonAssetResponse{}, newAPIError(500, "asset_read_failed", readErr.Error())
	}

	content := req.Content
	if strings.TrimSpace(content) == "" {
		content = string(originalBytes)
	}
	formatted, formatErr := pyintelligence.Format(ctx, pyintelligence.Request{
		Root:    "/",
		Path:    "/" + strings.TrimPrefix(relAssetPath, "/"),
		Content: ExtractExecutableContent(content),
		Options: defaultTyOptions(),
	})
	if formatErr != nil {
		return FormatPythonAssetResponse{}, newAPIError(500, "python_format_failed", formatErr.Error())
	}
	if formatted.Status != "ok" {
		return FormatPythonAssetResponse{Status: "error", AssetID: assetID, Content: content, Error: formatted.Error}, nil
	}
	formattedContent := ""
	if formatted.Result != nil {
		formattedContent = *formatted.Result
	} else {
		formattedContent = ExtractExecutableContent(content)
	}
	mergedContent := MergeExecutableContent(string(originalBytes), formattedContent)
	if writeErr := afero.WriteFile(fs, absAssetPath, []byte(mergedContent), 0o644); writeErr != nil {
		return FormatPythonAssetResponse{}, newAPIError(500, "asset_write_failed", writeErr.Error())
	}
	s.deps.SuppressWatcher(relAssetPath)
	s.deps.PushWorkspaceUpdateImmediateWithChangedIDs(ctx, "asset.updated", relAssetPath, []string{assetID})
	return FormatPythonAssetResponse{Status: "ok", AssetID: assetID, Content: formattedContent}, nil
}

func (s *AssetService) PythonDiagnostics(ctx context.Context, assetID string, req PythonDiagnosticsRequest) (PythonDiagnosticsResponse, *APIError) {
	trace := profiling.New("RENART_PYINTELLIGENCE_PROFILE", "python.diagnostics")
	profileAssetPath := ""
	packageFileCount := 0
	sentFileCount := 0
	completionCount := 0
	defer func() {
		trace.Done(
			"asset="+profileAssetPath,
			"package_files="+strconv.Itoa(packageFileCount),
			"sent_files="+strconv.Itoa(sentFileCount),
			"completions="+strconv.Itoa(completionCount),
		)
	}()

	relAssetPath, absAssetPath, err := s.resolvePythonAssetPath(assetID)
	trace.Step("resolve")
	if err != nil {
		return PythonDiagnosticsResponse{}, err
	}
	profileAssetPath = filepath.ToSlash(relAssetPath)
	content := req.Content
	if strings.TrimSpace(content) == "" {
		bytes, readErr := afero.ReadFile(s.fs(), absAssetPath)
		if readErr != nil {
			return PythonDiagnosticsResponse{}, newAPIError(500, "asset_read_failed", readErr.Error())
		}
		content = string(bytes)
	}
	trace.Step("content")
	packageStubs, packageFingerprint := s.installedPythonPackageStubs(relAssetPath, absAssetPath, content)
	packageFileCount = len(packageStubs)
	trace.Step("packages")
	options := defaultTyOptions()
	if len(packageStubs) > 0 {
		options = tyOptionsWithSitePackages()
	}
	sessionID, sessionFingerprint := tySessionFields(relAssetPath, options, packageFingerprint)
	trace.Step("session")
	requestFiles := s.pythonTySessionFilesForRequest(sessionID, sessionFingerprint, packageStubs)
	sentFileCount = len(requestFiles)
	trace.Step("payload")
	checked, checkErr := pyintelligence.Check(ctx, pyintelligence.Request{
		Root:               "/",
		Path:               "/" + strings.TrimPrefix(relAssetPath, "/"),
		Content:            content,
		Options:            options,
		Files:              requestFiles,
		SessionID:          sessionID,
		SessionFingerprint: sessionFingerprint,
	})
	trace.Step("ty")
	if checkErr != nil {
		return PythonDiagnosticsResponse{}, newAPIError(500, "python_diagnostics_failed", checkErr.Error())
	}
	if checked.Status != "ok" {
		return PythonDiagnosticsResponse{Status: "error", AssetID: assetID, Error: checked.Error}, nil
	}
	s.markPythonTySessionFilesReady(sessionID, sessionFingerprint)
	response := PythonDiagnosticsResponse{Status: "ok", AssetID: assetID, Diagnostics: pythonDiagnosticsFromTy(checked.Diagnostics)}
	response.Diagnostics = append(response.Diagnostics, s.pythonQueryDependencyDiagnostics(ctx, assetID, content)...)
	trace.Step("map_response")
	return response, nil
}

func (s *AssetService) pythonQueryDependencyDiagnostics(ctx context.Context, assetID, content string) []PythonDiagnostic {
	if s.deps.CurrentState == nil {
		return nil
	}
	state := s.deps.CurrentState()
	for _, candidate := range state.Pipelines {
		for _, asset := range candidate.Assets {
			if asset.ID != assetID {
				continue
			}
			known := make([]string, 0, len(candidate.Assets))
			for _, pipelineAsset := range candidate.Assets {
				known = append(known, pipelineAsset.Name)
			}
			findings := pythonQueryDependencyFindingsForSource(ctx, content, asset.Name, asset.Upstreams, known)
			result := make([]PythonDiagnostic, 0, len(findings))
			for _, finding := range findings {
				var diagnosticRange *PythonRange
				if finding.Line > 0 && finding.Column > 0 {
					diagnosticRange = &PythonRange{
						Start: PythonPosition{Line: finding.Line, Column: finding.Column},
						End:   PythonPosition{Line: finding.EndLine, Column: finding.EndColumn},
					}
				}
				result = append(result, PythonDiagnostic{
					ID:         finding.Code,
					Code:       finding.Code,
					Source:     finding.Source,
					Message:    finding.Message,
					Severity:   finding.Severity,
					Range:      diagnosticRange,
					Scope:      finding.Scope,
					Confidence: finding.Confidence,
				})
			}
			return result
		}
	}
	return nil
}

func (s *AssetService) PythonCompletions(ctx context.Context, assetID string, req PythonCompletionsRequest) (PythonCompletionsResponse, *APIError) {
	trace := profiling.New("RENART_PYINTELLIGENCE_PROFILE", "python.completions")
	profileAssetPath := ""
	packageFileCount := 0
	sentFileCount := 0
	completionCount := 0
	defer func() {
		trace.Done(
			"asset="+profileAssetPath,
			"package_files="+strconv.Itoa(packageFileCount),
			"sent_files="+strconv.Itoa(sentFileCount),
			"completions="+strconv.Itoa(completionCount),
		)
	}()

	if req.Line <= 0 || req.Column <= 0 {
		return PythonCompletionsResponse{}, newAPIError(400, "invalid_position", "line and column must be positive")
	}
	relAssetPath, absAssetPath, err := s.resolvePythonAssetPath(assetID)
	trace.Step("resolve")
	if err != nil {
		return PythonCompletionsResponse{}, err
	}
	profileAssetPath = filepath.ToSlash(relAssetPath)
	content := req.Content
	if strings.TrimSpace(content) == "" {
		bytes, readErr := afero.ReadFile(s.fs(), absAssetPath)
		if readErr != nil {
			return PythonCompletionsResponse{}, newAPIError(500, "asset_read_failed", readErr.Error())
		}
		content = string(bytes)
	}
	trace.Step("content")
	packageStubs, packageFingerprint := s.installedPythonPackageStubs(relAssetPath, absAssetPath, content)
	packageFileCount = len(packageStubs)
	trace.Step("packages")
	options := defaultTyOptions()
	if len(packageStubs) > 0 {
		options = tyOptionsWithSitePackages()
	}
	sessionID, sessionFingerprint := tySessionFields(relAssetPath, options, packageFingerprint)
	trace.Step("session")
	requestFiles := s.pythonTySessionFilesForRequest(sessionID, sessionFingerprint, packageStubs)
	sentFileCount = len(requestFiles)
	trace.Step("payload")
	completed, completeErr := pyintelligence.Complete(ctx, pyintelligence.Request{
		Root:               "/",
		Path:               "/" + strings.TrimPrefix(relAssetPath, "/"),
		Content:            content,
		Options:            options,
		Files:              requestFiles,
		Line:               req.Line,
		Column:             req.Column,
		Snippets:           req.Snippets,
		SessionID:          sessionID,
		SessionFingerprint: sessionFingerprint,
	})
	trace.Step("ty")
	if completeErr != nil {
		return PythonCompletionsResponse{}, newAPIError(500, "python_completions_failed", completeErr.Error())
	}
	if completed.Status != "ok" {
		return PythonCompletionsResponse{Status: "error", AssetID: assetID, Error: completed.Error}, nil
	}
	s.markPythonTySessionFilesReady(sessionID, sessionFingerprint)
	completionCount = len(completed.Result)
	response := PythonCompletionsResponse{Status: "ok", AssetID: assetID, Completions: pythonCompletionsFromTy(completed.Result)}
	trace.Step("map_response")
	return response, nil
}

func (s *AssetService) PythonHover(ctx context.Context, assetID string, req PythonPositionRequest) (PythonHoverResponse, *APIError) {
	trace := profiling.New("RENART_PYINTELLIGENCE_PROFILE", "python.hover")
	profileAssetPath := ""
	packageFileCount := 0
	sentFileCount := 0
	defer func() {
		trace.Done("asset="+profileAssetPath, "package_files="+strconv.Itoa(packageFileCount), "sent_files="+strconv.Itoa(sentFileCount))
	}()

	tyReq, relAssetPath, mountedFileCount, serviceErr := s.pythonTyPositionRequest(assetID, req, trace)
	if serviceErr != nil {
		return PythonHoverResponse{}, serviceErr
	}
	profileAssetPath = filepath.ToSlash(relAssetPath)
	packageFileCount = mountedFileCount
	sentFileCount = len(tyReq.Files)
	hovered, hoverErr := pyintelligence.HoverAt(ctx, tyReq)
	trace.Step("ty")
	if hoverErr != nil {
		return PythonHoverResponse{}, newAPIError(500, "python_hover_failed", hoverErr.Error())
	}
	if hovered.Status != "ok" {
		return PythonHoverResponse{Status: "error", AssetID: assetID, Error: hovered.Error}, nil
	}
	s.markPythonTySessionFilesReady(tyReq.SessionID, tyReq.SessionFingerprint)
	response := PythonHoverResponse{Status: "ok", AssetID: assetID, Hover: pythonHoverFromTy(hovered.Result)}
	trace.Step("map_response")
	return response, nil
}

func (s *AssetService) PythonSignatureHelp(ctx context.Context, assetID string, req PythonPositionRequest) (PythonSignatureHelpResponse, *APIError) {
	trace := profiling.New("RENART_PYINTELLIGENCE_PROFILE", "python.signature_help")
	profileAssetPath := ""
	packageFileCount := 0
	sentFileCount := 0
	defer func() {
		trace.Done("asset="+profileAssetPath, "package_files="+strconv.Itoa(packageFileCount), "sent_files="+strconv.Itoa(sentFileCount))
	}()

	tyReq, relAssetPath, mountedFileCount, serviceErr := s.pythonTyPositionRequest(assetID, req, trace)
	if serviceErr != nil {
		return PythonSignatureHelpResponse{}, serviceErr
	}
	profileAssetPath = filepath.ToSlash(relAssetPath)
	packageFileCount = mountedFileCount
	sentFileCount = len(tyReq.Files)
	shown, signatureErr := pyintelligence.SignatureHelpAt(ctx, tyReq)
	trace.Step("ty")
	if signatureErr != nil {
		return PythonSignatureHelpResponse{}, newAPIError(500, "python_signature_help_failed", signatureErr.Error())
	}
	if shown.Status != "ok" {
		return PythonSignatureHelpResponse{Status: "error", AssetID: assetID, Error: shown.Error}, nil
	}
	s.markPythonTySessionFilesReady(tyReq.SessionID, tyReq.SessionFingerprint)
	response := PythonSignatureHelpResponse{Status: "ok", AssetID: assetID, SignatureHelp: pythonSignatureHelpFromTy(shown.Result)}
	trace.Step("map_response")
	return response, nil
}

func (s *AssetService) PythonGotoDefinition(ctx context.Context, assetID string, req PythonPositionRequest) (PythonGotoDefinitionResponse, *APIError) {
	trace := profiling.New("RENART_PYINTELLIGENCE_PROFILE", "python.goto_definition")
	profileAssetPath := ""
	packageFileCount := 0
	sentFileCount := 0
	defer func() {
		trace.Done("asset="+profileAssetPath, "package_files="+strconv.Itoa(packageFileCount), "sent_files="+strconv.Itoa(sentFileCount))
	}()

	tyReq, relAssetPath, mountedFileCount, serviceErr := s.pythonTyPositionRequest(assetID, req, trace)
	if serviceErr != nil {
		return PythonGotoDefinitionResponse{}, serviceErr
	}
	profileAssetPath = filepath.ToSlash(relAssetPath)
	packageFileCount = mountedFileCount
	sentFileCount = len(tyReq.Files)
	resolved, gotoErr := pyintelligence.GotoDefinition(ctx, tyReq)
	trace.Step("ty")
	if gotoErr != nil {
		return PythonGotoDefinitionResponse{}, newAPIError(500, "python_goto_definition_failed", gotoErr.Error())
	}
	if resolved.Status != "ok" {
		return PythonGotoDefinitionResponse{Status: "error", AssetID: assetID, Error: resolved.Error}, nil
	}
	s.markPythonTySessionFilesReady(tyReq.SessionID, tyReq.SessionFingerprint)
	response := PythonGotoDefinitionResponse{Status: "ok", AssetID: assetID, Targets: pythonGotoTargetsFromTy(resolved.Result)}
	trace.Step("map_response")
	return response, nil
}

func (s *AssetService) pythonTyPositionRequest(assetID string, req PythonPositionRequest, trace *profiling.Trace) (pyintelligence.Request, string, int, *APIError) {
	if req.Line <= 0 || req.Column <= 0 {
		return pyintelligence.Request{}, "", 0, newAPIError(400, "invalid_position", "line and column must be positive")
	}
	relAssetPath, absAssetPath, err := s.resolvePythonAssetPath(assetID)
	trace.Step("resolve")
	if err != nil {
		return pyintelligence.Request{}, "", 0, err
	}
	content := req.Content
	if strings.TrimSpace(content) == "" {
		bytes, readErr := afero.ReadFile(s.fs(), absAssetPath)
		if readErr != nil {
			return pyintelligence.Request{}, "", 0, newAPIError(500, "asset_read_failed", readErr.Error())
		}
		content = string(bytes)
	}
	trace.Step("content")
	packageStubs, packageFingerprint := s.installedPythonPackageStubs(relAssetPath, absAssetPath, content)
	trace.Step("packages")
	options := defaultTyOptions()
	if len(packageStubs) > 0 {
		options = tyOptionsWithSitePackages()
	}
	sessionID, sessionFingerprint := tySessionFields(relAssetPath, options, packageFingerprint)
	trace.Step("session")
	requestFiles := s.pythonTySessionFilesForRequest(sessionID, sessionFingerprint, packageStubs)
	trace.Step("payload")
	return pyintelligence.Request{
		Root:               "/",
		Path:               "/" + strings.TrimPrefix(relAssetPath, "/"),
		Content:            content,
		Options:            options,
		Files:              requestFiles,
		Line:               req.Line,
		Column:             req.Column,
		SessionID:          sessionID,
		SessionFingerprint: sessionFingerprint,
	}, relAssetPath, len(packageStubs), nil
}

func (s *AssetService) pythonTySessionFilesForRequest(sessionID, sessionFingerprint string, files []pyintelligence.VirtualFile) []pyintelligence.VirtualFile {
	if sessionID == "" || sessionFingerprint == "" || len(files) == 0 {
		return files
	}
	s.pythonTySessionMu.Lock()
	ready := s.pythonTySessionFiles[sessionID] == sessionFingerprint
	s.pythonTySessionMu.Unlock()
	if ready {
		return nil
	}
	return files
}

// pythonTyRecycleEvery bounds the ty WASM module's monotonic linear-memory
// growth: after this many calls the module is recycled (see pyintelligence.
// Recycle) and the session tracking is cleared so package stubs are re-sent
// into the fresh instance.
const pythonTyRecycleEvery = 200

func (s *AssetService) markPythonTySessionFilesReady(sessionID, sessionFingerprint string) {
	s.pythonTySessionMu.Lock()
	defer s.pythonTySessionMu.Unlock()
	if sessionID != "" && sessionFingerprint != "" {
		s.pythonTySessionFiles[sessionID] = sessionFingerprint
	}
	s.pythonTyCallCount++
	if s.pythonTyCallCount >= pythonTyRecycleEvery {
		pyintelligence.Recycle()
		s.pythonTySessionFiles = make(map[string]string)
		s.pythonTyCallCount = 0
	}
}

func (s *AssetService) resolvePythonAssetPath(assetID string) (string, string, *APIError) {
	relAssetPath, decodeErr := DecodeID(assetID)
	if decodeErr != nil {
		return "", "", newAPIError(400, "invalid_asset_id", "invalid asset id")
	}
	if !strings.HasSuffix(strings.ToLower(relAssetPath), ".py") {
		return "", "", newAPIError(400, "invalid_asset_type", "only Python assets are supported")
	}
	absAssetPath, joinErr := s.resolver().JoinPath(relAssetPath)
	if joinErr != nil {
		return "", "", newAPIError(400, "invalid_asset_path", joinErr.Error())
	}
	return relAssetPath, absAssetPath, nil
}

func defaultTyOptions() map[string]any {
	return map[string]any{
		"environment": map[string]any{
			"python-version": "3.11",
		},
	}
}

func tyOptionsWithSitePackages() map[string]any {
	return map[string]any{
		"environment": map[string]any{
			"python-version": "3.11",
			"extra-paths":    []string{"/site-packages"},
		},
	}
}

func tySessionFields(relAssetPath string, options map[string]any, filesFingerprint string) (string, string) {
	path := filepath.ToSlash(relAssetPath)
	hash := sha256.New()
	_, _ = hash.Write([]byte(path))
	_, _ = hash.Write([]byte{0})
	if body, err := json.Marshal(options); err == nil {
		_, _ = hash.Write(body)
	}
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(filesFingerprint))
	return "asset:" + path, hex.EncodeToString(hash.Sum(nil))
}

type pythonPackageMount struct {
	files       []pyintelligence.VirtualFile
	fingerprint string
	cacheHit    bool
}

type pythonPackageMountCacheEntry struct {
	signature   string
	fingerprint string
	files       []pyintelligence.VirtualFile
}

type pythonModuleFileMeta struct {
	absPath     string
	virtualPath string
	size        int64
	modUnixNano int64
}

var (
	pythonImportPattern      = regexp.MustCompile(`(?m)^\s*(?:import\s+([A-Za-z_]\w*)|from\s+([A-Za-z_]\w*)\s+import\b)`)
	pythonRequirementPattern = regexp.MustCompile(`^\s*([A-Za-z0-9_.-]+)`)
	pyprojectDependencyBlock = regexp.MustCompile(`(?s)dependencies\s*=\s*\[(.*?)\]`)
	quotedDependencyPattern  = regexp.MustCompile(`['"]([^'"]+)['"]`)
)

func (s *AssetService) installedPythonPackageStubs(relAssetPath, absAssetPath, content string) ([]pyintelligence.VirtualFile, string) {
	trace := profiling.New("RENART_PYINTELLIGENCE_PROFILE", "python.packages")
	moduleCount := 0
	installedCount := 0
	fileCount := 0
	cacheHits := 0
	cacheMisses := 0
	defer func() {
		trace.Done(
			"asset="+filepath.ToSlash(relAssetPath),
			"modules="+strconv.Itoa(moduleCount),
			"installed="+strconv.Itoa(installedCount),
			"files="+strconv.Itoa(fileCount),
			"cache_hits="+strconv.Itoa(cacheHits),
			"cache_misses="+strconv.Itoa(cacheMisses),
		)
	}()

	modules := pythonRequestedModules(content)
	trace.Step("imports")
	for _, dependency := range s.pythonDependencyNames(absAssetPath) {
		modules[pythonModuleNameFromDependency(dependency)] = true
	}
	trace.Step("dependencies")
	moduleCount = len(modules)
	if len(modules) == 0 {
		return nil, ""
	}

	moduleNames := make([]string, 0, len(modules))
	modulePaths := map[string]string{}
	for module := range modules {
		if module == "" || module == renartSDKModule {
			continue
		}
		if modulePath := s.pythonModuleInstallPath(relAssetPath, module); modulePath != "" {
			moduleNames = append(moduleNames, module)
			modulePaths[module] = modulePath
		}
	}
	trace.Step("resolve_paths")
	sort.Strings(moduleNames)
	installedCount = len(moduleNames)

	files := make([]pyintelligence.VirtualFile, 0, len(moduleNames)+5)
	fingerprintHash := sha256.New()
	if modules[renartSDKModule] {
		for _, stub := range pysdk.TypeStubFiles() {
			stubModule := strings.SplitN(stub.Path, "/", 2)[0]
			if stubModule != renartSDKModule && modulePaths[stubModule] != "" {
				// Prefer a workspace-installed dependency over the SDK's small
				// editor fallback. The installed mount is appended below.
				continue
			}
			virtualPath := "/site-packages/" + stub.Path
			files = append(files, pyintelligence.VirtualFile{Path: virtualPath, Content: stub.Content})
			_, _ = fingerprintHash.Write([]byte(virtualPath))
			_, _ = fingerprintHash.Write([]byte{0})
			_, _ = fingerprintHash.Write([]byte(stub.Content))
			_, _ = fingerprintHash.Write([]byte{0})
		}
	}
	for _, module := range moduleNames {
		mount := s.cachedPythonInstalledModuleFiles(module, modulePaths[module])
		if len(mount.files) == 0 {
			continue
		}
		if mount.cacheHit {
			cacheHits++
		} else {
			cacheMisses++
		}
		files = append(files, mount.files...)
		_, _ = fingerprintHash.Write([]byte(module))
		_, _ = fingerprintHash.Write([]byte{0})
		_, _ = fingerprintHash.Write([]byte(filepath.Clean(modulePaths[module])))
		_, _ = fingerprintHash.Write([]byte{0})
		_, _ = fingerprintHash.Write([]byte(mount.fingerprint))
		_, _ = fingerprintHash.Write([]byte{0})
	}
	trace.Step("mounts")
	fileCount = len(files)
	if len(files) == 0 {
		return nil, ""
	}
	return files, hex.EncodeToString(fingerprintHash.Sum(nil))
}

func (s *AssetService) cachedPythonInstalledModuleFiles(module, modulePath string) pythonPackageMount {
	metas, signature := pythonInstalledModuleFileMetas(module, modulePath)
	if signature == "" {
		return pythonPackageMount{}
	}
	cacheKey := module + "\x00" + filepath.Clean(modulePath)

	s.pythonPackageMountMu.Lock()
	if cached, ok := s.pythonPackageMountCache[cacheKey]; ok && cached.signature == signature {
		files := append([]pyintelligence.VirtualFile(nil), cached.files...)
		s.pythonPackageMountMu.Unlock()
		return pythonPackageMount{files: files, fingerprint: cached.fingerprint, cacheHit: true}
	}
	s.pythonPackageMountMu.Unlock()

	mount := pythonInstalledModuleFilesFromMetas(metas)
	if len(mount.files) == 0 {
		return pythonPackageMount{}
	}

	s.pythonPackageMountMu.Lock()
	s.pythonPackageMountCache[cacheKey] = pythonPackageMountCacheEntry{
		signature:   signature,
		fingerprint: mount.fingerprint,
		files:       append([]pyintelligence.VirtualFile(nil), mount.files...),
	}
	s.pythonPackageMountMu.Unlock()
	return mount
}

func pythonInstalledModuleFileMetas(module, modulePath string) ([]pythonModuleFileMeta, string) {
	if module == "" || modulePath == "" {
		return nil, ""
	}
	if stat, err := os.Stat(modulePath); err == nil && !stat.IsDir() {
		ext := filepath.Ext(modulePath)
		if ext == "" {
			ext = ".py"
		}
		metas := []pythonModuleFileMeta{{
			absPath:     modulePath,
			virtualPath: "/site-packages/" + module + ext,
			size:        stat.Size(),
			modUnixNano: stat.ModTime().UnixNano(),
		}}
		return metas, pythonModuleFileSignature(metas)
	}

	metas := []pythonModuleFileMeta{}
	_ = filepath.WalkDir(modulePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == "__pycache__" || name == "tests" || name == "test" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".py") && !strings.HasSuffix(path, ".pyi") {
			return nil
		}
		stat, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(modulePath, path)
		if err != nil {
			return nil
		}
		metas = append(metas, pythonModuleFileMeta{
			absPath:     path,
			virtualPath: "/site-packages/" + module + "/" + filepath.ToSlash(rel),
			size:        stat.Size(),
			modUnixNano: stat.ModTime().UnixNano(),
		})
		return nil
	})
	sort.Slice(metas, func(i, j int) bool { return metas[i].virtualPath < metas[j].virtualPath })
	return metas, pythonModuleFileSignature(metas)
}

func pythonModuleFileSignature(metas []pythonModuleFileMeta) string {
	if len(metas) == 0 {
		return ""
	}
	hash := sha256.New()
	for _, meta := range metas {
		_, _ = hash.Write([]byte(meta.virtualPath))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(meta.absPath))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(strconv.FormatInt(meta.size, 10)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(strconv.FormatInt(meta.modUnixNano, 10)))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func pythonInstalledModuleFilesFromMetas(metas []pythonModuleFileMeta) pythonPackageMount {
	files := make([]pyintelligence.VirtualFile, 0, len(metas))
	fingerprintHash := sha256.New()
	for _, meta := range metas {
		content := readFirstExistingFile(meta.absPath)
		if content == "" {
			continue
		}
		files = append(files, pyintelligence.VirtualFile{Path: meta.virtualPath, Content: content})
		_, _ = fingerprintHash.Write([]byte(meta.virtualPath))
		_, _ = fingerprintHash.Write([]byte{0})
		_, _ = fingerprintHash.Write([]byte(content))
		_, _ = fingerprintHash.Write([]byte{0})
	}
	if len(files) == 0 {
		return pythonPackageMount{}
	}
	return pythonPackageMount{files: files, fingerprint: hex.EncodeToString(fingerprintHash.Sum(nil))}
}

func readFirstExistingFile(paths ...string) string {
	for _, path := range paths {
		bytes, err := os.ReadFile(path)
		if err == nil {
			return string(bytes)
		}
	}
	return ""
}

func pythonRequestedModules(content string) map[string]bool {
	modules := map[string]bool{}
	for _, match := range pythonImportPattern.FindAllStringSubmatch(content, -1) {
		name := match[1]
		if name == "" {
			name = match[2]
		}
		name = strings.TrimSpace(strings.Split(name, ".")[0])
		if name != "" {
			modules[name] = true
		}
	}
	return modules
}

func (s *AssetService) pythonDependencyNames(absAssetPath string) []string {
	startDir := filepath.Dir(absAssetPath)
	names := []string{}
	seen := map[string]bool{}
	add := func(list []string) {
		for _, name := range list {
			if name != "" && !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	// pyproject.toml is the standard; a legacy requirements.txt is still read so
	// declared names from either file count (union), which keeps existing
	// pipelines and intellisense working during the transition.
	if pyprojectPath := nearestPythonDependencyFile(startDir, s.deps.WorkspaceRoot, pyprojectFile); pyprojectPath != "" {
		if bytes, err := os.ReadFile(pyprojectPath); err == nil {
			add(dependencyNamesFromPyproject(string(bytes)))
		}
	}
	if requirementsPath := nearestPythonDependencyFile(startDir, s.deps.WorkspaceRoot, "requirements.txt"); requirementsPath != "" {
		if bytes, err := os.ReadFile(requirementsPath); err == nil {
			add(dependencyNamesFromRequirements(string(bytes)))
		}
	}
	return names
}

func dependencyNamesFromRequirements(content string) []string {
	dependencies := []string{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.Split(line, "#")[0])
		if line == "" || strings.HasPrefix(line, "-") {
			continue
		}
		if match := pythonRequirementPattern.FindStringSubmatch(line); len(match) == 2 {
			dependencies = append(dependencies, match[1])
		}
	}
	return dependencies
}

func dependencyNamesFromPyproject(content string) []string {
	match := pyprojectDependencyBlock.FindStringSubmatch(content)
	if len(match) != 2 {
		return nil
	}
	dependencies := []string{}
	for _, quoted := range quotedDependencyPattern.FindAllStringSubmatch(match[1], -1) {
		if len(quoted) == 2 {
			dependencies = append(dependencies, quoted[1])
		}
	}
	return dependencies
}

func nearestPythonDependencyFile(startDir, workspaceRoot, filename string) string {
	stopDir := filepath.Clean(workspaceRoot)
	dir := filepath.Clean(startDir)
	for {
		candidate := filepath.Join(dir, filename)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		if dir == stopDir || dir == string(filepath.Separator) || dir == "." {
			return ""
		}
		dir = filepath.Dir(dir)
	}
}

func pythonModuleNameFromDependency(dependency string) string {
	match := pythonRequirementPattern.FindStringSubmatch(dependency)
	if len(match) != 2 {
		return ""
	}
	module := strings.ToLower(strings.TrimSpace(match[1]))
	module = strings.ReplaceAll(module, "-", "_")
	module = strings.Split(module, ".")[0]
	return module
}

func (s *AssetService) pythonModuleIsInstalled(relAssetPath, module string) bool {
	return s.pythonModuleInstallPath(relAssetPath, module) != ""
}

func (s *AssetService) pythonModuleInstallPath(relAssetPath, module string) string {
	module = strings.TrimSpace(strings.Split(module, ".")[0])
	if module == "" {
		return ""
	}
	for _, root := range s.pythonPackageSearchRoots(relAssetPath) {
		if modulePath := pythonModulePathInRoot(root, module); modulePath != "" {
			return modulePath
		}
	}
	return ""
}

func (s *AssetService) pythonPackageSearchRoots(relAssetPath string) []string {
	roots := []string{}
	if s.deps.WorkspaceRoot != "" {
		workspaceRoot := filepath.Clean(s.deps.WorkspaceRoot)
		assetDir := filepath.Join(workspaceRoot, filepath.Dir(relAssetPath))
		for _, base := range []string{assetDir, workspaceRoot} {
			roots = appendPythonSitePackageGlobs(roots,
				filepath.Join(base, ".venv", "lib", "python*", "site-packages"),
				filepath.Join(base, "venv", "lib", "python*", "site-packages"),
			)
		}
		// Notebook cells run in a per-notebook uv venv kept outside the
		// (git-tracked) notebook folder, so it is not reachable from assetDir's
		// .venv glob above. Mount its site-packages so ty resolves imports of
		// notebook dependencies instead of flagging them as unresolved.
		roots = appendPythonSitePackageGlobs(roots,
			filepath.Join(notebookVenvDir(workspaceRoot, assetDir), "lib", "python*", "site-packages"),
		)
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = appendPythonSitePackageGlobs(roots,
			filepath.Join(home, ".bruin", "virtualenvs", "*", "lib", "python*", "site-packages"),
			filepath.Join(home, ".cache", "uv", "archive-v0", "*", "lib", "python*", "site-packages"),
			filepath.Join(home, ".local", "share", "uv", "archive-v0", "*", "lib", "python*", "site-packages"),
		)
		roots = appendPythonSitePackageGlobs(roots,
			filepath.Join(home, ".cache", "uv", "archive-v0", "*"),
			filepath.Join(home, ".local", "share", "uv", "archive-v0", "*"),
		)
	}
	return uniqueStrings(roots)
}

func appendPythonSitePackageGlobs(roots []string, patterns ...string) []string {
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		roots = append(roots, matches...)
	}
	return roots
}

func pythonModuleExistsInRoot(root, module string) bool {
	return pythonModulePathInRoot(root, module) != ""
}

func pythonModulePathInRoot(root, module string) string {
	if stat, err := os.Stat(filepath.Join(root, module)); err == nil && stat.IsDir() {
		return filepath.Join(root, module)
	}
	if stat, err := os.Stat(filepath.Join(root, module+".py")); err == nil && !stat.IsDir() {
		return filepath.Join(root, module+".py")
	}
	if stat, err := os.Stat(filepath.Join(root, module+".pyi")); err == nil && !stat.IsDir() {
		return filepath.Join(root, module+".pyi")
	}
	return ""
}

func uniqueStrings(input []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(input))
	for _, value := range input {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func pythonDiagnosticsFromTy(input []pyintelligence.Diagnostic) []PythonDiagnostic {
	result := make([]PythonDiagnostic, 0, len(input))
	for _, diagnostic := range input {
		result = append(result, PythonDiagnostic{
			ID:       diagnostic.ID,
			Message:  diagnostic.Message,
			Severity: diagnostic.Severity,
			Range:    pythonRangeFromTy(diagnostic.Range),
			Display:  diagnostic.Display,
		})
	}
	return result
}

func pythonRangeFromTy(input *pyintelligence.Range) *PythonRange {
	if input == nil {
		return nil
	}
	return &PythonRange{
		Start: PythonPosition{Line: input.Start.Line, Column: input.Start.Column},
		End:   PythonPosition{Line: input.End.Line, Column: input.End.Column},
	}
}

func pythonCompletionsFromTy(input []pyintelligence.Completion) []PythonCompletion {
	result := make([]PythonCompletion, 0, len(input))
	for _, completion := range input {
		result = append(result, PythonCompletion{
			Label:               completion.Label,
			Kind:                completion.Kind,
			Detail:              completion.Detail,
			InsertText:          completion.InsertText,
			InsertTextFormat:    completion.InsertTextFormat,
			Documentation:       completion.Documentation,
			ModuleName:          completion.ModuleName,
			AdditionalTextEdits: pythonTextEditsFromTy(completion.AdditionalTextEdits),
		})
	}
	return result
}

func pythonTextEditsFromTy(input []pyintelligence.TextEdit) []PythonTextEdit {
	result := make([]PythonTextEdit, 0, len(input))
	for _, edit := range input {
		result = append(result, PythonTextEdit{
			Range: PythonRange{
				Start: PythonPosition{Line: edit.Range.Start.Line, Column: edit.Range.Start.Column},
				End:   PythonPosition{Line: edit.Range.End.Line, Column: edit.Range.End.Column},
			},
			Text: edit.Text,
		})
	}
	return result
}

func pythonHoverFromTy(input *pyintelligence.Hover) *PythonHover {
	if input == nil {
		return nil
	}
	return &PythonHover{Contents: input.Contents, Range: pythonRangeFromTy(input.Range)}
}

func pythonSignatureHelpFromTy(input *pyintelligence.SignatureHelp) *PythonSignatureHelp {
	if input == nil {
		return nil
	}
	return &PythonSignatureHelp{
		Signatures:      pythonSignaturesFromTy(input.Signatures),
		ActiveSignature: input.ActiveSignature,
		ActiveParameter: input.ActiveParameter,
	}
}

func pythonSignaturesFromTy(input []pyintelligence.Signature) []PythonSignature {
	result := make([]PythonSignature, 0, len(input))
	for _, signature := range input {
		result = append(result, PythonSignature{
			Label:           signature.Label,
			Documentation:   signature.Documentation,
			Parameters:      pythonSignatureParametersFromTy(signature.Parameters),
			ActiveParameter: signature.ActiveParameter,
		})
	}
	return result
}

func pythonSignatureParametersFromTy(input []pyintelligence.SignatureParameter) []PythonSignatureParameter {
	result := make([]PythonSignatureParameter, 0, len(input))
	for _, parameter := range input {
		result = append(result, PythonSignatureParameter{
			Label:         parameter.Label,
			Name:          parameter.Name,
			Type:          parameter.Type,
			Documentation: parameter.Documentation,
		})
	}
	return result
}

func pythonGotoTargetsFromTy(input []pyintelligence.GotoTarget) []PythonGotoTarget {
	result := make([]PythonGotoTarget, 0, len(input))
	for _, target := range input {
		result = append(result, PythonGotoTarget{
			Path:       strings.TrimPrefix(target.Path, "/"),
			FocusRange: pythonRangeValueFromTy(target.FocusRange),
			FullRange:  pythonRangeValueFromTy(target.FullRange),
		})
	}
	return result
}

func pythonRangeValueFromTy(input pyintelligence.Range) PythonRange {
	return PythonRange{
		Start: PythonPosition{Line: input.Start.Line, Column: input.Start.Column},
		End:   PythonPosition{Line: input.End.Line, Column: input.End.Column},
	}
}
