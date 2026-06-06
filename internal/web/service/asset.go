package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
)

type AssetUpdateRequest struct {
	Name                *string
	Type                *string
	Content             *string
	MaterializationType *string
	Meta                map[string]string
	Upstreams           []string
}

type AssetMutationResponse struct {
	Status    string `json:"status"`
	AssetID   string `json:"asset_id,omitempty"`
	AssetPath string `json:"asset_path,omitempty"`
}

type StatusResponse struct {
	Status string `json:"status"`
}

type FormatSQLAssetRequest struct {
	Content string
}

type FormatSQLAssetResponse struct {
	Status  string
	AssetID string
	Content string
	Error   string
}

type FormatPythonAssetRequest struct {
	Content string
}

type FormatPythonAssetResponse struct {
	Status  string
	AssetID string
	Content string
	Error   string
}

type PythonDiagnosticsRequest struct {
	Content string
}

type PythonCompletionsRequest struct {
	Content  string
	Line     int
	Column   int
	Snippets bool
}

type PythonDiagnosticsResponse struct {
	Status      string
	AssetID     string
	Diagnostics []PythonDiagnostic
	Error       string
}

type PythonDiagnostic struct {
	ID       string
	Message  string
	Severity string
	Range    *PythonRange
	Display  string
}

type PythonRange struct {
	Start PythonPosition
	End   PythonPosition
}

type PythonPosition struct {
	Line   int
	Column int
}

type PythonCompletionsResponse struct {
	Status      string
	AssetID     string
	Completions []PythonCompletion
	Error       string
}

type PythonCompletion struct {
	Label               string
	Kind                string
	Detail              string
	InsertText          string
	InsertTextFormat    string
	Documentation       string
	ModuleName          string
	AdditionalTextEdits []PythonTextEdit
}

type PythonTextEdit struct {
	Range PythonRange
	Text  string
}

type AssetDependencies struct {
	Fs                                         afero.Fs
	WorkspaceRoot                              string
	Executor                                   BruinCommandExecutor
	ResolveAssetByID                           func(context.Context, string) (string, *pipeline.Pipeline, *pipeline.Asset, error)
	DefaultAssetContent                        func(string, string, string) string
	DerivedAssetContent                        func(string, string, string, string, string) string
	EnsurePythonRequirements                   func(string, string, string) error
	SuppressWatcher                            func(string)
	PushWorkspaceUpdate                        func(context.Context, string, string)
	PushWorkspaceUpdateImmediate               func(context.Context, string, string)
	PushWorkspaceUpdateImmediateWithChangedIDs func(context.Context, string, string, []string)
}

type AssetService struct {
	deps        AssetDependencies
	patchMu     sync.Mutex
	patchTimers map[string]*time.Timer
}

func NewAssetService(deps AssetDependencies) *AssetService {
	return &AssetService{deps: deps, patchTimers: make(map[string]*time.Timer)}
}

func (s *AssetService) fs() afero.Fs {
	if s.deps.Fs != nil {
		return s.deps.Fs
	}
	return afero.NewOsFs()
}

func (s *AssetService) resolver() *WorkspaceResolver {
	return NewWorkspaceResolver(s.deps.WorkspaceRoot, nil)
}

