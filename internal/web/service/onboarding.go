package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/connection"
	"github.com/bruin-data/bruin/pkg/git"
	bruinpath "github.com/bruin-data/bruin/pkg/path"
	"github.com/spf13/afero"
)

type OnboardingImportRequest struct {
	ConnectionName  string
	EnvironmentName string
	PipelineName    string
	Schema          string
	Pattern         string
	Tables          []string
	DisableColumns  bool
	CreateIfMissing bool
}

type OnboardingImportFormState struct {
	Database       string `json:"database,omitempty"`
	PipelineName   string `json:"pipeline_name,omitempty"`
	Schema         string `json:"schema,omitempty"`
	Pattern        string `json:"pattern,omitempty"`
	DisableColumns bool   `json:"disable_columns,omitempty"`
}

type OnboardingImportResultState struct {
	Output       string   `json:"output,omitempty"`
	Error        string   `json:"error,omitempty"`
	PipelinePath string   `json:"pipeline_path,omitempty"`
	AssetPaths   []string `json:"asset_paths,omitempty"`
}

type OnboardingSessionState struct {
	Active          bool                         `json:"active"`
	Step            string                       `json:"step,omitempty"`
	SelectedType    string                       `json:"selected_type,omitempty"`
	EnvironmentName string                       `json:"environment_name,omitempty"`
	DraftValues     map[string]any               `json:"draft_values,omitempty"`
	ImportForm      OnboardingImportFormState    `json:"import_form,omitempty"`
	SelectedTables  []string                     `json:"selected_tables,omitempty"`
	ImportResult    *OnboardingImportResultState `json:"import_result,omitempty"`
}

type OnboardingDiscoveryRequest struct {
	EnvironmentName string
	Type            string
	Values          map[string]any
	Database        string
}

type OnboardingDiscoveryResult struct {
	Status           string                  `json:"status"`
	ConnectionType   string                  `json:"connection_type,omitempty"`
	Databases        []string                `json:"databases"`
	SelectedDatabase string                  `json:"selected_database,omitempty"`
	Tables           []SQLDiscoveryTableItem `json:"tables"`
	Error            string                  `json:"error,omitempty"`
}

type OnboardingPathSuggestionsResult struct {
	Status      string           `json:"status"`
	Suggestions []SuggestionItem `json:"suggestions"`
	Error       string           `json:"error,omitempty"`
}

type OnboardingImportResult struct {
	Status       string
	Operation    OperationMetadata
	Output       string
	Error        string
	PipelinePath string
	AssetPaths   []string
	HTTPCode     int
}

type OnboardingService struct {
	workspaceRoot string
	executor      BruinCommandExecutor
	configPath    string
	statePath     string
}

const (
	OnboardingStateConnection = "connection-type"
	OnboardingStateConfig     = "connection-config"
	OnboardingStateImport     = "import"
	OnboardingStateSuccess    = "success"
)

func NewOnboardingService(workspaceRoot, configPath string, executor BruinCommandExecutor) *OnboardingService {
	return &OnboardingService{
		workspaceRoot: workspaceRoot,
		executor:      executor,
		configPath:    configPath,
		statePath:     filepath.Join(workspaceRoot, ".renart-onboarding.json"),
	}
}

func (s *OnboardingService) GetState() (OnboardingSessionState, error) {
	state, err := s.loadState()
	if err == nil {
		return normalizeOnboardingSessionState(state), nil
	}
	if !os.IsNotExist(err) {
		return OnboardingSessionState{}, err
	}

	return s.defaultState(), nil
}

func (s *OnboardingService) UpdateState(state OnboardingSessionState) error {
	normalized := normalizeOnboardingSessionState(state)
	if err := os.MkdirAll(filepath.Dir(s.statePath), 0o755); err != nil {
		return err
	}
	if err := git.EnsureGivenPatternIsInGitignore(afero.NewOsFs(), s.workspaceRoot, filepath.Base(s.statePath)); err != nil {
		return err
	}

	contents, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.statePath, contents, 0o644)
}

