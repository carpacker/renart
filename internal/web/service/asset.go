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
	Name                    *string           `json:"name,omitempty"`
	Type                    *string           `json:"type,omitempty"`
	Content                 *string           `json:"content,omitempty"`
	MaterializationType     *string           `json:"materialization_type,omitempty"`
	MaterializationStrategy *string           `json:"materialization_strategy,omitempty"`
	IncrementalKey          *string           `json:"incremental_key,omitempty"`
	Owner                   *string           `json:"owner,omitempty"`
	Tags                    []string          `json:"tags,omitempty"`
	Meta                    map[string]string `json:"meta,omitempty"`
	Upstreams               []string          `json:"upstreams,omitempty"`
	Parameters              map[string]string `json:"parameters,omitempty"`
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
	Content string `json:"content"`
}

type FormatSQLAssetResponse struct {
	Status  string `json:"status"`
	AssetID string `json:"asset_id"`
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

type FormatPythonAssetRequest struct {
	Content string `json:"content"`
}

type FormatPythonAssetResponse struct {
	Status  string `json:"status"`
	AssetID string `json:"asset_id"`
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

type PythonDiagnosticsRequest struct {
	Content string `json:"content"`
}

type PythonCompletionsRequest struct {
	Content  string `json:"content"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Snippets bool   `json:"snippets"`
}

type PythonPositionRequest struct {
	Content string `json:"content"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
}

type PythonDiagnosticsResponse struct {
	Status      string             `json:"status"`
	AssetID     string             `json:"asset_id"`
	Diagnostics []PythonDiagnostic `json:"diagnostics,omitempty"`
	Error       string             `json:"error,omitempty"`
}

type PythonDiagnostic struct {
	ID       string       `json:"id"`
	Message  string       `json:"message"`
	Severity string       `json:"severity"`
	Range    *PythonRange `json:"range,omitempty"`
	Display  string       `json:"display,omitempty"`
}

type PythonRange struct {
	Start PythonPosition `json:"start"`
	End   PythonPosition `json:"end"`
}

type PythonPosition struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type PythonCompletionsResponse struct {
	Status      string             `json:"status"`
	AssetID     string             `json:"asset_id"`
	Completions []PythonCompletion `json:"completions,omitempty"`
	Error       string             `json:"error,omitempty"`
}

type PythonCompletion struct {
	Label               string           `json:"label"`
	Kind                string           `json:"kind,omitempty"`
	Detail              string           `json:"detail,omitempty"`
	InsertText          string           `json:"insert_text,omitempty"`
	InsertTextFormat    string           `json:"insert_text_format"`
	Documentation       string           `json:"documentation,omitempty"`
	ModuleName          string           `json:"module_name,omitempty"`
	AdditionalTextEdits []PythonTextEdit `json:"additional_text_edits,omitempty"`
}

type PythonTextEdit struct {
	Range PythonRange `json:"range"`
	Text  string      `json:"text"`
}

type PythonHoverResponse struct {
	Status  string       `json:"status"`
	AssetID string       `json:"asset_id"`
	Hover   *PythonHover `json:"hover,omitempty"`
	Error   string       `json:"error,omitempty"`
}

type PythonHover struct {
	Contents string       `json:"contents"`
	Range    *PythonRange `json:"range,omitempty"`
}

type PythonSignatureHelpResponse struct {
	Status        string               `json:"status"`
	AssetID       string               `json:"asset_id"`
	SignatureHelp *PythonSignatureHelp `json:"signature_help,omitempty"`
	Error         string               `json:"error,omitempty"`
}

type PythonSignatureHelp struct {
	Signatures      []PythonSignature `json:"signatures"`
	ActiveSignature *int              `json:"active_signature,omitempty"`
	ActiveParameter *int              `json:"active_parameter,omitempty"`
}

type PythonSignature struct {
	Label           string                     `json:"label"`
	Documentation   string                     `json:"documentation,omitempty"`
	Parameters      []PythonSignatureParameter `json:"parameters"`
	ActiveParameter *int                       `json:"active_parameter,omitempty"`
}

type PythonSignatureParameter struct {
	Label         string `json:"label"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	Documentation string `json:"documentation,omitempty"`
}

type PythonGotoDefinitionResponse struct {
	Status  string             `json:"status"`
	AssetID string             `json:"asset_id"`
	Targets []PythonGotoTarget `json:"targets,omitempty"`
	Error   string             `json:"error,omitempty"`
}

type PythonGotoTarget struct {
	Path       string      `json:"path"`
	FocusRange PythonRange `json:"focus_range"`
	FullRange  PythonRange `json:"full_range"`
}

type AssetDependencies struct {
	Fs                                         afero.Fs
	WorkspaceRoot                              string
	Executor                                   BruinCommandExecutor
	ResolveAssetByID                           func(context.Context, string) (string, *pipeline.Pipeline, *pipeline.Asset, error)
	DefaultAssetContent                        func(string, string, string) string
	DerivedAssetContent                        func(string, string, string, string, string) string
	EnsurePythonProject                        func(string, string, string) error
	SuppressWatcher                            func(string)
	PushWorkspaceUpdate                        func(context.Context, string, string)
	PushWorkspaceUpdateImmediate               func(context.Context, string, string)
	PushWorkspaceUpdateImmediateWithChangedIDs func(context.Context, string, string, []string)
	PushAssetContentUpdateImmediate            func(string, string, []string, string)
}

type AssetService struct {
	deps                    AssetDependencies
	patchMu                 sync.Mutex
	patchTimers             map[string]*time.Timer
	assetFileMu             sync.Mutex
	assetFileLocks          map[string]*sync.Mutex
	pythonPackageMountMu    sync.Mutex
	pythonPackageMountCache map[string]pythonPackageMountCacheEntry
	pythonTySessionMu       sync.Mutex
	pythonTySessionFiles    map[string]string
	pythonTyCallCount       int
}

func NewAssetService(deps AssetDependencies) *AssetService {
	return &AssetService{
		deps:                    deps,
		patchTimers:             make(map[string]*time.Timer),
		assetFileLocks:          make(map[string]*sync.Mutex),
		pythonPackageMountCache: make(map[string]pythonPackageMountCacheEntry),
		pythonTySessionFiles:    make(map[string]string),
	}
}

// lockAssetFile serializes read-modify-write access to a single asset file.
// Without it, an interactive content save (Update) and the async SQL patch
// (RunSQLPatches) — or two overlapping saves — can interleave their truncate +
// write cycles, letting one goroutine read a torn/empty file, lose the @bruin
// header, and persist a headerless file that makes the asset disappear.
// Returns the unlock function. Keyed by the cleaned absolute path.
func (s *AssetService) lockAssetFile(absPath string) func() {
	key := filepath.Clean(absPath)
	s.assetFileMu.Lock()
	if s.assetFileLocks == nil {
		s.assetFileLocks = make(map[string]*sync.Mutex)
	}
	mu, ok := s.assetFileLocks[key]
	if !ok {
		mu = &sync.Mutex{}
		s.assetFileLocks[key] = mu
	}
	s.assetFileMu.Unlock()

	mu.Lock()
	return mu.Unlock
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

func (s *AssetService) Create(ctx context.Context, pipelineID string, req CreateAssetParams) (AssetMutationResponse, *APIError) {
	_, pipelinePath, err := s.resolver().DecodePipelineID(pipelineID)
	if err != nil {
		return AssetMutationResponse{}, newAPIError(400, "invalid_pipeline_id", "invalid pipeline id")
	}
	if req.Name == "" && req.Path == "" && req.SourceAssetID == "" {
		return AssetMutationResponse{}, newAPIError(400, "missing_name_or_path", "name or path is required")
	}

	var sourceAsset *pipeline.Asset
	var sourcePipeline *pipeline.Pipeline
	var sourceConnectionName string
	if strings.TrimSpace(req.SourceAssetID) != "" {
		_, resolvedPipeline, resolvedAsset, resolveErr := s.deps.ResolveAssetByID(ctx, req.SourceAssetID)
		if resolveErr != nil {
			return AssetMutationResponse{}, newAPIError(400, "invalid_source_asset_id", resolveErr.Error())
		}
		if !pipelinePathsReferToSameRoot(resolvedPipeline.DefinitionFile.Path, pipelinePath) {
			return AssetMutationResponse{}, newAPIError(400, "invalid_source_asset", "source asset must belong to the selected pipeline")
		}
		sourceAsset = resolvedAsset
		sourcePipeline = resolvedPipeline
		if conn, connErr := sourcePipeline.GetConnectionNameForAsset(sourceAsset); connErr == nil {
			sourceConnectionName = conn
		}
	}

	assetName := strings.TrimSpace(req.Name)
	if assetName == "" && sourceAsset != nil {
		assetName = deriveDownstreamAssetName(sourceAsset.Name, sourcePipeline)
	}
	if assetName != "" && !strings.Contains(assetName, ".") {
		return AssetMutationResponse{}, newAPIError(400, "missing_asset_prefix", "asset name must include a prefix, for example analytics.orders")
	}

	relAssetPath := req.Path
	if relAssetPath == "" {
		if sourceAsset != nil {
			assetTypeForPath := strings.TrimSpace(req.Type)
			if assetTypeForPath == "" {
				assetTypeForPath = deriveSQLAssetTypeForSource(sourceAsset, sourcePipeline, sourceConnectionName)
			}
			// Root the path at the pipeline's assets/ dir using the full prefixed
			// name so bruin can infer it back from the path (SQL assets carry no
			// explicit name:). Joining the source's dir with only the leaf dropped
			// the prefix when the source lived directly under assets/ (e.g. a
			// notebook-promoted asset), yielding an unprefixed, invalid path.
			relAssetPath = assetPathForInferredName(assetName, extensionForAssetType(assetTypeForPath))
		} else {
			relAssetPath = assetPathForInferredName(assetName, extensionForAssetType(req.Type))
		}
	}
	if inferredAssetNameFromPath(relAssetPath) == "" || !strings.Contains(inferredAssetNameFromPath(relAssetPath), ".") {
		return AssetMutationResponse{}, newAPIError(400, "missing_asset_prefix", "asset path must infer a prefixed asset name under assets/<prefix>/")
	}

	absAssetPath, err := SafeJoin(pipelinePath, relAssetPath)
	if err != nil {
		return AssetMutationResponse{}, newAPIError(400, "invalid_asset_path", err.Error())
	}
	fs := s.fs()
	if err := fs.MkdirAll(filepath.Dir(absAssetPath), 0o755); err != nil {
		return AssetMutationResponse{}, newAPIError(500, "asset_dir_create_failed", err.Error())
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

	// Sling assets are now a single flat-parameter .asset.yml (the DefaultAsset/
	// DerivedAsset content producers emit that shape), so they write like any
	// other single-file asset — no .sling.yml replication sidecar.
	if err := afero.WriteFile(fs, absAssetPath, []byte(content), 0o644); err != nil {
		return AssetMutationResponse{}, newAPIError(500, "asset_write_failed", err.Error())
	}
	if strings.TrimSpace(req.SeedFileName) != "" {
		seedFileName := filepath.Base(strings.TrimSpace(req.SeedFileName))
		if seedFileName == "." || seedFileName == string(filepath.Separator) || seedFileName == "" {
			return AssetMutationResponse{}, newAPIError(400, "invalid_seed_file_name", "seed file name is invalid")
		}
		seedFilePath := filepath.Join(filepath.Dir(absAssetPath), seedFileName)
		if err := afero.WriteFile(fs, seedFilePath, []byte(req.SeedFileContent), 0o644); err != nil {
			return AssetMutationResponse{}, newAPIError(500, "seed_file_write_failed", err.Error())
		}
	}
	if err := s.deps.EnsurePythonProject(absAssetPath, assetType, relAssetPath); err != nil {
		return AssetMutationResponse{}, newAPIError(500, "pyproject_write_failed", err.Error())
	}
	// A downstream Python asset reads its upstream via the Bruin Python SDK
	// (`from bruin import query`), provided by the bruin-sdk package, so declare
	// it as a dependency for the new asset. Best-effort: a failure here just
	// surfaces later as a missing-dependency hint.
	if sourceAsset != nil && (strings.Contains(strings.ToLower(assetType), "python") || strings.HasSuffix(strings.ToLower(relAssetPath), ".py")) {
		_, _ = s.addAssetPyprojectDependency(absAssetPath, "bruin-sdk")
	}

	relWorkspaceAssetPath, _ := filepath.Rel(s.deps.WorkspaceRoot, absAssetPath)
	assetPath := filepath.ToSlash(relWorkspaceAssetPath)
	if strings.HasSuffix(strings.ToLower(assetPath), ".sql") {
		if err := s.reconcileSQLAssetDependencies(ctx, assetPath); err != nil {
			return AssetMutationResponse{}, newAPIError(500, "asset_dependency_reconcile_failed", err.Error())
		}
	} else if isSlingAssetType(assetType) {
		// Auto-infer the upstream from the source mapping (best-effort: a newly
		// created skeleton often has no resolvable source yet).
		if err := s.reconcileSlingAssetDependencies(ctx, assetPath); err != nil {
			_ = err
		}
	}
	s.deps.SuppressWatcher(assetPath)
	s.deps.PushWorkspaceUpdateImmediate(ctx, "asset.created", assetPath)
	return AssetMutationResponse{Status: "ok", AssetID: EncodeID(assetPath), AssetPath: assetPath}, nil
}

type CreateAssetParams struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	Path            string `json:"path"`
	Content         string `json:"content"`
	SourceAssetID   string `json:"source_asset_id"`
	SeedFileName    string `json:"seed_file_name"`
	SeedFileContent string `json:"seed_file_content"`
}

func (s *AssetService) Update(ctx context.Context, assetID string, req AssetUpdateRequest) (AssetMutationResponse, *APIError) {
	relAssetPath, err := DecodeID(assetID)
	if err != nil {
		return AssetMutationResponse{}, newAPIError(400, "invalid_asset_id", "invalid asset id")
	}
	absAssetPath, err := s.resolver().JoinPath(relAssetPath)
	if err != nil {
		return AssetMutationResponse{}, newAPIError(400, "invalid_asset_path", err.Error())
	}

	// Serialize the whole read-modify-write against any concurrent save or the
	// async SQL patch for the same file, so neither can observe a torn write.
	unlock := s.lockAssetFile(absAssetPath)
	defer unlock()

	fs := s.fs()
	originalBytes, err := afero.ReadFile(fs, absAssetPath)
	if err != nil {
		return AssetMutationResponse{}, newAPIError(500, "asset_read_failed", err.Error())
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
	// Set when a YAML-defined asset has already been written through the
	// node-preserving codec, so we skip the executable-file rewrite below (which
	// would clobber the codec's output for api assets whose definition == executable).
	persistedViaCodec := false
	slingAssetUpdated := false

	if req.Name != nil || req.Type != nil || req.MaterializationType != nil || req.MaterializationStrategy != nil || req.IncrementalKey != nil || req.Owner != nil || req.Tags != nil || req.Meta != nil || req.Upstreams != nil || req.Parameters != nil {
		_, parsedPipeline, asset, resolveErr := s.deps.ResolveAssetByID(ctx, assetID)
		if resolveErr != nil {
			return AssetMutationResponse{}, newAPIError(400, "asset_resolve_failed", resolveErr.Error())
		}
		if isSlingAsset(asset) {
			originalHadExplicitName = true
			slingAssetUpdated = true
		}

		originalAssetName := asset.Name
		renamedAsset := false
		if req.Name != nil {
			nextName := strings.TrimSpace(*req.Name)
			if nextName == "" {
				return AssetMutationResponse{}, newAPIError(400, "invalid_asset_name", "asset name cannot be empty")
			}
			if !strings.Contains(nextName, ".") {
				return AssetMutationResponse{}, newAPIError(400, "missing_asset_prefix", "asset name must include a prefix, for example analytics.orders")
			}
			if existing := getAssetByNameCaseInsensitiveLocal(parsedPipeline, nextName); existing != nil && existing.DefinitionFile.Path != asset.DefinitionFile.Path {
				return AssetMutationResponse{}, newAPIError(400, "duplicate_asset_name", fmt.Sprintf("an asset named %q already exists", nextName))
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
				return AssetMutationResponse{}, newAPIError(400, "invalid_asset_type", "asset type cannot be empty")
			}
			asset.Type = pipeline.AssetType(nextType)
		}
		if req.MaterializationType != nil {
			asset.Materialization.Type = pipeline.MaterializationType(strings.ToLower(strings.TrimSpace(*req.MaterializationType)))
		}
		if req.MaterializationStrategy != nil {
			asset.Materialization.Strategy = pipeline.MaterializationStrategy(strings.ToLower(strings.TrimSpace(*req.MaterializationStrategy)))
		}
		if req.IncrementalKey != nil {
			asset.Materialization.IncrementalKey = strings.TrimSpace(*req.IncrementalKey)
		}
		if req.Owner != nil {
			asset.Owner = strings.TrimSpace(*req.Owner)
		}
		if req.Tags != nil {
			nextTags := make([]string, 0, len(req.Tags))
			for _, tag := range req.Tags {
				if trimmed := strings.TrimSpace(tag); trimmed != "" {
					nextTags = append(nextTags, trimmed)
				}
			}
			asset.Tags = nextTags
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
		if req.Parameters != nil {
			nextParameters := pipeline.EmptyStringMap{}
			for rawKey, rawValue := range req.Parameters {
				key := strings.TrimSpace(rawKey)
				if key == "" {
					continue
				}
				nextParameters[key] = rawValue
			}
			asset.Parameters = nextParameters
		}
		if isYAMLDefinedAsset(asset) {
			// api/sling/ingestr/plain-yaml: overlay managed fields onto the
			// definition file, preserving the request spec, sling replication
			// config and any other unmanaged content (and columns, which the old
			// per-type writers silently dropped).
			if apiErr := s.persistYAMLAssetPreservingInferredName(asset); apiErr != nil {
				return AssetMutationResponse{}, apiErr
			}
			persistedViaCodec = true
			if relDefinitionPath, relErr := filepath.Rel(s.deps.WorkspaceRoot, asset.DefinitionFile.Path); relErr == nil {
				changedAssetPaths = appendUniqueStrings(changedAssetPaths, filepath.ToSlash(relDefinitionPath))
			}
		} else if err := asset.Persist(fs, parsedPipeline); err != nil {
			return AssetMutationResponse{}, newAPIError(500, "asset_persist_failed", err.Error())
		}
		if renamedAsset {
			affectedIDs, affectedPaths, refactorErr := s.RefactorDirectDependencies(ctx, parsedPipeline, originalAssetName, asset.Name)
			if refactorErr != nil {
				return AssetMutationResponse{}, newAPIError(500, "asset_rename_refactor_failed", refactorErr.Error())
			}
			changedAssetIDs = appendUniqueStrings(changedAssetIDs, affectedIDs...)
			changedAssetPaths = appendUniqueStrings(changedAssetPaths, affectedPaths...)
		}
	}

	// A YAML-defined asset was fully written by the codec above; rewriting the
	// executable file here would overwrite that (for api the executable IS the
	// definition file) or needlessly touch the sling replication config.
	if !persistedViaCodec {
		latestBytes, err := afero.ReadFile(fs, absAssetPath)
		if err != nil {
			return AssetMutationResponse{}, newAPIError(500, "asset_read_failed", err.Error())
		}
		mergedContent := desiredExecutable
		if !isSlingExecutablePath(relAssetPath) && !isAPIExecutablePath(relAssetPath) {
			mergedContent = MergeExecutableContent(string(latestBytes), desiredExecutable)
		}
		if err := afero.WriteFile(fs, absAssetPath, []byte(mergedContent), 0o644); err != nil {
			return AssetMutationResponse{}, newAPIError(500, "asset_write_failed", err.Error())
		}
	}

	shouldReconcileDependencies := strings.HasSuffix(strings.ToLower(relAssetPath), ".sql") && (executableChanged || req.Upstreams != nil)
	if shouldReconcileDependencies {
		// Dependency reconciliation parses the SQL, which routinely fails while
		// the user is mid-typing an incomplete query. The content is already
		// saved, so treat this as best-effort: don't fail the save (which would
		// surface as a spurious error and leave the editor thinking it lost the
		// write). The async patch retries reconciliation once the query parses.
		if err := s.reconcileSQLAssetDependencies(ctx, relAssetPath); err != nil {
			_ = err // best-effort: keep the saved content, the async patch retries
		}
		if executableChanged {
			s.ScheduleSQLPatches(relAssetPath)
		}
	}
	if slingAssetUpdated {
		// Re-infer the sling upstream from the (possibly edited) source mapping.
		// Best-effort: a save with an unresolved source should still succeed.
		if err := s.reconcileSlingAssetDependencies(ctx, relAssetPath); err != nil {
			_ = err
		}
	}
	if inferredRenameRelAssetPath != "" && inferredRenameRelAssetPath != filepath.ToSlash(relAssetPath) {
		newAbsAssetPath, pathErr := s.resolver().JoinPath(inferredRenameRelAssetPath)
		if pathErr != nil {
			return AssetMutationResponse{}, newAPIError(400, "invalid_asset_path", pathErr.Error())
		}
		if exists, existsErr := afero.Exists(fs, newAbsAssetPath); existsErr != nil {
			return AssetMutationResponse{}, newAPIError(500, "asset_stat_failed", existsErr.Error())
		} else if exists {
			return AssetMutationResponse{}, newAPIError(400, "asset_path_exists", "an asset already exists at the inferred path")
		}
		currentBytes, readErr := afero.ReadFile(fs, absAssetPath)
		if readErr != nil {
			return AssetMutationResponse{}, newAPIError(500, "asset_read_failed", readErr.Error())
		}
		if mkdirErr := fs.MkdirAll(filepath.Dir(newAbsAssetPath), 0o755); mkdirErr != nil {
			return AssetMutationResponse{}, newAPIError(500, "asset_dir_create_failed", mkdirErr.Error())
		}
		if writeErr := afero.WriteFile(fs, newAbsAssetPath, []byte(removeAssetNameFieldFromContent(string(currentBytes))), 0o644); writeErr != nil {
			return AssetMutationResponse{}, newAPIError(500, "asset_write_failed", writeErr.Error())
		}
		if removeErr := fs.Remove(absAssetPath); removeErr != nil {
			return AssetMutationResponse{}, newAPIError(500, "asset_remove_failed", removeErr.Error())
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

func (s *AssetService) Delete(ctx context.Context, assetID string) (StatusResponse, *APIError) {
	relAssetPath, err := DecodeID(assetID)
	if err != nil {
		return StatusResponse{}, newAPIError(400, "invalid_asset_id", "invalid asset id")
	}
	absAssetPath, err := s.resolver().JoinPath(relAssetPath)
	if err != nil {
		return StatusResponse{}, newAPIError(400, "invalid_asset_path", err.Error())
	}
	pathsToRemove := []string{absAssetPath}
	pathsToSuppress := []string{filepath.ToSlash(relAssetPath)}
	if isSlingExecutablePath(relAssetPath) && s.deps.ResolveAssetByID != nil {
		resolvedRelPath, _, asset, resolveErr := s.deps.ResolveAssetByID(ctx, assetID)
		if resolveErr == nil && isSlingAsset(asset) && asset.DefinitionFile.Path != "" {
			pathsToRemove = appendUniqueStrings(pathsToRemove, asset.DefinitionFile.Path)
			if relDefinitionPath, relErr := filepath.Rel(s.deps.WorkspaceRoot, asset.DefinitionFile.Path); relErr == nil {
				pathsToSuppress = appendUniqueStrings(pathsToSuppress, filepath.ToSlash(relDefinitionPath))
			}
			pathsToSuppress = appendUniqueStrings(pathsToSuppress, filepath.ToSlash(resolvedRelPath))
		}
	}

	fs := s.fs()
	for _, pathToRemove := range pathsToRemove {
		if err := fs.Remove(pathToRemove); err != nil {
			return StatusResponse{}, newAPIError(500, "asset_delete_failed", err.Error())
		}
	}
	for _, changedPath := range pathsToSuppress {
		s.deps.SuppressWatcher(changedPath)
	}
	s.deps.PushWorkspaceUpdateImmediate(ctx, "asset.deleted", relAssetPath)
	return StatusResponse{Status: "ok"}, nil
}

func extensionForAssetType(assetType string) string {
	lowered := strings.ToLower(strings.TrimSpace(assetType))
	switch {
	case isSlingAssetType(lowered):
		return ".asset.yml"
	case isAPIAssetType(lowered):
		return ".asset.yml"
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
	queryTarget := sourceAssetName
	if strings.TrimSpace(queryTarget) == "" {
		queryTarget = strings.TrimSuffix(filepath.Base(assetPath), filepath.Ext(assetPath))
	}
	lowered := strings.ToLower(strings.TrimSpace(assetType))

	// Python downstream: a Bruin Python asset that reads the upstream table with
	// the Bruin Python SDK (query()) and returns it, declaring the dependency so
	// lineage/ordering are correct. The SDK is the bruin-sdk package (the editor
	// flags it as a missing import to add); it requires Python >=3.10, which the
	// seeded pyproject targets.
	if lowered == "python" || strings.HasSuffix(strings.ToLower(assetPath), ".py") {
		connectionLine := ""
		if strings.TrimSpace(connectionName) != "" {
			connectionLine = fmt.Sprintf("connection: %s\n", strings.TrimSpace(connectionName))
		}
		return fmt.Sprintf("\"\"\" @bruin\n\nname: %s\ntype: python\n%smaterialization:\n  type: table\n\ndepends:\n  - %s\n\n@bruin \"\"\"\n\nfrom bruin import query\n\n\ndef materialize():\n    return query(\"select * from %s\")\n", assetName, connectionLine, sourceAssetName, queryTarget)
	}

	// Sling downstream: a single flat-parameter .asset.yml that reads the upstream
	// asset as its source. bruin parses `parameters` as flat strings, so the whole
	// replication intent stays in one bruin-loadable file (no .sling.yml sidecar).
	if isSlingAssetType(assetType) {
		object := assetNameLeafPath(assetName)
		if strings.TrimSpace(object) == "" {
			object = "asset"
		}
		sourceConnection := strings.TrimSpace(connectionName)
		if sourceConnection == "" {
			sourceConnection = "your_source_connection"
		}
		return fmt.Sprintf("type: sling\n\ndepends:\n  - %s\n\nparameters:\n  source_connection: %s\n  source_table: %s\n  destination_connection: your_destination_connection\n  destination_table: public.%s\n  mode: full-refresh\n", sourceAssetName, sourceConnection, sourceAssetName, object)
	}

	header := fmt.Sprintf("/* @bruin\n\ntype: %s\nmaterialization:\n  type: view\n\n@bruin */\n\n", assetType)
	return header + fmt.Sprintf("select * from %s\n", queryTarget)
}

// EnsurePythonProjectFile seeds a default pyproject.toml (pandas) beside a new
// Python asset so it runs out of the box with uv. It is a no-op when any
// ancestor up to the workspace root already declares Python dependencies
// (pyproject.toml or a legacy requirements.txt), avoiding redundant manifests.
func EnsurePythonProjectFile(absAssetPath, assetType, relAssetPath string) error {
	loweredType := strings.ToLower(strings.TrimSpace(assetType))
	loweredPath := strings.ToLower(strings.TrimSpace(relAssetPath))
	if !strings.HasSuffix(loweredPath, ".py") && !strings.Contains(loweredType, "python") {
		return nil
	}
	startDir := filepath.Dir(absAssetPath)
	workspaceRoot := filepath.Clean(strings.TrimSuffix(absAssetPath, filepath.FromSlash(relAssetPath)))
	if nearestPythonDependencyFile(startDir, workspaceRoot, pyprojectFile) != "" ||
		nearestPythonDependencyFile(startDir, workspaceRoot, "requirements.txt") != "" {
		return nil
	}
	return writePyprojectDependencies(filepath.Join(startDir, pyprojectFile), "renart-pipeline", []string{"pandas"})
}

func defaultDerivedSQLAssetContent(assetName, assetType, assetPath, sourceAssetName, connectionName string) string {
	return DefaultDerivedSQLAssetContent(assetName, assetType, assetPath, sourceAssetName, connectionName)
}

func ensurePythonProjectFile(absAssetPath, assetType, relAssetPath string) error {
	return EnsurePythonProjectFile(absAssetPath, assetType, relAssetPath)
}