func (s *AssetService) Create(ctx context.Context, pipelineID string, req CreateAssetParams) (AssetMutationResponse, *ServiceAPIError) {
	_, pipelinePath, err := s.resolver().DecodePipelineID(pipelineID)
	if err != nil {
		return AssetMutationResponse{}, newServiceAPIError(400, "invalid_pipeline_id", "invalid pipeline id")
	}
	if req.Name == "" && req.Path == "" && req.SourceAssetID == "" {
		return AssetMutationResponse{}, newServiceAPIError(400, "missing_name_or_path", "name or path is required")
	}

	var sourceAsset *pipeline.Asset
	var sourcePipeline *pipeline.Pipeline
	var sourceConnectionName string
	var sourceRelAssetPath string
	if strings.TrimSpace(req.SourceAssetID) != "" {
		resolvedRelPath, resolvedPipeline, resolvedAsset, resolveErr := s.deps.ResolveAssetByID(ctx, req.SourceAssetID)
		if resolveErr != nil {
			return AssetMutationResponse{}, newServiceAPIError(400, "invalid_source_asset_id", resolveErr.Error())
		}
		if !pipelinePathsReferToSameRoot(resolvedPipeline.DefinitionFile.Path, pipelinePath) {
			return AssetMutationResponse{}, newServiceAPIError(400, "invalid_source_asset", "source asset must belong to the selected pipeline")
		}
		sourceAsset = resolvedAsset
		sourcePipeline = resolvedPipeline
		sourceRelAssetPath = resolvedRelPath
		if conn, connErr := sourcePipeline.GetConnectionNameForAsset(sourceAsset); connErr == nil {
			sourceConnectionName = conn
		}
	}

	assetName := strings.TrimSpace(req.Name)
	if assetName == "" && sourceAsset != nil {
		assetName = deriveDownstreamAssetName(sourceAsset.Name, sourcePipeline)
	}
	if assetName != "" && !strings.Contains(assetName, ".") {
		return AssetMutationResponse{}, newServiceAPIError(400, "missing_asset_prefix", "asset name must include a prefix, for example analytics.orders")
	}

	relAssetPath := req.Path
	if relAssetPath == "" {
		if sourceAsset != nil {
			sourceAbsAssetPath, pathErr := s.resolver().JoinPath(sourceRelAssetPath)
			if pathErr != nil {
				return AssetMutationResponse{}, newServiceAPIError(400, "invalid_source_asset_path", pathErr.Error())
			}
			sourcePipelineRelativeDir, relErr := filepath.Rel(pipelinePath, filepath.Dir(sourceAbsAssetPath))
			if relErr != nil {
				sourcePipelineRelativeDir = "assets"
			}
			assetTypeForPath := strings.TrimSpace(req.Type)
			if assetTypeForPath == "" {
				assetTypeForPath = deriveSQLAssetTypeForSource(sourceAsset, sourcePipeline, sourceConnectionName)
			}
			relAssetPath = filepath.ToSlash(filepath.Join(sourcePipelineRelativeDir, assetNameLeafPath(assetName)+extensionForAssetType(assetTypeForPath)))
		} else {
			relAssetPath = assetPathForInferredName(assetName, extensionForAssetType(req.Type))
		}
	}
	if inferredAssetNameFromPath(relAssetPath) == "" || !strings.Contains(inferredAssetNameFromPath(relAssetPath), ".") {
		return AssetMutationResponse{}, newServiceAPIError(400, "missing_asset_prefix", "asset path must infer a prefixed asset name under assets/<prefix>/")
	}

	absAssetPath, err := SafeJoin(pipelinePath, relAssetPath)
	if err != nil {
		return AssetMutationResponse{}, newServiceAPIError(400, "invalid_asset_path", err.Error())
	}
	fs := s.fs()
	if err := fs.MkdirAll(filepath.Dir(absAssetPath), 0o755); err != nil {
		return AssetMutationResponse{}, newServiceAPIError(500, "asset_dir_create_failed", err.Error())
	}

	assetType := strings.TrimSpace(req.Type)
	if assetType == "" {
		if sourceAsset != nil {
			assetType = deriveSQLAssetTypeForSource(sourceAsset, sourcePipeline, sourceConnectionName)
		} else {
			assetType = inferAssetTypeFromPath(relAssetPath)
		}
	}

	content := req.Content
	if content == "" {
		if sourceAsset != nil {
			content = s.deps.DerivedAssetContent(assetName, assetType, relAssetPath, sourceAsset.Name, sourceConnectionName)
		} else {
			content = s.deps.DefaultAssetContent(assetName, assetType, relAssetPath)
		}
	}

	if err := afero.WriteFile(fs, absAssetPath, []byte(content), 0o644); err != nil {
		return AssetMutationResponse{}, newServiceAPIError(500, "asset_write_failed", err.Error())
	}
	if strings.TrimSpace(req.SeedFileName) != "" {
		seedFileName := filepath.Base(strings.TrimSpace(req.SeedFileName))
		if seedFileName == "." || seedFileName == string(filepath.Separator) || seedFileName == "" {
			return AssetMutationResponse{}, newServiceAPIError(400, "invalid_seed_file_name", "seed file name is invalid")
		}
		seedFilePath := filepath.Join(filepath.Dir(absAssetPath), seedFileName)
		if err := afero.WriteFile(fs, seedFilePath, []byte(req.SeedFileContent), 0o644); err != nil {
			return AssetMutationResponse{}, newServiceAPIError(500, "seed_file_write_failed", err.Error())
		}
	}
	if err := s.deps.EnsurePythonRequirements(absAssetPath, assetType, relAssetPath); err != nil {
		return AssetMutationResponse{}, newServiceAPIError(500, "requirements_write_failed", err.Error())
	}

	relWorkspaceAssetPath, _ := filepath.Rel(s.deps.WorkspaceRoot, absAssetPath)
	assetPath := filepath.ToSlash(relWorkspaceAssetPath)
	if strings.HasSuffix(strings.ToLower(assetPath), ".sql") {
		if err := s.reconcileSQLAssetDependencies(ctx, assetPath); err != nil {
			return AssetMutationResponse{}, newServiceAPIError(500, "asset_dependency_reconcile_failed", err.Error())
		}
	}
	s.deps.SuppressWatcher(assetPath)
	s.deps.PushWorkspaceUpdateImmediate(ctx, "asset.created", assetPath)
	return AssetMutationResponse{Status: "ok", AssetID: EncodeID(assetPath), AssetPath: assetPath}, nil
}

