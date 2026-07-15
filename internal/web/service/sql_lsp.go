package service

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	polyglot "github.com/tobilg/polyglot/packages/go"

	"renart/internal/sqllsp"
	"renart/internal/web/model"
	"renart/internal/web/sqlformat"
)

type SQLLSPRequest struct {
	AssetID            string                   `json:"asset_id"`
	Content            string                   `json:"content"`
	Position           sqllsp.Position          `json:"position,omitempty"`
	IncludeDeclaration bool                     `json:"include_declaration,omitempty"`
	NewName            string                   `json:"new_name,omitempty"`
	FormattingOptions  sqllsp.FormattingOptions `json:"formatting_options,omitempty"`
}

type SQLLSPResponse struct {
	Status      string                       `json:"status"`
	Diagnostics []sqllsp.Diagnostic          `json:"diagnostics,omitempty"`
	Completions []sqllsp.CompletionItem      `json:"completions,omitempty"`
	Locations   []sqllsp.Location            `json:"locations,omitempty"`
	Hover       *sqllsp.Hover                `json:"hover,omitempty"`
	Edit        *sqllsp.WorkspaceEdit        `json:"edit,omitempty"`
	CodeActions []sqllsp.CodeAction          `json:"code_actions,omitempty"`
	Tokens      *sqllsp.SemanticTokens       `json:"tokens,omitempty"`
	TokenLegend *sqllsp.SemanticTokensLegend `json:"token_legend,omitempty"`
	Symbols     []sqllsp.DocumentSymbol      `json:"symbols,omitempty"`
	Signature   *sqllsp.SignatureHelp        `json:"signature,omitempty"`
	Error       string                       `json:"error,omitempty"`
}

type SQLLSPDependencies struct {
	WorkspaceRoot string
	CurrentState  func() model.WorkspaceState
	// PolyglotClient returns a shared SQL validation client, or nil when one is
	// not (yet) available. It is consulted on every request so an
	// asynchronously-loaded client is picked up as soon as it is ready. May be
	// nil, in which case diagnostics fall back to the regex-based checks.
	PolyglotClient func() *polyglot.Client
}

type SQLLSPService struct {
	deps SQLLSPDependencies

	// graphForState is derived purely from the workspace state, so it is cached
	// by the state's monotonic Revision to avoid rebuilding the graph (and
	// re-inferring every SQL asset's columns) on every keystroke request.
	cacheMu          sync.Mutex
	cachedRevision   int64
	cachedGraph      sqllsp.CanonicalGraph
	cachedGraphReady bool
	buildCount       atomic.Int64
}

func NewSQLLSPService(deps SQLLSPDependencies) *SQLLSPService {
	return &SQLLSPService{deps: deps}
}

// NewLazyPolyglotClient returns a getter suitable for
// SQLLSPDependencies.PolyglotClient. The first call kicks off a background load
// of the native validation library and returns nil; once the library is open,
// subsequent calls return the shared client. Loading never blocks the request
// path, and a load failure simply leaves the getter returning nil (regex-only
// diagnostics).
func NewLazyPolyglotClient() func() *polyglot.Client {
	var (
		once sync.Once
		mu   sync.RWMutex
		poly *polyglot.Client
	)
	return func() *polyglot.Client {
		once.Do(func() {
			go func() {
				client, _, err := sqllsp.OpenPolyglotClient(context.Background(), sqllsp.PolyglotFFIOptions{})
				if err != nil {
					return
				}
				mu.Lock()
				poly = client
				mu.Unlock()
			}()
		})
		mu.RLock()
		defer mu.RUnlock()
		return poly
	}
}

func (s *SQLLSPService) polyglotClient() *polyglot.Client {
	if s.deps.PolyglotClient == nil {
		return nil
	}
	return s.deps.PolyglotClient()
}

func (s *SQLLSPService) Diagnostics(ctx context.Context, req SQLLSPRequest) (SQLLSPResponse, *APIError) {
	engine, doc, apiErr := s.engineAndDocument(req)
	if apiErr != nil {
		return SQLLSPResponse{}, apiErr
	}
	return SQLLSPResponse{Status: "ok", Diagnostics: engine.Diagnostics(doc)}, nil
}

