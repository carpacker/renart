package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

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
	checked, checkErr := pyintelligence.Check(ctx, pyintelligence.Request{
		Root:    "/",
		Path:    "/" + strings.TrimPrefix(relAssetPath, "/"),
		Content: content,
		Options: options,
		Files:   packageStubs,
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
	if completions := s.fastPythonPackageCompletions(relAssetPath, content, req.Line, req.Column); len(completions) > 0 {
		return PythonCompletionsResponse{Status: "ok", AssetID: assetID, Completions: completions}, nil
	}
	packageStubs := s.installedPythonPackageStubs(relAssetPath, absAssetPath, content)
	options := defaultTyOptions()
	if len(packageStubs) > 0 {
		options = tyOptionsWithSitePackages()
	}
	completed, completeErr := pyintelligence.Complete(ctx, pyintelligence.Request{
		Root:     "/",
		Path:     "/" + strings.TrimPrefix(relAssetPath, "/"),
		Content:  content,
		Options:  options,
		Files:    packageStubs,
		Line:     req.Line,
		Column:   req.Column,
		Snippets: req.Snippets,
	})
	if completeErr != nil {
		return PythonCompletionsResponse{}, newServiceAPIError(500, "python_completions_failed", completeErr.Error())
	}
	if completed.Status != "ok" {
		return PythonCompletionsResponse{Status: "error", AssetID: assetID, Error: completed.Error}, nil
	}
	return PythonCompletionsResponse{Status: "ok", AssetID: assetID, Completions: pythonCompletionsFromTy(completed.Result)}, nil
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

var (
	pythonImportPattern      = regexp.MustCompile(`(?m)^\s*(?:import\s+([A-Za-z_]\w*)|from\s+([A-Za-z_]\w*)\s+import\b)`)
	pythonRequirementPattern = regexp.MustCompile(`^\s*([A-Za-z0-9_.-]+)`)
	pyprojectDependencyBlock = regexp.MustCompile(`(?s)dependencies\s*=\s*\[(.*?)\]`)
	quotedDependencyPattern  = regexp.MustCompile(`['"]([^'"]+)['"]`)
	pythonIdentifierPattern  = regexp.MustCompile(`^[A-Za-z_]\w*$`)
	pythonDefClassPattern    = regexp.MustCompile(`(?m)^(?:def|class)\s+([A-Za-z_]\w*)\b`)
	pythonAssignmentPattern  = regexp.MustCompile(`(?m)^([A-Za-z_]\w*)\s*=`)
	pythonDotTargetPattern   = regexp.MustCompile(`([A-Za-z_]\w*)\.([A-Za-z_]\w*)?$`)
	pythonImportAliasPattern = regexp.MustCompile(`(?m)^\s*import\s+([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*)(?:\s+as\s+([A-Za-z_]\w*))?`)
	pythonConstructorAssign  = regexp.MustCompile(`(?m)^\s*([A-Za-z_]\w*)\s*=\s*([A-Za-z_]\w*)\.([A-Za-z_]\w*)\s*\(`)
)

var pythonClassMemberCache sync.Map

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
		files = append(files, pyintelligence.VirtualFile{
			Path:    "/site-packages/" + module + "/__init__.pyi",
			Content: pythonInstalledModuleStub(modulePaths[module]),
		})
	}
	return files
}

func pythonInstalledModuleStub(modulePath string) string {
	content := ""
	if stat, err := os.Stat(modulePath); err == nil && stat.IsDir() {
		content = readFirstExistingFile(
			filepath.Join(modulePath, "__init__.pyi"),
			filepath.Join(modulePath, "__init__.py"),
		)
	} else {
		content = readFirstExistingFile(modulePath)
	}

	names := exportedPythonNames(content)
	var builder strings.Builder
	builder.WriteString("from typing import Any\n\n")
	for _, name := range names {
		_, _ = fmt.Fprintf(&builder, "%s: Any\n", name)
	}
	builder.WriteString("\ndef __getattr__(name: str) -> Any: ...\n")
	return builder.String()
}

func (s *AssetService) fastPythonPackageCompletions(relAssetPath, content string, line, column int) []PythonCompletion {
	target, prefix := pythonDotTargetAt(content, line, column)
	if target == "" {
		return nil
	}

	aliases := pythonImportAliases(content)
	if module := aliases[target]; module != "" {
		modulePath := s.pythonModuleInstallPath(relAssetPath, module)
		if modulePath == "" {
			return nil
		}
		return pythonSymbolCompletions(pythonModuleExports(modulePath), prefix, module)
	}

	if classRef := pythonAssignedConstructor(contentBeforePosition(content, line, column), target, aliases); classRef.Module != "" && classRef.Name != "" {
		modulePath := s.pythonModuleInstallPath(relAssetPath, classRef.Module)
		if modulePath == "" {
			return nil
		}
		return pythonMemberCompletions(pythonClassMembers(modulePath, classRef.Module, classRef.Name), prefix, classRef.Name)
	}

	return nil
}

type pythonClassRef struct {
	Module string
	Name   string
}

