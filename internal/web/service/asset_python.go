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
	"strings"

	"github.com/spf13/afero"
	"renart/internal/web/pyintelligence"
)

func (s *AssetService) FormatPython(ctx context.Context, assetID string, req FormatPythonAssetRequest) (FormatPythonAssetResponse, *ServiceAPIError) {
	relAssetPath, absAssetPath, err := s.resolvePythonAssetPath(assetID)
	if err != nil {
		return FormatPythonAssetResponse{}, err
	}
	fs := s.fs()
	originalBytes, readErr := afero.ReadFile(fs, absAssetPath)
	if readErr != nil {
		return FormatPythonAssetResponse{}, newServiceAPIError(500, "asset_read_failed", readErr.Error())
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
		return FormatPythonAssetResponse{}, newServiceAPIError(500, "python_format_failed", formatErr.Error())
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
		return FormatPythonAssetResponse{}, newServiceAPIError(500, "asset_write_failed", writeErr.Error())
	}
	s.deps.SuppressWatcher(relAssetPath)
	s.deps.PushWorkspaceUpdateImmediateWithChangedIDs(ctx, "asset.updated", relAssetPath, []string{assetID})
	return FormatPythonAssetResponse{Status: "ok", AssetID: assetID, Content: formattedContent}, nil
}

func (s *AssetService) PythonDiagnostics(ctx context.Context, assetID string, req PythonDiagnosticsRequest) (PythonDiagnosticsResponse, *ServiceAPIError) {
	relAssetPath, absAssetPath, err := s.resolvePythonAssetPath(assetID)
	if err != nil {
		return PythonDiagnosticsResponse{}, err
	}
	content := req.Content
	if strings.TrimSpace(content) == "" {
		bytes, readErr := afero.ReadFile(s.fs(), absAssetPath)
		if readErr != nil {
			return PythonDiagnosticsResponse{}, newServiceAPIError(500, "asset_read_failed", readErr.Error())
		}
		content = string(bytes)
	}
	packageStubs := s.installedPythonPackageStubs(relAssetPath, absAssetPath, content)
	options := defaultTyOptions()
	if len(packageStubs) > 0 {
		options = tyOptionsWithSitePackages()
	}
	sessionID, sessionFingerprint := tySessionFields(relAssetPath, options, packageStubs)
	checked, checkErr := pyintelligence.Check(ctx, pyintelligence.Request{
		Root:               "/",
		Path:               "/" + strings.TrimPrefix(relAssetPath, "/"),
		Content:            content,
		Options:            options,
		Files:              packageStubs,
		SessionID:          sessionID,
		SessionFingerprint: sessionFingerprint,
	})
	if checkErr != nil {
		return PythonDiagnosticsResponse{}, newServiceAPIError(500, "python_diagnostics_failed", checkErr.Error())
	}
	if checked.Status != "ok" {
		return PythonDiagnosticsResponse{Status: "error", AssetID: assetID, Error: checked.Error}, nil
	}
	return PythonDiagnosticsResponse{Status: "ok", AssetID: assetID, Diagnostics: pythonDiagnosticsFromTy(checked.Diagnostics)}, nil
}

func (s *AssetService) PythonCompletions(ctx context.Context, assetID string, req PythonCompletionsRequest) (PythonCompletionsResponse, *ServiceAPIError) {
	if req.Line <= 0 || req.Column <= 0 {
		return PythonCompletionsResponse{}, newServiceAPIError(400, "invalid_position", "line and column must be positive")
	}
	relAssetPath, absAssetPath, err := s.resolvePythonAssetPath(assetID)
	if err != nil {
		return PythonCompletionsResponse{}, err
	}
	content := req.Content
	if strings.TrimSpace(content) == "" {
		bytes, readErr := afero.ReadFile(s.fs(), absAssetPath)
		if readErr != nil {
			return PythonCompletionsResponse{}, newServiceAPIError(500, "asset_read_failed", readErr.Error())
		}
		content = string(bytes)
	}
	packageStubs := s.installedPythonPackageStubs(relAssetPath, absAssetPath, content)
	options := defaultTyOptions()
	if len(packageStubs) > 0 {
		options = tyOptionsWithSitePackages()
	}
	sessionID, sessionFingerprint := tySessionFields(relAssetPath, options, packageStubs)
	completed, completeErr := pyintelligence.Complete(ctx, pyintelligence.Request{
		Root:               "/",
		Path:               "/" + strings.TrimPrefix(relAssetPath, "/"),
		Content:            content,
		Options:            options,
		Files:              packageStubs,
		Line:               req.Line,
		Column:             req.Column,
		Snippets:           req.Snippets,
		SessionID:          sessionID,
		SessionFingerprint: sessionFingerprint,
	})
	if completeErr != nil {
		return PythonCompletionsResponse{}, newServiceAPIError(500, "python_completions_failed", completeErr.Error())
	}
	if completed.Status != "ok" {
		return PythonCompletionsResponse{Status: "error", AssetID: assetID, Error: completed.Error}, nil
	}
	return PythonCompletionsResponse{Status: "ok", AssetID: assetID, Completions: pythonCompletionsFromTy(completed.Result)}, nil
}

