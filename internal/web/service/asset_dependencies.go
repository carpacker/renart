package service

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/sqlparser"
	"github.com/spf13/afero"
)

type sqlAssetDependencyMerge struct {
	Upstreams []pipeline.Upstream
	Inferred  []string
}

const renartInferredUpstreamsMetaKey = "renart_inferred_upstreams"

func (s *AssetService) RefactorDirectDependencies(ctx context.Context, parsedPipeline *pipeline.Pipeline, oldName, newName string) ([]string, []string, error) {
	if parsedPipeline == nil || strings.TrimSpace(oldName) == strings.TrimSpace(newName) {
		return nil, nil, nil
	}

	sqlParserInstance, err := sqlparser.NewSQLParser(false)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create sql parser: %w", err)
	}
	defer sqlParserInstance.Close()

	renderer := jinja.NewRendererWithYesterday(parsedPipeline.Name, "web-rename")
	fs := s.fs()
	changedIDs := make([]string, 0)
	changedPaths := make([]string, 0)

	for _, current := range parsedPipeline.Assets {
		if strings.EqualFold(current.Name, newName) || strings.EqualFold(current.Name, oldName) {
			continue
		}

		updated := false
		for index, upstream := range current.Upstreams {
			if !strings.EqualFold(upstream.Value, oldName) {
				continue
			}

			current.Upstreams[index].Value = newName
			updated = true
		}

		isSQLAsset := isSQLAssetFile(current)
		if isSQLAsset {
			nextContent := ReplaceAssetNameReferences(current.ExecutableFile.Content, oldName, newName)
			if nextContent != current.ExecutableFile.Content {
				current.ExecutableFile.Content = nextContent
				updated = true
			}
		}

		if !updated {
			continue
		}

		if err := current.Persist(fs, parsedPipeline); err != nil {
			return nil, nil, fmt.Errorf("failed to persist renamed dependency updates for asset '%s': %w", current.Name, err)
		}

		if isSQLAsset {
			if err := reconcileSQLAssetDependenciesFS(ctx, fs, current, parsedPipeline, sqlParserInstance, renderer); err != nil {
				return nil, nil, fmt.Errorf("failed to refresh dependencies for asset '%s': %w", current.Name, err)
			}
		}

		assetPath := current.ExecutableFile.Path
		if assetPath == "" {
			assetPath = current.DefinitionFile.Path
		}

		relAssetPath, relErr := filepath.Rel(s.deps.WorkspaceRoot, assetPath)
		if relErr != nil {
			relAssetPath = assetPath
		}

		normalizedPath := filepath.ToSlash(relAssetPath)
		changedIDs = append(changedIDs, EncodeID(normalizedPath))
		changedPaths = append(changedPaths, normalizedPath)
	}

	return changedIDs, changedPaths, nil
}

func (s *AssetService) ScheduleSQLPatches(relAssetPath string) {
	assetPath := filepath.ToSlash(relAssetPath)

	s.patchMu.Lock()
	if existing, ok := s.patchTimers[assetPath]; ok {
		existing.Stop()
	}

	s.patchTimers[assetPath] = time.AfterFunc(1500*time.Millisecond, func() {
		s.RunSQLPatches(assetPath)

		s.patchMu.Lock()
		delete(s.patchTimers, assetPath)
		s.patchMu.Unlock()
	})
	s.patchMu.Unlock()
}

func (s *AssetService) RunSQLPatches(relAssetPath string) {
	prefixedPath := relAssetPath
	if !strings.HasPrefix(prefixedPath, "./") {
		prefixedPath = "./" + strings.TrimPrefix(prefixedPath, "./")
	}

	if s.deps.Executor != nil {
		_, _ = s.deps.Executor.ApplyPatch(context.Background(), PatchRequest{
			Operation:  "fill-columns-from-db",
			TargetPath: prefixedPath,
		})
	}

	if err := s.reconcileSQLAssetDependencies(context.Background(), relAssetPath); err != nil {
		return
	}

	if s.deps.SuppressWatcher != nil {
		s.deps.SuppressWatcher(relAssetPath)
	}
	if s.deps.PushWorkspaceUpdate != nil {
		s.deps.PushWorkspaceUpdate(context.Background(), "asset.patched", relAssetPath)
	}
}

func appendUniqueStrings(values []string, extras ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(extras))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, extra := range extras {
		if extra == "" {
			continue
		}
		if _, ok := seen[extra]; ok {
			continue
		}
		seen[extra] = struct{}{}
		values = append(values, extra)
	}
	return values
}