func pythonDotTargetAt(content string, line, column int) (string, string) {
	if line <= 0 || column <= 0 {
		return "", ""
	}
	lines := strings.Split(content, "\n")
	if line > len(lines) {
		return "", ""
	}
	lineText := lines[line-1]
	end := min(column-1, len(lineText))
	match := pythonDotTargetPattern.FindStringSubmatch(lineText[:end])
	if len(match) != 3 {
		return "", ""
	}
	return match[1], match[2]
}

func contentBeforePosition(content string, line, column int) string {
	if line <= 0 || column <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if line > len(lines) {
		return content
	}
	lines = lines[:line]
	last := lines[len(lines)-1]
	lines[len(lines)-1] = last[:min(column-1, len(last))]
	return strings.Join(lines, "\n")
}

func pythonImportAliases(content string) map[string]string {
	aliases := map[string]string{}
	for _, match := range pythonImportAliasPattern.FindAllStringSubmatch(content, -1) {
		module := strings.TrimSpace(match[1])
		alias := strings.TrimSpace(match[2])
		if alias == "" {
			alias = strings.Split(module, ".")[0]
		}
		aliases[alias] = strings.Split(module, ".")[0]
	}
	return aliases
}

func pythonAssignedConstructor(content, variable string, aliases map[string]string) pythonClassRef {
	matches := pythonConstructorAssign.FindAllStringSubmatch(content, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		match := matches[i]
		if len(match) != 4 || match[1] != variable {
			continue
		}
		module := aliases[match[2]]
		if module == "" {
			continue
		}
		return pythonClassRef{Module: module, Name: match[3]}
	}
	return pythonClassRef{}
}

type pythonSymbol struct {
	Name string
	Kind string
}

func pythonModuleExports(modulePath string) []pythonSymbol {
	names := exportedPythonNames(pythonModuleInitContent(modulePath))
	symbols := make([]pythonSymbol, 0, len(names))
	for _, name := range names {
		kind := "variable"
		if len(name) > 0 && strings.ToUpper(name[:1]) == name[:1] {
			kind = "class"
		}
		symbols = append(symbols, pythonSymbol{Name: name, Kind: kind})
	}
	return symbols
}

func pythonModuleInitContent(modulePath string) string {
	if stat, err := os.Stat(modulePath); err == nil && stat.IsDir() {
		return readFirstExistingFile(
			filepath.Join(modulePath, "__init__.pyi"),
			filepath.Join(modulePath, "__init__.py"),
		)
	}
	return readFirstExistingFile(modulePath)
}

func pythonSymbolCompletions(symbols []pythonSymbol, prefix, detail string) []PythonCompletion {
	completions := make([]PythonCompletion, 0, len(symbols))
	for _, symbol := range symbols {
		if prefix != "" && !strings.HasPrefix(symbol.Name, prefix) {
			continue
		}
		completions = append(completions, PythonCompletion{
			Label:            symbol.Name,
			Kind:             symbol.Kind,
			Detail:           detail,
			InsertTextFormat: "plaintext",
		})
	}
	return completions
}

func pythonMemberCompletions(members []pythonSymbol, prefix, detail string) []PythonCompletion {
	return pythonSymbolCompletions(members, prefix, detail)
}

func pythonClassMembers(modulePath, rootModule, className string) []pythonSymbol {
	cacheKey := modulePath + "::" + className
	if cached, ok := pythonClassMemberCache.Load(cacheKey); ok {
		return cached.([]pythonSymbol)
	}

	content := ""
	if classFile := resolvePythonClassFile(modulePath, rootModule, className); classFile != "" {
		content = readFirstExistingFile(classFile)
	}
	if content == "" {
		content = searchPythonClassContent(modulePath, className)
	}
	members := parsePythonClassMembers(content, className)
	pythonClassMemberCache.Store(cacheKey, members)
	return members
}

func resolvePythonClassFile(modulePath, rootModule, className string) string {
	siteRoot := filepath.Dir(modulePath)
	moduleName := rootModule
	for i := 0; i < 4; i++ {
		path := pythonModuleFilePath(siteRoot, moduleName)
		if path == "" {
			return ""
		}
		content := readFirstExistingFile(path)
		if strings.Contains(content, "class "+className) {
			return path
		}
		source := pythonImportedSymbolSources(content)[className]
		if source == "" || source == moduleName {
			return ""
		}
		moduleName = source
	}
	return ""
}

func pythonModuleFilePath(siteRoot, moduleName string) string {
	base := filepath.Join(append([]string{siteRoot}, strings.Split(moduleName, ".")...)...)
	for _, path := range []string{base + ".pyi", base + ".py", filepath.Join(base, "__init__.pyi"), filepath.Join(base, "__init__.py")} {
		if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
			return path
		}
	}
	return ""
}

func pythonImportedSymbolSources(content string) map[string]string {
	sources := map[string]string{}
	for _, entry := range pythonImportEntries(content) {
		for _, name := range entry.Names {
			sources[name] = entry.Module
		}
	}
	return sources
}