func (s *AssetService) PythonHover(ctx context.Context, assetID string, req PythonPositionRequest) (PythonHoverResponse, *ServiceAPIError) {
	tyReq, serviceErr := s.pythonTyPositionRequest(assetID, req)
	if serviceErr != nil {
		return PythonHoverResponse{}, serviceErr
	}
	hovered, hoverErr := pyintelligence.HoverAt(ctx, tyReq)
	if hoverErr != nil {
		return PythonHoverResponse{}, newServiceAPIError(500, "python_hover_failed", hoverErr.Error())
	}
	if hovered.Status != "ok" {
		return PythonHoverResponse{Status: "error", AssetID: assetID, Error: hovered.Error}, nil
	}
	return PythonHoverResponse{Status: "ok", AssetID: assetID, Hover: pythonHoverFromTy(hovered.Result)}, nil
}

func (s *AssetService) PythonSignatureHelp(ctx context.Context, assetID string, req PythonPositionRequest) (PythonSignatureHelpResponse, *ServiceAPIError) {
	tyReq, serviceErr := s.pythonTyPositionRequest(assetID, req)
	if serviceErr != nil {
		return PythonSignatureHelpResponse{}, serviceErr
	}
	shown, signatureErr := pyintelligence.SignatureHelpAt(ctx, tyReq)
	if signatureErr != nil {
		return PythonSignatureHelpResponse{}, newServiceAPIError(500, "python_signature_help_failed", signatureErr.Error())
	}
	if shown.Status != "ok" {
		return PythonSignatureHelpResponse{Status: "error", AssetID: assetID, Error: shown.Error}, nil
	}
	return PythonSignatureHelpResponse{Status: "ok", AssetID: assetID, SignatureHelp: pythonSignatureHelpFromTy(shown.Result)}, nil
}

func (s *AssetService) PythonGotoDefinition(ctx context.Context, assetID string, req PythonPositionRequest) (PythonGotoDefinitionResponse, *ServiceAPIError) {
	tyReq, serviceErr := s.pythonTyPositionRequest(assetID, req)
	if serviceErr != nil {
		return PythonGotoDefinitionResponse{}, serviceErr
	}
	resolved, gotoErr := pyintelligence.GotoDefinition(ctx, tyReq)
	if gotoErr != nil {
		return PythonGotoDefinitionResponse{}, newServiceAPIError(500, "python_goto_definition_failed", gotoErr.Error())
	}
	if resolved.Status != "ok" {
		return PythonGotoDefinitionResponse{Status: "error", AssetID: assetID, Error: resolved.Error}, nil
	}
	return PythonGotoDefinitionResponse{Status: "ok", AssetID: assetID, Targets: pythonGotoTargetsFromTy(resolved.Result)}, nil
}

func (s *AssetService) pythonTyPositionRequest(assetID string, req PythonPositionRequest) (pyintelligence.Request, *ServiceAPIError) {
	if req.Line <= 0 || req.Column <= 0 {
		return pyintelligence.Request{}, newServiceAPIError(400, "invalid_position", "line and column must be positive")
	}
	relAssetPath, absAssetPath, err := s.resolvePythonAssetPath(assetID)
	if err != nil {
		return pyintelligence.Request{}, err
	}
	content := req.Content
	if strings.TrimSpace(content) == "" {
		bytes, readErr := afero.ReadFile(s.fs(), absAssetPath)
		if readErr != nil {
			return pyintelligence.Request{}, newServiceAPIError(500, "asset_read_failed", readErr.Error())
		}
		content = string(bytes)
	}
	packageStubs := s.installedPythonPackageStubs(relAssetPath, absAssetPath, content)
	options := defaultTyOptions()
	if len(packageStubs) > 0 {
		options = tyOptionsWithSitePackages()
	}
	sessionID, sessionFingerprint := tySessionFields(relAssetPath, options, packageStubs)
	return pyintelligence.Request{
		Root:               "/",
		Path:               "/" + strings.TrimPrefix(relAssetPath, "/"),
		Content:            content,
		Options:            options,
		Files:              packageStubs,
		Line:               req.Line,
		Column:             req.Column,
		SessionID:          sessionID,
		SessionFingerprint: sessionFingerprint,
	}, nil
}