func (s *SQLLSPService) Completions(ctx context.Context, req SQLLSPRequest) (SQLLSPResponse, *APIError) {
	engine, doc, apiErr := s.engineAndDocument(req)
	if apiErr != nil {
		return SQLLSPResponse{}, apiErr
	}
	return SQLLSPResponse{Status: "ok", Completions: engine.Complete(doc, req.Position)}, nil
}

func (s *SQLLSPService) Definition(ctx context.Context, req SQLLSPRequest) (SQLLSPResponse, *APIError) {
	engine, doc, apiErr := s.engineAndDocument(req)
	if apiErr != nil {
		return SQLLSPResponse{}, apiErr
	}
	return SQLLSPResponse{Status: "ok", Locations: engine.Definition(doc, req.Position)}, nil
}

func (s *SQLLSPService) References(ctx context.Context, req SQLLSPRequest) (SQLLSPResponse, *APIError) {
	state := s.deps.CurrentState()
	asset, notebook, ok := s.selectedAsset(state, req.AssetID)
	if !ok {
		return SQLLSPResponse{}, &APIError{Status: 400, Code: "asset_not_found", Message: "asset not found"}
	}
	content := req.Content
	if strings.TrimSpace(content) == "" {
		content, _ = sqlLSPDocumentContent(asset)
	}
	doc := sqllsp.TextDocumentItem{URI: assetURI(s.deps.WorkspaceRoot, asset), LanguageID: "sql", Text: content}
	engine := sqllsp.NewEngineWithPolyglot(s.graphForRequest(state, notebook), s.polyglotClient())
	docs := s.documentsForState(state, notebook, req.AssetID, content)
	return SQLLSPResponse{Status: "ok", Locations: engine.WorkspaceReferences(doc, req.Position, docs, req.IncludeDeclaration)}, nil
}

func (s *SQLLSPService) Rename(ctx context.Context, req SQLLSPRequest) (SQLLSPResponse, *APIError) {
	engine, doc, apiErr := s.engineAndDocument(req)
	if apiErr != nil {
		return SQLLSPResponse{}, apiErr
	}
	edit, err := engine.Rename(doc, req.Position, req.NewName)
	if err != nil {
		// Not a request failure: the rename is simply unavailable here (e.g.
		// templated SQL). Report the reason so the editor can show it.
		return SQLLSPResponse{Status: "error", Error: err.Error()}, nil
	}
	return SQLLSPResponse{Status: "ok", Edit: edit}, nil
}

func (s *SQLLSPService) CodeActions(ctx context.Context, req SQLLSPRequest) (SQLLSPResponse, *APIError) {
	engine, doc, apiErr := s.engineAndDocument(req)
	if apiErr != nil {
		return SQLLSPResponse{}, apiErr
	}
	return SQLLSPResponse{Status: "ok", CodeActions: engine.CodeActions(doc)}, nil
}

func (s *SQLLSPService) Hover(ctx context.Context, req SQLLSPRequest) (SQLLSPResponse, *APIError) {
	engine, doc, apiErr := s.engineAndDocument(req)
	if apiErr != nil {
		return SQLLSPResponse{}, apiErr
	}
	return SQLLSPResponse{Status: "ok", Hover: engine.Hover(doc, req.Position)}, nil
}

func (s *SQLLSPService) SemanticTokens(ctx context.Context, req SQLLSPRequest) (SQLLSPResponse, *APIError) {
	engine, doc, apiErr := s.engineAndDocument(req)
	if apiErr != nil {
		return SQLLSPResponse{}, apiErr
	}
	tokens := engine.SemanticTokens(doc)
	return SQLLSPResponse{
		Status:      "ok",
		Tokens:      &tokens,
		TokenLegend: &sqllsp.SemanticTokensLegend{TokenTypes: sqllsp.SemanticTokenTypes(), TokenModifiers: []string{}},
	}, nil
}