type pythonImportEntry struct {
	Module string
	Names  []string
}

func pythonImportEntries(content string) []pythonImportEntry {
	entries := []pythonImportEntry{}
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "from ") || !strings.Contains(line, " import ") {
			continue
		}
		parts := strings.SplitN(strings.TrimPrefix(line, "from "), " import ", 2)
		if len(parts) != 2 {
			continue
		}
		module := strings.TrimSpace(parts[0])
		importPart := parts[1]
		if strings.Contains(importPart, "(") && !strings.Contains(importPart, ")") {
			importPart = strings.TrimPrefix(strings.SplitN(importPart, "(", 2)[1], "(")
			for i+1 < len(lines) {
				i++
				segment := strings.TrimSpace(lines[i])
				if strings.Contains(segment, ")") {
					importPart += "," + strings.SplitN(segment, ")", 2)[0]
					break
				}
				importPart += "," + segment
			}
		} else if strings.Contains(importPart, "(") {
			importPart = strings.SplitN(importPart, "(", 2)[1]
			importPart = strings.SplitN(importPart, ")", 2)[0]
		}
		names := []string{}
		for _, imported := range strings.Split(importPart, ",") {
			imported = strings.TrimSpace(strings.Split(imported, "#")[0])
			if imported == "" {
				continue
			}
			parts := strings.Fields(imported)
			name := parts[0]
			if len(parts) >= 3 && parts[1] == "as" {
				name = parts[2]
			}
			if pythonIdentifierPattern.MatchString(name) {
				names = append(names, name)
			}
		}
		entries = append(entries, pythonImportEntry{Module: module, Names: names})
	}
	return entries
}

var errPythonClassFound = errors.New("python class found")

func searchPythonClassContent(modulePath, className string) string {
	root := modulePath
	if stat, err := os.Stat(root); err != nil || !stat.IsDir() {
		root = filepath.Dir(root)
	}
	content := ""
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == "__pycache__" || name == "tests" || name == "test" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".py") && !strings.HasSuffix(path, ".pyi") {
			return nil
		}
		candidate := readFirstExistingFile(path)
		if strings.Contains(candidate, "class "+className) {
			content = candidate
			return errPythonClassFound
		}
		return nil
	})
	return content
}

func parsePythonClassMembers(content, className string) []pythonSymbol {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	classIndent := -1
	members := map[string]string{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if classIndent < 0 {
			if strings.HasPrefix(trimmed, "class "+className) {
				classIndent = indent
			}
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "@") {
			continue
		}
		if indent <= classIndent {
			break
		}
		if strings.HasPrefix(trimmed, "def ") {
			name := strings.Fields(strings.TrimPrefix(trimmed, "def "))[0]
			name = strings.Split(name, "(")[0]
			addPythonMember(members, name, "method")
			continue
		}
		if match := pythonAssignmentPattern.FindStringSubmatch(trimmed); len(match) == 2 {
			addPythonMember(members, match[1], "property")
		}
	}
	result := make([]pythonSymbol, 0, len(members))
	for name, kind := range members {
		result = append(result, pythonSymbol{Name: name, Kind: kind})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func addPythonMember(members map[string]string, name, kind string) {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "_") || !pythonIdentifierPattern.MatchString(name) {
		return
	}
	members[name] = kind
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

func exportedPythonNames(content string) []string {
	names := map[string]bool{}
	for _, match := range pythonDefClassPattern.FindAllStringSubmatch(content, -1) {
		if len(match) == 2 {
			addPythonExportName(names, match[1])
		}
	}
	for _, match := range pythonAssignmentPattern.FindAllStringSubmatch(content, -1) {
		if len(match) == 2 {
			addPythonExportName(names, match[1])
		}
	}

	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "from ") || !strings.Contains(line, " import ") {
			continue
		}
		importPart := strings.SplitN(line, " import ", 2)[1]
		if strings.Contains(importPart, "(") && !strings.Contains(importPart, ")") {
			importPart = strings.TrimPrefix(strings.SplitN(importPart, "(", 2)[1], "(")
			for i+1 < len(lines) {
				i++
				segment := strings.TrimSpace(lines[i])
				if strings.Contains(segment, ")") {
					importPart += "," + strings.SplitN(segment, ")", 2)[0]
					break
				}
				importPart += "," + segment
			}
		} else if strings.Contains(importPart, "(") {
			importPart = strings.SplitN(importPart, "(", 2)[1]
			importPart = strings.SplitN(importPart, ")", 2)[0]
		}
		for _, imported := range strings.Split(importPart, ",") {
			imported = strings.TrimSpace(strings.Split(imported, "#")[0])
			if imported == "" {
				continue
			}
			parts := strings.Fields(imported)
			name := parts[0]
			if len(parts) >= 3 && parts[1] == "as" {
				name = parts[2]
			}
			addPythonExportName(names, name)
		}
	}

	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func addPythonExportName(names map[string]bool, name string) {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "_") || !pythonIdentifierPattern.MatchString(name) {
		return
	}
	names[name] = true
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