func (s *OnboardingService) PreviewDiscovery(ctx context.Context, req OnboardingDiscoveryRequest) (OnboardingDiscoveryResult, int) {
	configService := NewConfigService(s.workspaceRoot, s.configPath)
	cfg, _, err := configService.LoadForEditing()
	if err != nil {
		return OnboardingDiscoveryResult{Status: "error", Databases: []string{}, Tables: []SQLDiscoveryTableItem{}, Error: err.Error()}, 500
	}

	environmentName := strings.TrimSpace(req.EnvironmentName)
	if environmentName == "" {
		environmentName = cfg.SelectedEnvironmentName
	}
	if environmentName == "" {
		environmentName = cfg.DefaultEnvironmentName
	}
	if environmentName == "" {
		environmentName = "default"
	}

	typeName := strings.TrimSpace(req.Type)
	if typeName == "" {
		return OnboardingDiscoveryResult{Status: "error", Databases: []string{}, Tables: []SQLDiscoveryTableItem{}, Error: "connection type is required"}, 400
	}

	values := cloneAnyMap(req.Values)
	selectedDatabase := strings.TrimSpace(req.Database)
	if err := ensureDiscoveryDraftValues(typeName, values, selectedDatabase); err != nil {
		return OnboardingDiscoveryResult{Status: "error", Databases: []string{}, Tables: []SQLDiscoveryTableItem{}, Error: err.Error()}, 400
	}
	selectedDatabase = stringValue(values["database"])
	if typeName == "duckdb" && selectedDatabase == "" {
		selectedDatabase = stringValue(values["path"])
	}

	connectionName := DefaultOnboardingConnectionName(typeName)
	if err := cfg.DeleteConnection(environmentName, connectionName); err != nil && !strings.Contains(err.Error(), "does not exist") {
		return OnboardingDiscoveryResult{Status: "error", Databases: []string{}, Tables: []SQLDiscoveryTableItem{}, Error: err.Error()}, 400
	}
	if err := configService.AddConnection(cfg, UpsertWorkspaceConnectionParams{
		EnvironmentName: environmentName,
		Name:            connectionName,
		Type:            typeName,
		Values:          values,
	}); err != nil {
		return OnboardingDiscoveryResult{Status: "error", Databases: []string{}, Tables: []SQLDiscoveryTableItem{}, Error: err.Error()}, 400
	}

	if err := cfg.SelectEnvironment(environmentName); err != nil {
		return OnboardingDiscoveryResult{Status: "error", Databases: []string{}, Tables: []SQLDiscoveryTableItem{}, Error: err.Error()}, 400
	}

	manager, errs := connection.NewManagerFromConfigWithContext(ctx, cfg)
	if len(errs) > 0 {
		return OnboardingDiscoveryResult{Status: "error", Databases: []string{}, Tables: []SQLDiscoveryTableItem{}, Error: errs[0].Error()}, 400
	}

	conn := manager.GetConnection(connectionName)
	if conn == nil {
		return OnboardingDiscoveryResult{Status: "error", Databases: []string{}, Tables: []SQLDiscoveryTableItem{}, Error: fmt.Sprintf("connection '%s' not found", connectionName)}, 400
	}

	result := OnboardingDiscoveryResult{
		Status:           "ok",
		ConnectionType:   strings.TrimSpace(manager.GetConnectionType(connectionName)),
		Databases:        []string{},
		SelectedDatabase: selectedDatabase,
		Tables:           []SQLDiscoveryTableItem{},
	}

	fetcher, ok := conn.(interface {
		GetDatabases(ctx context.Context) ([]string, error)
	})
	if !ok {
		return OnboardingDiscoveryResult{Status: "error", Databases: []string{}, Tables: []SQLDiscoveryTableItem{}, Error: fmt.Sprintf("connection '%s' does not support discovery", connectionName)}, 400
	}

	databases, err := fetcher.GetDatabases(ctx)
	if err != nil {
		return OnboardingDiscoveryResult{Status: "error", Databases: []string{}, Tables: []SQLDiscoveryTableItem{}, Error: err.Error()}, 400
	}
	sort.Strings(databases)
	result.Databases = databases

	if selectedDatabase == "" {
		return result, 200
	}

	if fetcherWithSchemas, ok := conn.(interface {
		GetTablesWithSchemas(ctx context.Context, databaseName string) (map[string][]string, error)
	}); ok {
		items, err := fetcherWithSchemas.GetTablesWithSchemas(ctx, selectedDatabase)
		if err != nil {
			return OnboardingDiscoveryResult{Status: "error", Databases: databases, Tables: []SQLDiscoveryTableItem{}, Error: err.Error()}, 400
		}
		result.Tables = BuildSQLDiscoveryTableItems(selectedDatabase, items)
		return result, 200
	}

	if tableFetcher, ok := conn.(interface {
		GetTables(ctx context.Context, databaseName string) ([]string, error)
	}); ok {
		items, err := tableFetcher.GetTables(ctx, selectedDatabase)
		if err != nil {
			return OnboardingDiscoveryResult{Status: "error", Databases: databases, Tables: []SQLDiscoveryTableItem{}, Error: err.Error()}, 400
		}
		result.Tables = BuildSQLDiscoveryTableItemsWithoutSchemas(selectedDatabase, items)
		return result, 200
	}

	return OnboardingDiscoveryResult{Status: "error", Databases: databases, Tables: []SQLDiscoveryTableItem{}, Error: fmt.Sprintf("connection '%s' does not support table discovery", connectionName)}, 400
}