func (s *SQLLSPService) DocumentSymbols(ctx context.Context, req SQLLSPRequest) (SQLLSPResponse, *APIError) {
	engine, doc, apiErr := s.engineAndDocument(req)
	if apiErr != nil {
		return SQLLSPResponse{}, apiErr
	}
	return SQLLSPResponse{Status: "ok", Symbols: engine.DocumentSymbols(doc)}, nil
}

func (s *SQLLSPService) Formatting(ctx context.Context, req SQLLSPRequest) (SQLLSPResponse, *APIError) {
	_, doc, apiErr := s.engineAndDocument(req)
	if apiErr != nil {
		return SQLLSPResponse{}, apiErr
	}
	formatted, err := sqlformat.Format(ctx, doc.Text, s.dialectForDocument(doc))
	if err != nil {
		return SQLLSPResponse{Status: "error", Error: err.Error()}, nil
	}
	return SQLLSPResponse{
		Status: "ok",
		Edit: &sqllsp.WorkspaceEdit{Changes: map[sqllsp.URI][]sqllsp.TextEdit{
			doc.URI: {
				{Range: sqllsp.Range{Start: sqllsp.Position{}, End: sqllsp.PositionAt(doc.Text, len(doc.Text))}, NewText: formatted},
			},
		}},
	}, nil
}

func (s *SQLLSPService) SignatureHelp(ctx context.Context, req SQLLSPRequest) (SQLLSPResponse, *APIError) {
	engine, doc, apiErr := s.engineAndDocument(req)
	if apiErr != nil {
		return SQLLSPResponse{}, apiErr
	}
	return SQLLSPResponse{Status: "ok", Signature: engine.SignatureHelp(doc, req.Position)}, nil
}

func (s *SQLLSPService) dialectForDocument(doc sqllsp.TextDocumentItem) string {
	state := s.deps.CurrentState()
	for _, pipeline := range state.Pipelines {
		for _, asset := range pipeline.Assets {
			if assetURI(s.deps.WorkspaceRoot, asset) == doc.URI {
				return sqllsp.DialectFromAssetType(asset.Type)
			}
		}
	}
	for _, notebook := range state.Notebooks {
		for _, cell := range notebook.Cells {
			if assetURI(s.deps.WorkspaceRoot, cell) == doc.URI {
				return sqllsp.DialectFromAssetType(cell.Type)
			}
		}
	}
	return sqlformat.DialectGeneric
}

func (s *SQLLSPService) engineAndDocument(req SQLLSPRequest) (*sqllsp.Engine, sqllsp.TextDocumentItem, *APIError) {
	state := s.deps.CurrentState()
	asset, notebook, ok := s.selectedAsset(state, req.AssetID)
	if !ok {
		return nil, sqllsp.TextDocumentItem{}, &APIError{Status: 400, Code: "asset_not_found", Message: "asset not found"}
	}
	content := req.Content
	if strings.TrimSpace(content) == "" {
		content, _ = sqlLSPDocumentContent(asset)
	}
	doc := sqllsp.TextDocumentItem{URI: assetURI(s.deps.WorkspaceRoot, asset), LanguageID: "sql", Text: content}
	return sqllsp.NewEngineWithPolyglot(s.graphForRequest(state, notebook), s.polyglotClient()), doc, nil
}

// selectedAsset finds the asset an LSP request targets: a pipeline asset or a
// notebook cell. For a cell the containing notebook is returned too, so the
// graph can be scoped to its sibling cells.
func (s *SQLLSPService) selectedAsset(state model.WorkspaceState, assetID string) (model.Asset, *model.Notebook, bool) {
	for _, pipeline := range state.Pipelines {
		for _, asset := range pipeline.Assets {
			if asset.ID == assetID {
				return asset, nil, true
			}
		}
	}
	for i := range state.Notebooks {
		for _, cell := range state.Notebooks[i].Cells {
			if cell.ID == assetID {
				return cell, &state.Notebooks[i], true
			}
		}
	}
	return model.Asset{}, nil, false
}