type CreateAssetParams struct {
	Name            string
	Type            string
	Path            string
	Content         string
	SourceAssetID   string
	SeedFileName    string
	SeedFileContent string
}

func (s *AssetService) Update(ctx context.Context, assetID string, req AssetUpdateRequest) (AssetMutationResponse, *ServiceAPIError) {
	relAssetPath, err := DecodeID(assetID)
	if err != nil {
		return AssetMutationResponse{}, newServiceAPIError(400, "invalid_asset_id", "invalid asset id")
	}
	absAssetPath, err := s.resolver().JoinPath(relAssetPath)
	if err != nil {
		return AssetMutationResponse{}, newServiceAPIError(400, "invalid_asset_path", err.Error())
	}

	fs := s.fs()
	originalBytes, err := afero.ReadFile(fs, absAssetPath)
	if err != nil {
		return AssetMutationResponse{}, newServiceAPIError(500, "asset_read_failed", err.Error())
	}
	originalHadExplicitName := assetContentHasExplicitName(string(originalBytes))
	originalExecutable := ExtractExecutableContent(string(originalBytes))
	desiredExecutable := originalExecutable
	if req.Content != nil {
		desiredExecutable = *req.Content
	}
	executableChanged := req.Content != nil && normalizeExecutableContent(desiredExecutable) != normalizeExecutableContent(originalExecutable)

	changedAssetIDs := []string{assetID}
	changedAssetPaths := []string{filepath.ToSlash(relAssetPath)}
	nextAssetID := assetID
	nextRelAssetPath := filepath.ToSlash(relAssetPath)
	inferredRenameRelAssetPath := ""

	if req.Name != nil || req.Type != nil || req.MaterializationType != nil || req.Meta != nil || req.Upstreams != nil {
		_, parsedPipeline, asset, resolveErr := s.deps.ResolveAssetByID(ctx, assetID)
		if resolveErr != nil {
			return AssetMutationResponse{}, newServiceAPIError(400, "asset_resolve_failed", resolveErr.Error())
		}

		originalAssetName := asset.Name
		renamedAsset := false
		if req.Name != nil {
			nextName := strings.TrimSpace(*req.Name)
			if nextName == "" {
				return AssetMutationResponse{}, newServiceAPIError(400, "invalid_asset_name", "asset name cannot be empty")
			}
			if !strings.Contains(nextName, ".") {
				return AssetMutationResponse{}, newServiceAPIError(400, "missing_asset_prefix", "asset name must include a prefix, for example analytics.orders")
			}
			if existing := getAssetByNameCaseInsensitiveLocal(parsedPipeline, nextName); existing != nil && existing.DefinitionFile.Path != asset.DefinitionFile.Path {
				return AssetMutationResponse{}, newServiceAPIError(400, "duplicate_asset_name", fmt.Sprintf("an asset named %q already exists", nextName))
			}
			if nextName != asset.Name {
				asset.Name = nextName
				renamedAsset = true
				if !originalHadExplicitName {
					extension := filepath.Ext(relAssetPath)
					inferredRenameRelAssetPath = filepath.ToSlash(filepath.Join(pipelineRelPathForAsset(relAssetPath), assetPathForInferredName(nextName, extension)))
				}
			}
		}
		if req.Type != nil {
			nextType := strings.TrimSpace(*req.Type)
			if nextType == "" {
				return AssetMutationResponse{}, newServiceAPIError(400, "invalid_asset_type", "asset type cannot be empty")
			}
			asset.Type = pipeline.AssetType(nextType)
		}
		if req.MaterializationType != nil {
			asset.Materialization.Type = pipeline.MaterializationType(strings.ToLower(strings.TrimSpace(*req.MaterializationType)))
		}
		if req.Meta != nil {
			nextMeta := make(map[string]string)
			for rawKey, rawValue := range req.Meta {
				key := strings.TrimSpace(rawKey)
				if key == "" {
					continue
				}
				nextMeta[key] = rawValue
			}
			if len(nextMeta) == 0 {
				asset.Meta = nil
			} else {
				asset.Meta = nextMeta
			}
		}
		if req.Upstreams != nil {
			applyManualAssetUpstreams(asset, parsedPipeline, req.Upstreams)
		}
		if err := asset.Persist(fs, parsedPipeline); err != nil {
			return AssetMutationResponse{}, newServiceAPIError(500, "asset_persist_failed", err.Error())
		}
		if renamedAsset {
			affectedIDs, affectedPaths, refactorErr := s.RefactorDirectDependencies(ctx, parsedPipeline, originalAssetName, asset.Name)
			if refactorErr != nil {
				return AssetMutationResponse{}, newServiceAPIError(500, "asset_rename_refactor_failed", refactorErr.Error())
			}
			changedAssetIDs = appendUniqueStrings(changedAssetIDs, affectedIDs...)
			changedAssetPaths = appendUniqueStrings(changedAssetPaths, affectedPaths...)
		}
	}

	latestBytes, err := afero.ReadFile(fs, absAssetPath)
	if err != nil {
		return AssetMutationResponse{}, newServiceAPIError(500, "asset_read_failed", err.Error())
	}
	mergedContent := MergeExecutableContent(string(latestBytes), desiredExecutable)
	if err := afero.WriteFile(fs, absAssetPath, []byte(mergedContent), 0o644); err != nil {
		return AssetMutationResponse{}, newServiceAPIError(500, "asset_write_failed", err.Error())
	}

	shouldReconcileDependencies := strings.HasSuffix(strings.ToLower(relAssetPath), ".sql") && (executableChanged || req.Upstreams != nil)
	if shouldReconcileDependencies {
		if err := s.reconcileSQLAssetDependencies(ctx, relAssetPath); err != nil {
			return AssetMutationResponse{}, newServiceAPIError(500, "asset_dependency_reconcile_failed", err.Error())
		}
		if executableChanged {
			s.ScheduleSQLPatches(relAssetPath)
		}
	}
	if inferredRenameRelAssetPath != "" && inferredRenameRelAssetPath != filepath.ToSlash(relAssetPath) {
		newAbsAssetPath, pathErr := s.resolver().JoinPath(inferredRenameRelAssetPath)
		if pathErr != nil {
			return AssetMutationResponse{}, newServiceAPIError(400, "invalid_asset_path", pathErr.Error())
		}
		if exists, existsErr := afero.Exists(fs, newAbsAssetPath); existsErr != nil {
			return AssetMutationResponse{}, newServiceAPIError(500, "asset_stat_failed", existsErr.Error())
		} else if exists {
			return AssetMutationResponse{}, newServiceAPIError(400, "asset_path_exists", "an asset already exists at the inferred path")
		}
		currentBytes, readErr := afero.ReadFile(fs, absAssetPath)
		if readErr != nil {
			return AssetMutationResponse{}, newServiceAPIError(500, "asset_read_failed", readErr.Error())
		}
		if mkdirErr := fs.MkdirAll(filepath.Dir(newAbsAssetPath), 0o755); mkdirErr != nil {
			return AssetMutationResponse{}, newServiceAPIError(500, "asset_dir_create_failed", mkdirErr.Error())
		}
		if writeErr := afero.WriteFile(fs, newAbsAssetPath, []byte(removeAssetNameFieldFromContent(string(currentBytes))), 0o644); writeErr != nil {
			return AssetMutationResponse{}, newServiceAPIError(500, "asset_write_failed", writeErr.Error())
		}
		if removeErr := fs.Remove(absAssetPath); removeErr != nil {
			return AssetMutationResponse{}, newServiceAPIError(500, "asset_remove_failed", removeErr.Error())
		}
		nextRelAssetPath = inferredRenameRelAssetPath
		nextAssetID = EncodeID(inferredRenameRelAssetPath)
		changedAssetIDs = appendUniqueStrings(changedAssetIDs, nextAssetID)
		changedAssetPaths = appendUniqueStrings(changedAssetPaths, inferredRenameRelAssetPath)
	}
	for _, changedPath := range changedAssetPaths {
		s.deps.SuppressWatcher(changedPath)
	}
	s.deps.PushWorkspaceUpdateImmediateWithChangedIDs(ctx, "asset.updated", nextRelAssetPath, changedAssetIDs)
	return AssetMutationResponse{Status: "ok", AssetID: nextAssetID, AssetPath: nextRelAssetPath}, nil
}