func (s *OnboardingService) loadState() (OnboardingSessionState, error) {
	contents, err := os.ReadFile(s.statePath)
	if err != nil {
		return OnboardingSessionState{}, err
	}

	var state OnboardingSessionState
	if err := json.Unmarshal(contents, &state); err != nil {
		return OnboardingSessionState{}, err
	}

	return state, nil
}

func (s *OnboardingService) defaultState() OnboardingSessionState {
	if s.shouldActivateByDefault() {
		return OnboardingSessionState{
			Active: true,
			Step:   OnboardingStateConnection,
			ImportForm: OnboardingImportFormState{
				PipelineName: "analytics",
			},
		}
	}

	return OnboardingSessionState{Active: false}
}

func (s *OnboardingService) shouldActivateByDefault() bool {
	pipelinePaths, err := bruinpath.GetPipelinePaths(s.workspaceRoot, PipelineDefinitionFiles)
	if err == nil && len(pipelinePaths) > 0 {
		return false
	}

	if _, err := os.Stat(s.configPath); err == nil {
		cfg, cfgErr := config.LoadFromFileOrEnv(afero.NewOsFs(), s.configPath)
		if cfgErr == nil {
			for _, env := range cfg.Environments {
				if env.Connections != nil && len(env.Connections.ConnectionsSummaryList()) > 0 {
					return false
				}
			}
		}
	}

	return true
}

func normalizeOnboardingSessionState(state OnboardingSessionState) OnboardingSessionState {
	if !state.Active {
		return OnboardingSessionState{Active: false}
	}

	step := strings.TrimSpace(state.Step)
	switch step {
	case OnboardingStateConnection, OnboardingStateConfig, OnboardingStateImport, OnboardingStateSuccess:
	default:
		step = OnboardingStateConnection
	}

	result := OnboardingSessionState{
		Active:          true,
		Step:            step,
		SelectedType:    strings.TrimSpace(state.SelectedType),
		EnvironmentName: strings.TrimSpace(state.EnvironmentName),
		DraftValues:     cloneAnyMap(state.DraftValues),
		ImportForm: OnboardingImportFormState{
			Database:       strings.TrimSpace(state.ImportForm.Database),
			PipelineName:   strings.TrimSpace(state.ImportForm.PipelineName),
			Schema:         strings.TrimSpace(state.ImportForm.Schema),
			Pattern:        strings.TrimSpace(state.ImportForm.Pattern),
			DisableColumns: state.ImportForm.DisableColumns,
		},
		SelectedTables: append([]string(nil), state.SelectedTables...),
	}

	if result.ImportForm.PipelineName == "" {
		result.ImportForm.PipelineName = "analytics"
	}

	if state.ImportResult != nil {
		result.ImportResult = &OnboardingImportResultState{
			Output:       state.ImportResult.Output,
			Error:        state.ImportResult.Error,
			PipelinePath: strings.TrimSpace(state.ImportResult.PipelinePath),
			AssetPaths:   append([]string(nil), state.ImportResult.AssetPaths...),
		}
	}

	return result
}

func (s *OnboardingService) PathSuggestions(prefix string) (OnboardingPathSuggestionsResult, int) {
	trimmed := strings.TrimSpace(prefix)
	if trimmed == "" {
		trimmed = "./"
	}

	var (
		suggestions []SuggestionItem
		err         error
	)

	if strings.HasPrefix(trimmed, "/") {
		suggestions, err = BuildAbsolutePathSuggestionItems(trimmed)
	} else {
		suggestions, err = BuildWorkspacePathSuggestionItems(s.workspaceRoot, trimmed)
	}
	if err != nil {
		return OnboardingPathSuggestionsResult{Status: "error", Suggestions: []SuggestionItem{}, Error: err.Error()}, 400
	}

	return OnboardingPathSuggestionsResult{Status: "ok", Suggestions: suggestions}, 200
}