// documentsForState collects the SQL documents reference search runs over:
// every pipeline SQL asset and query sensor, plus — when the request targets a
// notebook cell — the sibling cells of that notebook.
func (s *SQLLSPService) documentsForState(state model.WorkspaceState, notebook *model.Notebook, selectedAssetID, selectedContent string) []sqllsp.TextDocumentItem {
	assets := make([]model.Asset, 0, 16)
	for _, pipeline := range state.Pipelines {
		assets = append(assets, pipeline.Assets...)
	}
	if notebook != nil {
		assets = append(assets, notebook.Cells...)
	}
	var docs []sqllsp.TextDocumentItem
	for _, asset := range assets {
		content, isSQLDocument := sqlLSPDocumentContent(asset)
		if !isSQLDocument {
			continue
		}
		if asset.ID == selectedAssetID {
			content = selectedContent
		}
		docs = append(docs, sqllsp.TextDocumentItem{
			URI:        assetURI(s.deps.WorkspaceRoot, asset),
			LanguageID: "sql",
			Text:       content,
		})
	}
	return docs
}

func sqlLSPDocumentContent(asset model.Asset) (string, bool) {
	normalizedType := strings.ToLower(strings.TrimSpace(asset.Type))
	if strings.HasSuffix(normalizedType, ".sql") {
		return asset.Content, true
	}
	if strings.HasSuffix(normalizedType, ".sensor.query") {
		return asset.Parameters["query"], true
	}
	return "", false
}

// graphForState returns the canonical graph for the given workspace state. The
// graph depends only on the state, so it is cached by the state's monotonic
// Revision: during editing every keystroke issues LSP requests against the same
// saved state, and rebuilding the graph each time is wasted work. A Revision of
// 0 (an unmanaged/initial state) is never cached so callers always see fresh
// results.
func (s *SQLLSPService) graphForState(state model.WorkspaceState) sqllsp.CanonicalGraph {
	revision := state.Revision
	if revision > 0 {
		s.cacheMu.Lock()
		if s.cachedGraphReady && s.cachedRevision == revision {
			graph := s.cachedGraph
			s.cacheMu.Unlock()
			return graph
		}
		s.cacheMu.Unlock()
	}

	graph := s.buildGraph(state)

	if revision > 0 {
		s.cacheMu.Lock()
		s.cachedRevision = revision
		s.cachedGraph = graph
		s.cachedGraphReady = true
		s.cacheMu.Unlock()
	}
	return graph
}

func (s *SQLLSPService) buildGraph(state model.WorkspaceState) sqllsp.CanonicalGraph {
	s.buildCount.Add(1)
	var pipelineAssets []model.Asset
	for _, pipeline := range state.Pipelines {
		pipelineAssets = append(pipelineAssets, pipeline.Assets...)
	}
	nodes, columns := s.graphAssetNodes(pipelineAssets)
	graph := sqllsp.GraphFromRenartAssets(sqllsp.FileURI(s.deps.WorkspaceRoot), nodes, columns)
	return s.withInferredColumns(graph, pipelineAssets, columns)
}

// graphForRequest returns the revision-cached pipeline graph, extended with the
// requesting notebook's cells when the request targets one. Cells are scoped to
// their notebook — pipeline assets and other notebooks never see them —
// mirroring the per-notebook DuckDB session.
func (s *SQLLSPService) graphForRequest(state model.WorkspaceState, notebook *model.Notebook) sqllsp.CanonicalGraph {
	graph := s.graphForState(state)
	if notebook == nil {
		return graph
	}
	return s.graphWithNotebookCells(graph, *notebook)
}