func (s *AssetService) Delete(ctx context.Context, assetID string) (StatusResponse, *ServiceAPIError) {
	relAssetPath, err := DecodeID(assetID)
	if err != nil {
		return StatusResponse{}, newServiceAPIError(400, "invalid_asset_id", "invalid asset id")
	}
	absAssetPath, err := s.resolver().JoinPath(relAssetPath)
	if err != nil {
		return StatusResponse{}, newServiceAPIError(400, "invalid_asset_path", err.Error())
	}
	if err := s.fs().Remove(absAssetPath); err != nil {
		return StatusResponse{}, newServiceAPIError(500, "asset_delete_failed", err.Error())
	}
	s.deps.SuppressWatcher(relAssetPath)
	s.deps.PushWorkspaceUpdateImmediate(ctx, "asset.deleted", relAssetPath)
	return StatusResponse{Status: "ok"}, nil
}

func extensionForAssetType(assetType string) string {
	lowered := strings.ToLower(strings.TrimSpace(assetType))
	switch {
	case strings.HasSuffix(lowered, ".seed"):
		return ".asset.yml"
	case strings.HasSuffix(lowered, ".py") || strings.Contains(lowered, "python"):
		return ".py"
	case strings.HasSuffix(lowered, ".sql") || strings.Contains(lowered, "sql"):
		return ".sql"
	default:
		return ".sql"
	}
}