func ReplaceAssetNameReferences(content, oldName, newName string) string {
	trimmedOld := strings.TrimSpace(oldName)
	trimmedNew := strings.TrimSpace(newName)
	if trimmedOld == "" || trimmedNew == "" || trimmedOld == trimmedNew {
		return content
	}

	pattern := fmt.Sprintf(`(?i)(^|[^A-Za-z0-9_.])(%s)([^A-Za-z0-9_.]|$)`, regexp.QuoteMeta(trimmedOld))
	re := regexp.MustCompile(pattern)
	return re.ReplaceAllString(content, `${1}`+trimmedNew+`${3}`)
}

func isSQLAssetFile(asset *pipeline.Asset) bool {
	if asset == nil {
		return false
	}

	assetPath := asset.ExecutableFile.Path
	if assetPath == "" {
		assetPath = asset.DefinitionFile.Path
	}
	assetPath = strings.ToLower(assetPath)
	assetType := strings.ToLower(string(asset.Type))
	return strings.HasSuffix(assetPath, ".sql") || strings.Contains(assetType, "sql")
}

func updateSQLAssetDependencies(ctx context.Context, asset *pipeline.Asset, parsedPipeline *pipeline.Pipeline, sqlParserInstance *sqlparser.SQLParser, renderer *jinja.Renderer) error {
	return reconcileSQLAssetDependencies(ctx, asset, parsedPipeline, sqlParserInstance, renderer)
}

func (s *AssetService) reconcileSQLAssetDependencies(ctx context.Context, relAssetPath string) error {
	assetID := EncodeID(filepath.ToSlash(relAssetPath))
	_, parsedPipeline, asset, err := s.deps.ResolveAssetByID(ctx, assetID)
	if err != nil {
		return err
	}

	sqlParserInstance, err := sqlparser.NewSQLParser(false)
	if err != nil {
		return err
	}
	defer sqlParserInstance.Close()

	renderer := jinja.NewRendererWithYesterday(parsedPipeline.Name, "web-asset-update")
	return reconcileSQLAssetDependenciesFS(ctx, s.fs(), asset, parsedPipeline, sqlParserInstance, renderer)
}

func reconcileSQLAssetDependencies(ctx context.Context, asset *pipeline.Asset, parsedPipeline *pipeline.Pipeline, sqlParserInstance *sqlparser.SQLParser, renderer *jinja.Renderer) error {
	return reconcileSQLAssetDependenciesFS(ctx, afero.NewOsFs(), asset, parsedPipeline, sqlParserInstance, renderer)
}

func reconcileSQLAssetDependenciesFS(ctx context.Context, fs afero.Fs, asset *pipeline.Asset, parsedPipeline *pipeline.Pipeline, sqlParserInstance *sqlparser.SQLParser, renderer *jinja.Renderer) error {
	if asset == nil || parsedPipeline == nil {
		return nil
	}
	if fs == nil {
		fs = afero.NewOsFs()
	}

	inferredNames, err := inferAllSQLAssetDependencies(ctx, asset, parsedPipeline, sqlParserInstance, renderer)
	if err != nil {
		return err
	}
	merged := mergeSQLAssetDependencies(asset.Name, asset.Upstreams, asset.Meta, inferredNames)
	asset.Upstreams = merged.Upstreams
	setRenartInferredUpstreams(&asset.Meta, merged.Inferred)
	originalHadExplicitName := assetContentHasExplicitName(asset.ExecutableFile.Content)

	if err := asset.Persist(fs, parsedPipeline); err != nil {
		return fmt.Errorf("failed to persist asset '%s': %w", asset.Name, err)
	}
	if !originalHadExplicitName {
		if err := removePersistedAssetNameField(asset); err != nil {
			return err
		}
	}

	return nil
}