// graphWithNotebookCells extends base with the notebook's cells (relations with
// declared or inferred columns) and the cells' external table references (bare
// relations that resolve without claiming any columns), so sibling reads and
// warehouse tables do not surface as unresolved relations. base is the shared
// cached graph, so the extension builds fresh slices instead of appending in
// place.
func (s *SQLLSPService) graphWithNotebookCells(base sqllsp.CanonicalGraph, notebook model.Notebook) sqllsp.CanonicalGraph {
	nodes, columns := s.graphAssetNodes(notebook.Cells)
	cellGraph := sqllsp.GraphFromRenartAssets(base.WorkspaceURI, nodes, columns)

	merged := base
	merged.Assets = append(append([]sqllsp.AssetNode{}, base.Assets...), cellGraph.Assets...)
	merged.Relations = append(append([]sqllsp.RelationNode{}, base.Relations...), cellGraph.Relations...)
	merged.Schemas = append(append([]sqllsp.SchemaLayer{}, base.Schemas...), cellGraph.Schemas...)

	relationNames := make(map[string]bool, len(merged.Relations))
	for _, relation := range merged.Relations {
		relationNames[strings.ToLower(strings.TrimSpace(relation.Name))] = true
	}
	for _, cell := range notebook.Cells {
		for _, ref := range cell.ExternalRefs {
			key := strings.ToLower(strings.TrimSpace(ref))
			if key == "" || relationNames[key] {
				continue
			}
			relationNames[key] = true
			merged.Relations = append(merged.Relations, sqllsp.RelationNode{
				ID:   "relation:external:" + key,
				Name: ref,
			})
		}
	}

	return s.withInferredColumns(merged, notebook.Cells, columns)
}

// graphAssetNodes converts workspace assets (pipeline assets or notebook cells)
// into graph nodes plus their declared columns, keyed by asset ID.
func (s *SQLLSPService) graphAssetNodes(modelAssets []model.Asset) ([]sqllsp.AssetNode, map[string][]sqllsp.ColumnInfo) {
	var nodes []sqllsp.AssetNode
	columns := map[string][]sqllsp.ColumnInfo{}
	for _, asset := range modelAssets {
		if strings.TrimSpace(asset.Name) == "" {
			continue
		}
		isSQLAsset := strings.HasSuffix(strings.ToLower(asset.Type), ".sql")
		isQuerySensor := strings.HasSuffix(strings.ToLower(asset.Type), ".sensor.query")
		kind := strings.ToLower(strings.TrimSpace(asset.Type))
		dialect := ""
		if kind == "" {
			kind = "asset"
		}
		if isSQLAsset {
			kind = "sql_model"
		}
		if isSQLAsset || isQuerySensor {
			dialect = sqllsp.DialectFromAssetType(asset.Type)
		}
		nodes = append(nodes, sqllsp.AssetNode{
			ID:      asset.ID,
			Name:    asset.Name,
			Kind:    kind,
			Dialect: dialect,
			URI:     assetURI(s.deps.WorkspaceRoot, asset),
		})
		for _, column := range asset.Columns {
			columns[asset.ID] = append(columns[asset.ID], sqllsp.ColumnInfo{Name: column.Name, Type: column.Type, Description: column.Description})
		}
	}
	return nodes, columns
}

// maxInferenceRounds caps the column-inference fixpoint. Targets are processed
// upstream-first with in-round index updates, so a well-formed DAG converges in
// two rounds (one to propagate, one to confirm stability) regardless of chain
// depth; the cap only bites when declared upstreams are missing, stale, or
// cyclic, where each round still propagates at least one hop. Chains that
// exhaust it degrade to missing columns (no completions, no column
// validation), never false diagnostics.
const maxInferenceRounds = 5