func inferAssetTypeFromPath(path string) string {
	lowered := strings.ToLower(strings.TrimSpace(path))
	switch filepath.Ext(lowered) {
	case ".py":
		return "python"
	case ".sql":
		return "duckdb.sql"
	default:
		return "duckdb.sql"
	}
}

func deriveDownstreamAssetName(sourceAssetName string, parsedPipeline *pipeline.Pipeline) string {
	trimmed := strings.TrimSpace(sourceAssetName)
	if trimmed == "" {
		trimmed = "asset"
	}

	prefix := ""
	leaf := trimmed
	if lastDot := strings.LastIndex(trimmed, "."); lastDot >= 0 {
		prefix = trimmed[:lastDot]
		leaf = trimmed[lastDot+1:]
	}

	baseLeaf := SlugUnderscore(leaf)
	if baseLeaf == "" {
		baseLeaf = "asset"
	}
	baseLeaf += "_child"

	buildCandidate := func(index int) string {
		candidate := fmt.Sprintf("%s_%d", baseLeaf, index)
		if prefix == "" {
			return candidate
		}
		return prefix + "." + candidate
	}

	if parsedPipeline == nil {
		return buildCandidate(1)
	}

	exists := func(name string) bool {
		for _, asset := range parsedPipeline.Assets {
			if asset != nil && strings.EqualFold(strings.TrimSpace(asset.Name), name) {
				return true
			}
		}
		return false
	}

	for index := 1; index < 1000; index += 1 {
		candidate := buildCandidate(index)
		if !exists(candidate) {
			return candidate
		}
	}

	return buildCandidate(1)
}