func (s *OnboardingService) ImportDatabase(ctx context.Context, req OnboardingImportRequest) OnboardingImportResult {
	connectionName := strings.TrimSpace(req.ConnectionName)
	pipelineName := strings.TrimSpace(req.PipelineName)
	if connectionName == "" {
		return OnboardingImportResult{Status: "error", Error: "connection name is required", HTTPCode: 400}
	}
	if pipelineName == "" {
		return OnboardingImportResult{Status: "error", Error: "pipeline name is required", HTTPCode: 400}
	}

	relPipelinePath := filepath.ToSlash(pipelineName)
	absPipelinePath, err := SafeJoin(s.workspaceRoot, relPipelinePath)
	if err != nil {
		return OnboardingImportResult{Status: "error", Error: err.Error(), HTTPCode: 400}
	}

	if req.CreateIfMissing {
		if err := os.MkdirAll(absPipelinePath, 0o755); err != nil {
			return OnboardingImportResult{Status: "error", Error: err.Error(), HTTPCode: 500}
		}
		pipelineFile := filepath.Join(absPipelinePath, "pipeline.yml")
		if _, statErr := os.Stat(pipelineFile); statErr != nil {
			if os.IsNotExist(statErr) {
				content := fmt.Sprintf("name: %s\nschedule: daily\nstart_date: \"2024-01-01\"\n", filepath.Base(relPipelinePath))
				if writeErr := os.WriteFile(pipelineFile, []byte(content), 0o644); writeErr != nil {
					return OnboardingImportResult{Status: "error", Error: writeErr.Error(), HTTPCode: 500}
				}
			} else {
				return OnboardingImportResult{Status: "error", Error: statErr.Error(), HTTPCode: 500}
			}
		}
	}

	operation := importDatabaseOperation(relPipelinePath, connectionName, req.EnvironmentName)

	output, runErr := s.executor.ImportDatabase(ctx, ImportDatabaseRequest{
		PipelinePath:   relPipelinePath,
		ConnectionName: connectionName,
		Schema:         req.Schema,
		Tables:         req.Tables,
		DisableColumns: req.DisableColumns,
		Environment:    req.EnvironmentName,
		ConfigFilePath: s.configPath,
	})
	if runErr != nil {
		return OnboardingImportResult{
			Status:       "error",
			Operation:    operation,
			Output:       string(output),
			Error:        runErr.Error(),
			PipelinePath: relPipelinePath,
			HTTPCode:     400,
		}
	}

	patchOp := patchOperation("fill-asset-dependencies", relPipelinePath)
	patchOutput, patchErr := s.executor.ApplyPatch(ctx, PatchRequest{
		Operation:  "fill-asset-dependencies",
		TargetPath: relPipelinePath,
	})
	if patchErr != nil {
		return OnboardingImportResult{
			Status:       "error",
			Operation:    patchOp,
			Output:       string(patchOutput),
			Error:        patchErr.Error(),
			PipelinePath: relPipelinePath,
			HTTPCode:     400,
		}
	}

	assetPaths := make([]string, 0, len(req.Tables))
	for _, table := range req.Tables {
		trimmed := strings.TrimSpace(table)
		if trimmed == "" {
			continue
		}
		parts := strings.Split(trimmed, ".")
		if len(parts) >= 2 {
			schemaName := parts[len(parts)-2]
			shortName := parts[len(parts)-1]
			assetPaths = append(assetPaths, filepath.ToSlash(filepath.Join(relPipelinePath, "assets", schemaName, shortName+".asset.yml")))
			continue
		}
		shortName := parts[len(parts)-1]
		assetPaths = append(assetPaths, filepath.ToSlash(filepath.Join(relPipelinePath, "assets", shortName+".asset.yml")))
	}

	return OnboardingImportResult{
		Status:       "ok",
		Operation:    operation,
		Output:       strings.TrimSpace(string(output) + "\n" + string(patchOutput)),
		PipelinePath: relPipelinePath,
		AssetPaths:   assetPaths,
		HTTPCode:     200,
	}
}

func DefaultOnboardingConnectionName(typeName string) string {
	trimmed := strings.TrimSpace(typeName)
	if trimmed == "" {
		return "default-connection"
	}
	return trimmed + "-default"
}

func ensureDiscoveryDraftValues(typeName string, values map[string]any, selectedDatabase string) error {
	trimmedType := strings.TrimSpace(typeName)
	trimmedDatabase := strings.TrimSpace(selectedDatabase)

	if trimmedType == "duckdb" {
		if stringValue(values["path"]) == "" {
			return fmt.Errorf("path is required")
		}
		return nil
	}

	if stringValue(values["database"]) != "" {
		return nil
	}

	if trimmedDatabase != "" {
		values["database"] = trimmedDatabase
		return nil
	}

	switch trimmedType {
	case "postgres":
		values["database"] = "postgres"
	case "redshift":
		values["database"] = "dev"
	}

	return nil
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	trimmed := strings.TrimSpace(fmt.Sprint(value))
	if trimmed == "<nil>" {
		return ""
	}
	return trimmed
}