// withInferredColumns appends inferred schema layers for the SQL assets that
// declare no columns. Inference is self-referential — a `select *` asset
// copies its upstream's columns, which may themselves be inferred — so it runs
// to a fixpoint: rounds repeat until no inferred column set changes (or
// maxInferenceRounds). Each round re-infers every undeclared asset (partial
// results can grow, e.g. `select a.x, *`) and applies each result to the
// engine's index immediately, so later assets in the same round see it.
// Processing upstream-first (topoOrderInferenceTargets, using the
// auto-reconciled declared upstreams) makes that in-round chaining resolve a
// DAG in a single round; the fixpoint remains the correctness net when the
// edges lie. The final result is the least fixpoint either way — order only
// affects how fast it converges, not what it converges to.
func (s *SQLLSPService) withInferredColumns(graph sqllsp.CanonicalGraph, modelAssets []model.Asset, columns map[string][]sqllsp.ColumnInfo) sqllsp.CanonicalGraph {
	relationByAssetID := map[string]sqllsp.RelationNode{}
	for _, relation := range graph.Relations {
		if relation.AssetID != "" {
			relationByAssetID[relation.AssetID] = relation
		}
	}
	var targets []model.Asset
	for _, asset := range modelAssets {
		if len(columns[asset.ID]) > 0 || !strings.HasSuffix(strings.ToLower(asset.Type), ".sql") {
			continue
		}
		if _, ok := relationByAssetID[asset.ID]; !ok {
			continue
		}
		targets = append(targets, asset)
	}
	if len(targets) == 0 {
		return graph
	}
	targets = topoOrderInferenceTargets(targets)

	baseSchemas := graph.Schemas
	inferred := map[string][]sqllsp.ColumnInfo{}
	for round := 0; round < maxInferenceRounds; round++ {
		engine := sqllsp.NewEngine(graph)
		changed := false
		for _, asset := range targets {
			next := engine.InferOutputColumns(asset.Content)
			if slices.Equal(next, inferred[asset.ID]) {
				continue
			}
			inferred[asset.ID] = next
			engine.SetRelationColumns(relationByAssetID[asset.ID].ID, next)
			changed = true
		}
		if !changed {
			break
		}
		// Rebuild the schema list from the base each round so a relation never
		// carries stale layers from earlier rounds; base is shared with the
		// cached graph, so append onto a fresh slice.
		schemas := append(make([]sqllsp.SchemaLayer, 0, len(baseSchemas)+len(targets)), baseSchemas...)
		for _, asset := range targets {
			cols := inferred[asset.ID]
			if len(cols) == 0 {
				continue
			}
			relation := relationByAssetID[asset.ID]
			schemas = append(schemas, sqllsp.SchemaLayer{
				RelationID:   relation.ID,
				SourceKind:   "inferred",
				Completeness: "partial",
				Columns:      cols,
				Provenance:   relation.Provenance,
			})
		}
		graph.Schemas = schemas
	}
	return graph
}

// topoOrderInferenceTargets orders inference targets upstream-first (Kahn's
// algorithm) using their declared upstreams, which are kept truthful by
// dependency reconciliation on asset save and by the notebook loader's
// used-tables scan. Only edges between targets matter — an upstream with
// declared columns needs no inference. Assets on cycles (or with indegrees
// the queue never drains) are appended in their original order; the caller's
// fixpoint absorbs them.
func topoOrderInferenceTargets(targets []model.Asset) []model.Asset {
	indexByName := map[string]int{}
	for i, asset := range targets {
		indexByName[strings.ToLower(strings.TrimSpace(asset.Name))] = i
	}
	downstream := make([][]int, len(targets))
	indegree := make([]int, len(targets))
	for i, asset := range targets {
		for _, upstream := range asset.Upstreams {
			j, ok := indexByName[strings.ToLower(strings.TrimSpace(upstream))]
			if !ok || j == i {
				continue
			}
			downstream[j] = append(downstream[j], i)
			indegree[i]++
		}
	}
	ordered := make([]model.Asset, 0, len(targets))
	visited := make([]bool, len(targets))
	queue := make([]int, 0, len(targets))
	for i := range targets {
		if indegree[i] == 0 {
			queue = append(queue, i)
		}
	}
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		visited[i] = true
		ordered = append(ordered, targets[i])
		for _, j := range downstream[i] {
			indegree[j]--
			if indegree[j] == 0 {
				queue = append(queue, j)
			}
		}
	}
	for i := range targets {
		if !visited[i] {
			ordered = append(ordered, targets[i])
		}
	}
	return ordered
}

func assetURI(root string, asset model.Asset) sqllsp.URI {
	if filepath.IsAbs(asset.Path) {
		return sqllsp.FileURI(asset.Path)
	}
	return sqllsp.FileURI(filepath.Join(root, asset.Path))
}