func deriveSQLAssetTypeForSource(sourceAsset *pipeline.Asset, parsedPipeline *pipeline.Pipeline, sourceConnectionName string) string {
	if sourceAsset != nil {
		assetType := strings.TrimSpace(string(sourceAsset.Type))
		if strings.Contains(strings.ToLower(assetType), "sql") {
			return assetType
		}
		if strings.EqualFold(assetType, "ingestr") {
			destination := strings.TrimSpace(sourceAsset.Parameters["destination"])
			if destination != "" {
				if assetType, ok := sqlAssetTypeForIngestrDestination(destination); ok {
					return assetType
				}
				if assetType, ok := sqlAssetTypeForConnectionName(parsedPipeline, destination); ok {
					return assetType
				}
			}
			destinationConnection := strings.TrimSpace(sourceAsset.Parameters["destination_connection"])
			if assetType, ok := sqlAssetTypeForConnectionName(parsedPipeline, destinationConnection); ok {
				return assetType
			}
		}
	}
	if parsedPipeline != nil {
		for _, current := range parsedPipeline.Assets {
			if current == nil {
				continue
			}
			assetType := strings.TrimSpace(string(current.Type))
			if strings.Contains(strings.ToLower(assetType), "sql") {
				return assetType
			}
		}
	}
	return "duckdb.sql"
}

func DefaultDerivedSQLAssetContent(assetName, assetType, assetPath, sourceAssetName, connectionName string) string {
	header := fmt.Sprintf("/* @bruin\n\ntype: %s\nmaterialization:\n  type: view\n\n@bruin */\n\n", assetType)
	queryTarget := sourceAssetName
	if strings.TrimSpace(queryTarget) == "" {
		queryTarget = strings.TrimSuffix(filepath.Base(assetPath), filepath.Ext(assetPath))
	}
	return header + fmt.Sprintf("select * from %s\n", queryTarget)
}

func EnsurePythonRequirementsFile(absAssetPath, assetType, relAssetPath string) error {
	loweredType := strings.ToLower(strings.TrimSpace(assetType))
	loweredPath := strings.ToLower(strings.TrimSpace(relAssetPath))
	if !strings.HasSuffix(loweredPath, ".py") && !strings.Contains(loweredType, "python") {
		return nil
	}
	requirementsPath := filepath.Join(filepath.Dir(absAssetPath), "requirements.txt")
	fs := afero.NewOsFs()
	if exists, err := afero.Exists(fs, requirementsPath); err != nil {
		return err
	} else if exists {
		return nil
	}
	return afero.WriteFile(fs, requirementsPath, []byte("pandas\n"), 0o644)
}

func defaultDerivedSQLAssetContent(assetName, assetType, assetPath, sourceAssetName, connectionName string) string {
	return DefaultDerivedSQLAssetContent(assetName, assetType, assetPath, sourceAssetName, connectionName)
}

func ensurePythonRequirementsFile(absAssetPath, assetType, relAssetPath string) error {
	return EnsurePythonRequirementsFile(absAssetPath, assetType, relAssetPath)
}