func (s *AssetService) resolvePythonAssetPath(assetID string) (string, string, *ServiceAPIError) {
	relAssetPath, decodeErr := DecodeID(assetID)
	if decodeErr != nil {
		return "", "", newServiceAPIError(400, "invalid_asset_id", "invalid asset id")
	}
	if !strings.HasSuffix(strings.ToLower(relAssetPath), ".py") {
		return "", "", newServiceAPIError(400, "invalid_asset_type", "only Python assets are supported")
	}
	absAssetPath, joinErr := s.resolver().JoinPath(relAssetPath)
	if joinErr != nil {
		return "", "", newServiceAPIError(400, "invalid_asset_path", joinErr.Error())
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

func tySessionFields(relAssetPath string, options map[string]any, files []pyintelligence.VirtualFile) (string, string) {
	path := filepath.ToSlash(relAssetPath)
	hash := sha256.New()
	_, _ = hash.Write([]byte(path))
	_, _ = hash.Write([]byte{0})
	if body, err := json.Marshal(options); err == nil {
		_, _ = hash.Write(body)
	}
	_, _ = hash.Write([]byte{0})
	for _, file := range files {
		_, _ = hash.Write([]byte(file.Path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(file.Content))
		_, _ = hash.Write([]byte{0})
	}
	return "asset:" + path, hex.EncodeToString(hash.Sum(nil))
}

var (
	pythonImportPattern      = regexp.MustCompile(`(?m)^\s*(?:import\s+([A-Za-z_]\w*)|from\s+([A-Za-z_]\w*)\s+import\b)`)
	pythonRequirementPattern = regexp.MustCompile(`^\s*([A-Za-z0-9_.-]+)`)
	pyprojectDependencyBlock = regexp.MustCompile(`(?s)dependencies\s*=\s*\[(.*?)\]`)
	quotedDependencyPattern  = regexp.MustCompile(`['"]([^'"]+)['"]`)
)

func (s *AssetService) installedPythonPackageStubs(relAssetPath, absAssetPath, content string) []pyintelligence.VirtualFile {
	modules := pythonRequestedModules(content)
	for _, dependency := range s.pythonDependencyNames(absAssetPath) {
		modules[pythonModuleNameFromDependency(dependency)] = true
	}
	if len(modules) == 0 {
		return nil
	}

	moduleNames := make([]string, 0, len(modules))
	modulePaths := map[string]string{}
	for module := range modules {
		if module == "" {
			continue
		}
		if modulePath := s.pythonModuleInstallPath(relAssetPath, module); modulePath != "" {
			moduleNames = append(moduleNames, module)
			modulePaths[module] = modulePath
		}
	}
	sort.Strings(moduleNames)

	files := make([]pyintelligence.VirtualFile, 0, len(moduleNames))
	for _, module := range moduleNames {
		files = append(files, pythonInstalledModuleFiles(module, modulePaths[module])...)
	}
	return files
}

func pythonInstalledModuleFiles(module, modulePath string) []pyintelligence.VirtualFile {
	if module == "" || modulePath == "" {
		return nil
	}
	if stat, err := os.Stat(modulePath); err == nil && !stat.IsDir() {
		content := readFirstExistingFile(modulePath)
		if content == "" {
			return nil
		}
		ext := filepath.Ext(modulePath)
		if ext == "" {
			ext = ".py"
		}
		return []pyintelligence.VirtualFile{{Path: "/site-packages/" + module + ext, Content: content}}
	}

	files := []pyintelligence.VirtualFile{}
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
		content := readFirstExistingFile(path)
		if content == "" {
			return nil
		}
		rel, err := filepath.Rel(modulePath, path)
		if err != nil {
			return nil
		}
		files = append(files, pyintelligence.VirtualFile{
			Path:    "/site-packages/" + module + "/" + filepath.ToSlash(rel),
			Content: content,
		})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
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
	requirementsPath := nearestPythonDependencyFile(filepath.Dir(absAssetPath), s.deps.WorkspaceRoot, "requirements.txt")
	if requirementsPath != "" {
		bytes, err := os.ReadFile(requirementsPath)
		if err != nil {
			return nil
		}
		return dependencyNamesFromRequirements(string(bytes))
	}

	pyprojectPath := nearestPythonDependencyFile(filepath.Dir(absAssetPath), s.deps.WorkspaceRoot, "pyproject.toml")
	if pyprojectPath == "" {
		return nil
	}
	bytes, err := os.ReadFile(pyprojectPath)
	if err != nil {
		return nil
	}
	return dependencyNamesFromPyproject(string(bytes))
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