func mergeSQLAssetDependencies(assetName string, currentUpstreams []pipeline.Upstream, meta pipeline.EmptyStringMap, inferredNames []string) sqlAssetDependencyMerge {
	tracked := parseRenartInferredUpstreams(meta)
	manualAssetUpstreams := make([]pipeline.Upstream, 0)
	nonAssetUpstreams := make([]pipeline.Upstream, 0)
	manualNames := make(map[string]struct{})

	for _, upstream := range currentUpstreams {
		if !isAssetUpstream(upstream) {
			nonAssetUpstreams = append(nonAssetUpstreams, upstream)
			continue
		}

		normalized := normalizeDependencyName(upstream.Value)
		if normalized == "" || strings.EqualFold(normalized, assetName) {
			continue
		}
		if _, ok := tracked[normalized]; ok {
			continue
		}

		manualAssetUpstreams = append(manualAssetUpstreams, upstream)
		manualNames[normalized] = struct{}{}
	}

	nextInferred := make([]string, 0, len(inferredNames))
	for _, name := range inferredNames {
		normalized := normalizeDependencyName(name)
		if normalized == "" || strings.EqualFold(normalized, assetName) {
			continue
		}
		if _, ok := manualNames[normalized]; ok {
			continue
		}
		nextInferred = append(nextInferred, name)
	}

	sort.SliceStable(nextInferred, func(i, j int) bool {
		return strings.ToLower(nextInferred[i]) < strings.ToLower(nextInferred[j])
	})

	nextUpstreams := make([]pipeline.Upstream, 0, len(nonAssetUpstreams)+len(manualAssetUpstreams)+len(nextInferred))
	nextUpstreams = append(nextUpstreams, nonAssetUpstreams...)
	nextUpstreams = append(nextUpstreams, manualAssetUpstreams...)
	for _, name := range nextInferred {
		nextUpstreams = append(nextUpstreams, pipeline.Upstream{Type: "asset", Value: name, Mode: pipeline.UpstreamModeFull})
	}

	return sqlAssetDependencyMerge{Upstreams: nextUpstreams, Inferred: nextInferred}
}

func inferAllSQLAssetDependencies(ctx context.Context, asset *pipeline.Asset, parsedPipeline *pipeline.Pipeline, sqlParserInstance *sqlparser.SQLParser, renderer *jinja.Renderer) ([]string, error) {
	cloned := *asset
	cloned.Upstreams = nil

	assetRenderer, err := renderer.CloneForAsset(ctx, parsedPipeline, &cloned)
	if err != nil {
		return nil, fmt.Errorf("failed to create renderer for asset '%s': %w", asset.Name, err)
	}

	missingDeps, err := sqlParserInstance.GetMissingDependenciesForAsset(&cloned, parsedPipeline, assetRenderer)
	if err != nil {
		return nil, fmt.Errorf("failed to infer dependencies for asset '%s': %w", asset.Name, err)
	}
	if len(missingDeps) == 0 {
		missingDeps, err = inferSQLAssetDependenciesFromUsedTables(&cloned, parsedPipeline, sqlParserInstance, assetRenderer)
		if err != nil {
			return nil, err
		}
	}

	result := make([]string, 0, len(missingDeps))
	seen := make(map[string]struct{}, len(missingDeps))
	for _, dep := range missingDeps {
		canonical := resolveInferredDependencyName(dep, asset, parsedPipeline)
		normalized := normalizeDependencyName(canonical)
		if normalized == "" {
			continue
		}

		key := normalizeDependencyName(canonical)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, canonical)
	}

	return result, nil
}

func inferSQLAssetDependenciesFromUsedTables(asset *pipeline.Asset, parsedPipeline *pipeline.Pipeline, sqlParserInstance *sqlparser.SQLParser, renderer jinja.RendererInterface) ([]string, error) {
	if asset == nil || parsedPipeline == nil {
		return nil, nil
	}

	dialect, err := sqlparser.AssetTypeToDialect(asset.Type)
	if err != nil {
		return nil, nil
	}

	renderedQuery, err := renderer.Render(mergeAssetMacrosWithQuery(asset.ExecutableFile.Content, parsedPipeline.Macros))
	if err != nil {
		return nil, fmt.Errorf("failed to render the query before parsing the SQL")
	}

	usedTables, err := sqlParserInstance.UsedTables(renderedQuery, dialect)
	if err != nil {
		return nil, fmt.Errorf("failed to infer dependencies for asset '%s': failed to get used tables: %w", asset.Name, err)
	}

	result := make([]string, 0, len(usedTables))
	for _, table := range usedTables {
		canonical := resolveInferredDependencyName(table, asset, parsedPipeline)
		if found := getAssetByNameCaseInsensitiveLocal(parsedPipeline, canonical); found != nil {
			result = append(result, found.Name)
		}
	}

	return result, nil
}

func mergeAssetMacrosWithQuery(query string, macros []pipeline.Macro) string {
	if len(macros) == 0 {
		return query
	}

	var b strings.Builder
	for _, macro := range macros {
		b.WriteString(string(macro))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(query)

	return b.String()
}

func resolveInferredDependencyName(dep string, asset *pipeline.Asset, parsedPipeline *pipeline.Pipeline) string {
	name := strings.TrimSpace(dep)
	if name == "" || parsedPipeline == nil {
		return name
	}

	if found := getAssetByNameCaseInsensitiveLocal(parsedPipeline, name); found != nil {
		return found.Name
	}

	if strings.Contains(name, ".") || asset == nil {
		return name
	}

	lastDot := strings.LastIndex(strings.TrimSpace(asset.Name), ".")
	if lastDot <= 0 {
		return name
	}

	candidate := asset.Name[:lastDot+1] + name
	if found := getAssetByNameCaseInsensitiveLocal(parsedPipeline, candidate); found != nil {
		return found.Name
	}

	return name
}

func applyManualAssetUpstreams(asset *pipeline.Asset, parsedPipeline *pipeline.Pipeline, requested []string) {
	if asset == nil {
		return
	}

	tracked := parseRenartInferredUpstreams(asset.Meta)
	preservedNonAsset := make([]pipeline.Upstream, 0)
	preservedTracked := make([]pipeline.Upstream, 0)
	manualNames := make(map[string]struct{})
	nextManual := make([]pipeline.Upstream, 0, len(requested))

	for _, raw := range requested {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if parsedPipeline != nil {
			if found := getAssetByNameCaseInsensitiveLocal(parsedPipeline, name); found != nil {
				name = found.Name
			}
		}
		if strings.EqualFold(name, asset.Name) {
			continue
		}

		normalized := normalizeDependencyName(name)
		if _, ok := manualNames[normalized]; ok {
			continue
		}
		manualNames[normalized] = struct{}{}
		nextManual = append(nextManual, pipeline.Upstream{Type: "asset", Value: name, Mode: pipeline.UpstreamModeFull})
	}

	for _, upstream := range asset.Upstreams {
		if !isAssetUpstream(upstream) {
			preservedNonAsset = append(preservedNonAsset, upstream)
			continue
		}

		normalized := normalizeDependencyName(upstream.Value)
		if normalized == "" {
			continue
		}
		if _, ok := tracked[normalized]; ok {
			if _, overridden := manualNames[normalized]; !overridden {
				preservedTracked = append(preservedTracked, upstream)
			}
		}
	}

	asset.Upstreams = append(append(preservedNonAsset, nextManual...), preservedTracked...)
	nextTracked := make([]string, 0, len(preservedTracked))
	for _, upstream := range preservedTracked {
		nextTracked = append(nextTracked, upstream.Value)
	}
	setRenartInferredUpstreams(&asset.Meta, nextTracked)
}

func parseRenartInferredUpstreams(meta map[string]string) map[string]string {
	result := make(map[string]string)
	if meta == nil {
		return result
	}

	for _, raw := range strings.Split(meta[renartInferredUpstreamsMetaKey], ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		result[normalizeDependencyName(name)] = name
	}

	return result
}

func getAssetByNameCaseInsensitiveLocal(parsedPipeline *pipeline.Pipeline, name string) *pipeline.Asset {
	if parsedPipeline == nil {
		return nil
	}

	for _, asset := range parsedPipeline.Assets {
		if asset != nil && strings.EqualFold(asset.Name, name) {
			return asset
		}
	}

	return nil
}

func setRenartInferredUpstreams(meta *pipeline.EmptyStringMap, upstreams []string) {
	unique := make([]string, 0, len(upstreams))
	seen := make(map[string]struct{}, len(upstreams))
	for _, upstream := range upstreams {
		name := strings.TrimSpace(upstream)
		if name == "" {
			continue
		}
		key := normalizeDependencyName(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, name)
	}

	if len(unique) == 0 {
		if *meta == nil {
			return
		}
		delete(*meta, renartInferredUpstreamsMetaKey)
		if len(*meta) == 0 {
			*meta = nil
		}
		return
	}

	sort.SliceStable(unique, func(i, j int) bool {
		return strings.ToLower(unique[i]) < strings.ToLower(unique[j])
	})

	if *meta == nil {
		*meta = pipeline.EmptyStringMap{}
	}
	(*meta)[renartInferredUpstreamsMetaKey] = strings.Join(unique, ",")
}

func normalizeDependencyName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isAssetUpstream(upstream pipeline.Upstream) bool {
	return upstream.Type == "" || strings.EqualFold(upstream.Type, "asset")
}

func pipelinePathsReferToSameRoot(sourcePipelinePath, targetPipelineRoot string) bool {
	normalizedSource := filepath.Clean(sourcePipelinePath)
	normalizedTarget := filepath.Clean(targetPipelineRoot)
	if normalizedSource == normalizedTarget {
		return true
	}
	return filepath.Clean(filepath.Dir(normalizedSource)) == normalizedTarget
}
