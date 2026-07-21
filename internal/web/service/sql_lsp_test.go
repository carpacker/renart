package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	polyglot "github.com/tobilg/polyglot/packages/go"
	"renart/internal/sqllsp"
	"renart/internal/web/model"
)

func TestSQLLSPServiceCompletesFromRenartWorkspaceState(t *testing.T) {
	state := model.WorkspaceState{
		Pipelines: []model.Pipeline{{
			ID:   "pipeline",
			Name: "analytics",
			Assets: []model.Asset{
				{
					ID:      "orders",
					Name:    "analytics.orders",
					Type:    "duckdb.sql",
					Path:    "analytics/assets/analytics/orders.sql",
					Content: "select 1 as order_id, 10 as total_amount",
					Columns: []model.Column{
						{Name: "order_id", Type: "integer"},
						{Name: "total_amount", Type: "integer"},
					},
				},
				{
					ID:      "report",
					Name:    "analytics.report",
					Type:    "duckdb.sql",
					Path:    "analytics/assets/analytics/report.sql",
					Content: "select o.\nfrom analytics.orders o",
				},
			},
		}},
	}
	service := NewSQLLSPService(SQLLSPDependencies{
		WorkspaceRoot: t.TempDir(),
		CurrentState:  func() model.WorkspaceState { return state },
	})

	response, apiErr := service.Completions(context.Background(), SQLLSPRequest{
		AssetID:  "report",
		Content:  "select o.\nfrom analytics.orders o",
		Position: sqllsp.Position{Line: 0, Character: len("select o.")},
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	labels := map[string]bool{}
	for _, item := range response.Completions {
		labels[item.Label] = true
	}
	if !labels["order_id"] || !labels["total_amount"] {
		t.Fatalf("expected Renart workspace columns in completions, got %#v", response.Completions)
	}
}

func TestSQLLSPServiceCompletesQuerySensorFromParameterSQL(t *testing.T) {
	state := model.WorkspaceState{
		Pipelines: []model.Pipeline{{
			ID:   "pipeline",
			Name: "analytics",
			Assets: []model.Asset{
				{
					ID:   "orders",
					Name: "analytics.orders",
					Type: "duckdb.sql",
					Path: "analytics/assets/analytics/orders.sql",
					Columns: []model.Column{
						{Name: "order_id", Type: "integer"},
						{Name: "total_amount", Type: "integer"},
					},
				},
				{
					ID:   "orders-ready",
					Name: "analytics.orders_ready",
					Type: "duckdb.sensor.query",
					Path: "analytics/assets/analytics/orders_ready.asset.yml",
					Parameters: map[string]string{
						"query": "select o.\nfrom analytics.orders o",
					},
				},
			},
		}},
	}
	service := NewSQLLSPService(SQLLSPDependencies{
		WorkspaceRoot: t.TempDir(),
		CurrentState:  func() model.WorkspaceState { return state },
	})

	response, apiErr := service.Completions(context.Background(), SQLLSPRequest{
		AssetID: "orders-ready",
		Position: sqllsp.Position{
			Line:      0,
			Character: len("select o."),
		},
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	labels := map[string]bool{}
	for _, item := range response.Completions {
		labels[item.Label] = true
	}
	if !labels["order_id"] || !labels["total_amount"] {
		t.Fatalf("expected workspace columns for query sensor SQL, got %#v", response.Completions)
	}
}

func TestSQLLSPServiceCompletesInferredRenartAssetColumns(t *testing.T) {
	state := model.WorkspaceState{
		Pipelines: []model.Pipeline{{
			ID:   "pipeline",
			Name: "analytics",
			Assets: []model.Asset{
				{
					ID:      "orders",
					Name:    "analytics.orders",
					Type:    "duckdb.sql",
					Path:    "analytics/assets/analytics/orders.sql",
					Content: "select 1 as order_id, 10 as total_amount",
				},
				{
					ID:      "report",
					Name:    "analytics.report",
					Type:    "duckdb.sql",
					Path:    "analytics/assets/analytics/report.sql",
					Content: "select o.\nfrom analytics.orders o",
				},
			},
		}},
	}
	service := NewSQLLSPService(SQLLSPDependencies{
		WorkspaceRoot: t.TempDir(),
		CurrentState:  func() model.WorkspaceState { return state },
	})

	response, apiErr := service.Completions(context.Background(), SQLLSPRequest{
		AssetID:  "report",
		Content:  "select o.\nfrom analytics.orders o",
		Position: sqllsp.Position{Line: 0, Character: len("select o.")},
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	labels := map[string]bool{}
	for _, item := range response.Completions {
		labels[item.Label] = true
	}
	if !labels["order_id"] || !labels["total_amount"] {
		t.Fatalf("expected inferred Renart workspace columns in completions, got %#v", response.Completions)
	}
}

func TestSQLLSPServiceTreatsNonSQLAssetsAsDeclaredRelations(t *testing.T) {
	state := model.WorkspaceState{
		Pipelines: []model.Pipeline{{
			ID:   "pipeline",
			Name: "example",
			Assets: []model.Asset{
				{
					ID:   "api",
					Name: "example.my_api_asset_1",
					Type: "api",
					Path: "example/assets/my_api_asset_1.asset.yml",
					Columns: []model.Column{
						{Name: "id", Type: "string"},
						{Name: "status", Type: "string"},
					},
				},
				{
					ID:      "report",
					Name:    "example.report",
					Type:    "duckdb.sql",
					Path:    "example/assets/report.sql",
					Content: "select * from example.my_api_asset_1",
				},
			},
		}},
	}
	service := NewSQLLSPService(SQLLSPDependencies{
		WorkspaceRoot: t.TempDir(),
		CurrentState:  func() model.WorkspaceState { return state },
	})

	diagnostics, apiErr := service.Diagnostics(context.Background(), SQLLSPRequest{
		AssetID: "report",
		Content: "select * from example.my_api_asset_1",
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	for _, diagnostic := range diagnostics.Diagnostics {
		if diagnostic.Code == "unresolved-relation" {
			t.Fatalf("expected API asset relation to resolve, got diagnostics %#v", diagnostics.Diagnostics)
		}
	}

	completions, apiErr := service.Completions(context.Background(), SQLLSPRequest{
		AssetID:  "report",
		Content:  "select api.\nfrom example.my_api_asset_1 api",
		Position: sqllsp.Position{Line: 0, Character: len("select api.")},
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	labels := map[string]bool{}
	for _, item := range completions.Completions {
		labels[item.Label] = true
	}
	if !labels["id"] || !labels["status"] {
		t.Fatalf("expected API asset columns in completions, got %#v", completions.Completions)
	}
}

func TestSQLLSPServiceWarnsForCrossConnectionReference(t *testing.T) {
	state := model.WorkspaceState{
		Pipelines: []model.Pipeline{{
			ID:   "pipeline",
			Name: "analytics",
			Assets: []model.Asset{
				{
					ID:         "orders",
					Name:       "analytics.orders",
					Type:       "pg.sql",
					Path:       "analytics/assets/analytics/orders.sql",
					Connection: "postgres-default",
				},
				{
					ID:         "report",
					Name:       "analytics.report",
					Type:       "duckdb.sql",
					Path:       "analytics/assets/analytics/report.sql",
					Content:    "select * from analytics.orders",
					Connection: "duckdb-default",
				},
			},
		}},
	}
	service := NewSQLLSPService(SQLLSPDependencies{
		WorkspaceRoot: t.TempDir(),
		CurrentState:  func() model.WorkspaceState { return state },
	})

	response, apiErr := service.Diagnostics(context.Background(), SQLLSPRequest{
		AssetID: "report",
		Content: "select * from analytics.orders",
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	for _, diagnostic := range response.Diagnostics {
		if diagnostic.Code == "cross-connection-reference" {
			if diagnostic.Severity != 2 {
				t.Fatalf("expected warning severity, got %#v", diagnostic)
			}
			return
		}
	}
	t.Fatalf("expected cross-connection diagnostic, got %#v", response.Diagnostics)
}

func TestSQLLSPServiceUsesRequestConnectionForEmbeddedQuery(t *testing.T) {
	state := model.WorkspaceState{
		Pipelines: []model.Pipeline{{
			ID:   "pipeline",
			Name: "analytics",
			Assets: []model.Asset{
				{
					ID:         "orders",
					Name:       "analytics.orders",
					Type:       "pg.sql",
					Path:       "analytics/assets/analytics/orders.sql",
					Connection: "postgres-default",
				},
				{
					ID:         "task",
					Name:       "analytics.task",
					Type:       "python",
					Path:       "analytics/assets/analytics/task.py",
					Connection: "duckdb-default",
				},
			},
		}},
	}
	service := NewSQLLSPService(SQLLSPDependencies{
		WorkspaceRoot: t.TempDir(),
		CurrentState:  func() model.WorkspaceState { return state },
	})

	response, apiErr := service.Diagnostics(context.Background(), SQLLSPRequest{
		AssetID:    "task",
		Content:    "select * from analytics.orders",
		Connection: "postgres-default",
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	for _, diagnostic := range response.Diagnostics {
		if diagnostic.Code == "cross-connection-reference" {
			t.Fatalf("request connection should match the referenced asset: %#v", response.Diagnostics)
		}
	}
}

func TestSQLLSPServiceFindsReferencesAcrossWorkspaceAssets(t *testing.T) {
	state := model.WorkspaceState{
		Pipelines: []model.Pipeline{{
			ID:   "pipeline",
			Name: "analytics",
			Assets: []model.Asset{
				{
					ID:      "orders",
					Name:    "analytics.orders",
					Type:    "duckdb.sql",
					Path:    "analytics/assets/orders.sql",
					Content: "select 1 as order_id",
				},
				{
					ID:      "report",
					Name:    "analytics.report",
					Type:    "duckdb.sql",
					Path:    "analytics/assets/report.sql",
					Content: "select * from analytics.orders",
				},
				{
					ID:      "downstream",
					Name:    "analytics.downstream",
					Type:    "duckdb.sql",
					Path:    "analytics/assets/downstream.sql",
					Content: `select * from {{ ref("analytics.orders") }}`,
				},
				{
					ID:   "orders-ready",
					Name: "analytics.orders_ready",
					Type: "duckdb.sensor.query",
					Path: "analytics/assets/orders_ready.asset.yml",
					Parameters: map[string]string{
						"query": "select count(*) > 0 from analytics.orders",
					},
				},
			},
		}},
	}
	service := NewSQLLSPService(SQLLSPDependencies{
		WorkspaceRoot: t.TempDir(),
		CurrentState:  func() model.WorkspaceState { return state },
	})

	response, apiErr := service.References(context.Background(), SQLLSPRequest{
		AssetID:  "report",
		Content:  "select * from analytics.orders",
		Position: sqllsp.Position{Line: 0, Character: len("select * from analytics.ord")},
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if len(response.Locations) != 3 {
		t.Fatalf("expected references in report, downstream, and query sensor assets, got %#v", response.Locations)
	}
	foundReport := false
	foundDownstream := false
	foundSensor := false
	for _, location := range response.Locations {
		if strings.Contains(string(location.URI), "report.sql") {
			foundReport = true
		}
		if strings.Contains(string(location.URI), "downstream.sql") {
			foundDownstream = true
		}
		if strings.Contains(string(location.URI), "orders_ready.asset.yml") {
			foundSensor = true
		}
	}
	if !foundReport || !foundDownstream || !foundSensor {
		t.Fatalf("expected report, downstream, and query sensor references, got %#v", response.Locations)
	}
}

func sqlLSPCachingState(revision int64, extraColumn string) model.WorkspaceState {
	orders := model.Asset{
		ID:      "orders",
		Name:    "analytics.orders",
		Type:    "duckdb.sql",
		Path:    "analytics/assets/orders.sql",
		Content: "select 1 as order_id",
		Columns: []model.Column{{Name: "order_id", Type: "integer"}},
	}
	if extraColumn != "" {
		orders.Columns = append(orders.Columns, model.Column{Name: extraColumn, Type: "integer"})
	}
	return model.WorkspaceState{
		Revision: revision,
		Pipelines: []model.Pipeline{{
			ID:   "pipeline",
			Name: "analytics",
			Assets: []model.Asset{
				orders,
				{
					ID:      "report",
					Name:    "analytics.report",
					Type:    "duckdb.sql",
					Path:    "analytics/assets/report.sql",
					Content: "select o.\nfrom analytics.orders o",
				},
			},
		}},
	}
}

// The graph is derived only from workspace state, so it must be built once per
// Revision and reused across the many requests a single edit session fires.
func TestSQLLSPServiceCachesGraphByRevision(t *testing.T) {
	current := sqlLSPCachingState(1, "")
	service := NewSQLLSPService(SQLLSPDependencies{
		WorkspaceRoot: t.TempDir(),
		CurrentState:  func() model.WorkspaceState { return current },
	})

	completionReq := SQLLSPRequest{
		AssetID:  "report",
		Content:  "select o.\nfrom analytics.orders o",
		Position: sqllsp.Position{Line: 0, Character: len("select o.")},
	}
	if _, apiErr := service.Completions(context.Background(), completionReq); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, apiErr := service.Diagnostics(context.Background(), SQLLSPRequest{AssetID: "report", Content: completionReq.Content}); apiErr != nil {
		t.Fatal(apiErr)
	}
	if got := service.buildCount.Load(); got != 1 {
		t.Fatalf("expected a single graph build across same-revision requests, got %d", got)
	}

	// A revision bump with a changed schema must invalidate the cache and surface
	// the new column.
	current = sqlLSPCachingState(2, "extra_col")
	resp, apiErr := service.Completions(context.Background(), completionReq)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	labels := map[string]bool{}
	for _, item := range resp.Completions {
		labels[item.Label] = true
	}
	if !labels["extra_col"] {
		t.Fatalf("expected new column surfaced after revision bump, got %#v", resp.Completions)
	}
	if got := service.buildCount.Load(); got != 2 {
		t.Fatalf("expected exactly one rebuild after revision bump, got build count %d", got)
	}
}

// A Revision of 0 signals an unmanaged/initial state, which must never be cached
// so callers cannot be served a stale graph.
func TestSQLLSPServiceDoesNotCacheUnversionedState(t *testing.T) {
	current := sqlLSPCachingState(0, "")
	service := NewSQLLSPService(SQLLSPDependencies{
		WorkspaceRoot: t.TempDir(),
		CurrentState:  func() model.WorkspaceState { return current },
	})

	req := SQLLSPRequest{AssetID: "report", Content: "select o.\nfrom analytics.orders o"}
	if _, apiErr := service.Diagnostics(context.Background(), req); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, apiErr := service.Diagnostics(context.Background(), req); apiErr != nil {
		t.Fatal(apiErr)
	}
	if got := service.buildCount.Load(); got != 2 {
		t.Fatalf("expected a rebuild per request for unversioned state, got build count %d", got)
	}
}

func TestSQLLSPServiceExposesEditorFeatureEndpoints(t *testing.T) {
	state := model.WorkspaceState{
		Pipelines: []model.Pipeline{{
			ID:   "pipeline",
			Name: "analytics",
			Assets: []model.Asset{
				{
					ID:      "orders",
					Name:    "analytics.orders",
					Type:    "duckdb.sql",
					Path:    "analytics/assets/orders.sql",
					Content: "select 1 as order_id, 10 as total_amount",
					Columns: []model.Column{
						{Name: "order_id", Type: "integer"},
						{Name: "total_amount", Type: "integer"},
					},
				},
				{
					ID:      "report",
					Name:    "analytics.report",
					Type:    "duckdb.sql",
					Path:    "analytics/assets/report.sql",
					Content: "select o.order_id from analytics.orders o",
				},
			},
		}},
	}
	service := NewSQLLSPService(SQLLSPDependencies{
		WorkspaceRoot: t.TempDir(),
		CurrentState:  func() model.WorkspaceState { return state },
	})

	tokens, apiErr := service.SemanticTokens(context.Background(), SQLLSPRequest{
		AssetID: "report",
		Content: "select o.order_id from analytics.orders o",
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if tokens.Tokens == nil || len(tokens.Tokens.Data) == 0 || tokens.TokenLegend == nil {
		t.Fatalf("expected semantic token data and legend, got %#v", tokens)
	}

	symbols, apiErr := service.DocumentSymbols(context.Background(), SQLLSPRequest{
		AssetID: "report",
		Content: "select o.order_id from analytics.orders o",
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if len(symbols.Symbols) == 0 {
		t.Fatalf("expected document symbols, got %#v", symbols)
	}

	signature, apiErr := service.SignatureHelp(context.Background(), SQLLSPRequest{
		AssetID:  "report",
		Content:  "insert into analytics.orders values (1, ",
		Position: sqllsp.Position{Line: 0, Character: len("insert into analytics.orders values (1, ")},
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if signature.Signature == nil || signature.Signature.ActiveParameter != 1 {
		t.Fatalf("expected insert signature help, got %#v", signature)
	}

	formatting, apiErr := service.Formatting(context.Background(), SQLLSPRequest{
		AssetID: "report",
		Content: "select  o.order_id  from analytics.orders o",
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if formatting.Edit == nil || len(formatting.Edit.Changes) == 0 {
		t.Fatalf("expected formatting edit, got %#v", formatting)
	}
}

func TestSQLLSPServiceReportsWhyTemplatedRenameIsUnavailable(t *testing.T) {
	state := model.WorkspaceState{
		Pipelines: []model.Pipeline{{
			ID:   "pipeline",
			Name: "analytics",
			Assets: []model.Asset{{
				ID:      "report",
				Name:    "analytics.report",
				Type:    "duckdb.sql",
				Path:    "analytics/assets/report.sql",
				Content: "select o.order_id from {{ ref(\"analytics.orders\") }} o",
			}},
		}},
	}
	service := NewSQLLSPService(SQLLSPDependencies{
		WorkspaceRoot: t.TempDir(),
		CurrentState:  func() model.WorkspaceState { return state },
	})

	response, apiErr := service.Rename(context.Background(), SQLLSPRequest{
		AssetID:  "report",
		Content:  "select o.order_id from {{ ref(\"analytics.orders\") }} o",
		Position: sqllsp.Position{Line: 0, Character: len("select o")},
		NewName:  "ord",
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if response.Status != "error" || response.Error == "" {
		t.Fatalf("expected an error status with a reason, got %#v", response)
	}
	if response.Edit != nil {
		t.Fatalf("expected no edit for a templated document, got %#v", response.Edit)
	}
}

func notebookLSPState() model.WorkspaceState {
	return model.WorkspaceState{
		Pipelines: []model.Pipeline{{
			ID:   "pipeline",
			Name: "analytics",
			Assets: []model.Asset{{
				ID:      "orders",
				Name:    "analytics.orders",
				Type:    "duckdb.sql",
				Path:    "analytics/assets/orders.sql",
				Content: "select 1 as order_id, 10 as total_amount",
				Columns: []model.Column{
					{Name: "order_id", Type: "integer"},
					{Name: "total_amount", Type: "integer"},
				},
			}},
		}},
		Notebooks: []model.Notebook{
			{
				ID:    "nb1",
				Title: "Revenue",
				Path:  "notebooks/revenue",
				Cells: []model.Asset{
					{
						ID:      "nb1-base",
						Name:    "base_stats",
						Type:    "duckdb.sql",
						Path:    "notebooks/revenue/cells/base_stats.sql",
						Content: "select 1 as metric_day, 2 as metric_value",
						Class:   "notebook",
						CellID:  "uuid1:base_stats",
					},
					{
						ID:           "nb1-summary",
						Name:         "summary",
						Type:         "duckdb.sql",
						Path:         "notebooks/revenue/cells/summary.sql",
						Content:      "select b.metric_value from base_stats b",
						Class:        "notebook",
						CellID:       "uuid1:summary",
						ExternalRefs: []string{"raw.events"},
					},
				},
			},
			{
				ID:    "nb2",
				Title: "Other",
				Path:  "notebooks/other",
				Cells: []model.Asset{{
					ID:      "nb2-cell",
					Name:    "other_cell",
					Type:    "duckdb.sql",
					Path:    "notebooks/other/cells/other_cell.sql",
					Content: "select 1 as other_metric",
					Class:   "notebook",
					CellID:  "uuid2:other_cell",
				}},
			},
		},
	}
}

func notebookLSPService(t *testing.T, state model.WorkspaceState) *SQLLSPService {
	t.Helper()
	return NewSQLLSPService(SQLLSPDependencies{
		WorkspaceRoot: t.TempDir(),
		CurrentState:  func() model.WorkspaceState { return state },
	})
}

func TestSQLLSPServiceCompletesSiblingNotebookCellColumns(t *testing.T) {
	service := notebookLSPService(t, notebookLSPState())

	// base_stats declares no columns in state, so they are inferred from its
	// SQL; the sibling cell should still see them behind the alias.
	response, apiErr := service.Completions(context.Background(), SQLLSPRequest{
		AssetID:  "nb1-summary",
		Content:  "select b.\nfrom base_stats b",
		Position: sqllsp.Position{Line: 0, Character: len("select b.")},
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	labels := map[string]bool{}
	for _, item := range response.Completions {
		labels[item.Label] = true
	}
	if !labels["metric_day"] || !labels["metric_value"] {
		t.Fatalf("expected sibling cell columns in completions, got %#v", response.Completions)
	}

	// Pipeline assets stay visible from notebook cells.
	pipelineResponse, apiErr := service.Completions(context.Background(), SQLLSPRequest{
		AssetID:  "nb1-summary",
		Content:  "select o.\nfrom analytics.orders o",
		Position: sqllsp.Position{Line: 0, Character: len("select o.")},
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	pipelineLabels := map[string]bool{}
	for _, item := range pipelineResponse.Completions {
		pipelineLabels[item.Label] = true
	}
	if !pipelineLabels["order_id"] || !pipelineLabels["total_amount"] {
		t.Fatalf("expected pipeline asset columns in notebook completions, got %#v", pipelineResponse.Completions)
	}
}

func TestSQLLSPServiceNotebookDiagnosticsResolveSiblingsAndExternalRefs(t *testing.T) {
	service := notebookLSPService(t, notebookLSPState())

	response, apiErr := service.Diagnostics(context.Background(), SQLLSPRequest{
		AssetID: "nb1-summary",
		Content: "select b.metric_value from base_stats b join raw.events e on e.id = b.metric_day",
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	for _, diagnostic := range response.Diagnostics {
		if diagnostic.Code == "unresolved-relation" {
			t.Fatalf("expected sibling cell and external ref to resolve, got %#v", diagnostic)
		}
	}
}

func TestSQLLSPServiceScopesNotebookCellsToTheirNotebook(t *testing.T) {
	service := notebookLSPService(t, notebookLSPState())

	// A cell from another notebook must not resolve.
	crossNotebook, apiErr := service.Diagnostics(context.Background(), SQLLSPRequest{
		AssetID: "nb1-summary",
		Content: "select * from other_cell",
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if !hasDiagnosticCode(crossNotebook.Diagnostics, "unresolved-relation") {
		t.Fatalf("expected another notebook's cell to stay unresolved, got %#v", crossNotebook.Diagnostics)
	}

	// Pipeline asset requests must not see notebook cells.
	fromPipeline, apiErr := service.Diagnostics(context.Background(), SQLLSPRequest{
		AssetID: "orders",
		Content: "select * from base_stats",
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if !hasDiagnosticCode(fromPipeline.Diagnostics, "unresolved-relation") {
		t.Fatalf("expected notebook cells to be invisible to pipeline assets, got %#v", fromPipeline.Diagnostics)
	}
}

func TestSQLLSPServiceInfersColumnsThroughSelectStarChains(t *testing.T) {
	state := model.WorkspaceState{
		Pipelines: []model.Pipeline{{
			ID:   "pipeline",
			Name: "analytics",
			Assets: []model.Asset{
				{
					ID:      "base",
					Name:    "analytics.base",
					Type:    "duckdb.sql",
					Path:    "analytics/assets/base.sql",
					Content: "select 1 as x, 2 as y",
				},
				{
					ID:      "mid",
					Name:    "analytics.mid",
					Type:    "duckdb.sql",
					Path:    "analytics/assets/mid.sql",
					Content: "select * from analytics.base",
				},
				{
					ID:      "report",
					Name:    "analytics.report",
					Type:    "duckdb.sql",
					Path:    "analytics/assets/report.sql",
					Content: "select m.x from analytics.mid m",
				},
			},
		}},
	}
	service := notebookLSPService(t, state)

	// mid's columns exist only via inference *through* base's inferred columns:
	// a single inference pass cannot see them.
	response, apiErr := service.Completions(context.Background(), SQLLSPRequest{
		AssetID:  "report",
		Content:  "select m.\nfrom analytics.mid m",
		Position: sqllsp.Position{Line: 0, Character: len("select m.")},
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	labels := map[string]bool{}
	for _, item := range response.Completions {
		labels[item.Label] = true
	}
	if !labels["x"] || !labels["y"] {
		t.Fatalf("expected columns to propagate through the select * chain, got %#v", response.Completions)
	}
}

func TestSQLLSPServiceInfersColumnsThroughChainsDeeperThanTheRoundCap(t *testing.T) {
	// A select * chain deeper than maxInferenceRounds, listed downstream-first
	// so per-round propagation alone cannot resolve it: only the topological
	// ordering by declared upstreams lets one round walk the whole chain.
	assets := []model.Asset{{
		ID:      "base",
		Name:    "analytics.base",
		Type:    "duckdb.sql",
		Path:    "analytics/assets/base.sql",
		Content: "select 1 as x, 2 as y",
	}}
	previous := "analytics.base"
	for i := 1; i <= 6; i++ {
		name := fmt.Sprintf("analytics.c%d", i)
		assets = append([]model.Asset{{
			ID:        fmt.Sprintf("c%d", i),
			Name:      name,
			Type:      "duckdb.sql",
			Path:      fmt.Sprintf("analytics/assets/c%d.sql", i),
			Content:   "select * from " + previous,
			Upstreams: []string{previous},
		}}, assets...)
		previous = name
	}
	state := model.WorkspaceState{
		Pipelines: []model.Pipeline{{ID: "pipeline", Name: "analytics", Assets: assets}},
	}
	service := notebookLSPService(t, state)

	response, apiErr := service.Completions(context.Background(), SQLLSPRequest{
		AssetID:  "c6",
		Content:  "select t.\nfrom analytics.c6 t",
		Position: sqllsp.Position{Line: 0, Character: len("select t.")},
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	labels := map[string]bool{}
	for _, item := range response.Completions {
		labels[item.Label] = true
	}
	if !labels["x"] || !labels["y"] {
		t.Fatalf("expected columns to survive a 6-hop select * chain, got %#v", response.Completions)
	}
}

func TestSQLLSPServiceInfersColumnsThroughNotebookCellChains(t *testing.T) {
	state := notebookLSPState()
	state.Notebooks[0].Cells = append(state.Notebooks[0].Cells, model.Asset{
		ID:      "nb1-star",
		Name:    "star_stats",
		Type:    "duckdb.sql",
		Path:    "notebooks/revenue/cells/star_stats.sql",
		Content: "select * from base_stats",
		Class:   "notebook",
		CellID:  "uuid1:star_stats",
	})
	service := notebookLSPService(t, state)

	// star_stats copies base_stats via select *, and base_stats' columns are
	// themselves inferred — two inference hops inside one notebook.
	response, apiErr := service.Completions(context.Background(), SQLLSPRequest{
		AssetID:  "nb1-summary",
		Content:  "select s.\nfrom star_stats s",
		Position: sqllsp.Position{Line: 0, Character: len("select s.")},
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	labels := map[string]bool{}
	for _, item := range response.Completions {
		labels[item.Label] = true
	}
	if !labels["metric_day"] || !labels["metric_value"] {
		t.Fatalf("expected sibling cell columns through a select * chain, got %#v", response.Completions)
	}
}

func TestSQLLSPServiceFindsReferencesFromNotebookCells(t *testing.T) {
	service := notebookLSPService(t, notebookLSPState())

	content := "select o.order_id from analytics.orders o"
	response, apiErr := service.References(context.Background(), SQLLSPRequest{
		AssetID:            "nb1-summary",
		Content:            content,
		Position:           sqllsp.Position{Line: 0, Character: len("select o.order_id from analytics.or")},
		IncludeDeclaration: true,
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if len(response.Locations) == 0 {
		t.Fatalf("expected references to the pipeline asset from a notebook cell, got %#v", response)
	}
}

func hasDiagnosticCode(diagnostics []sqllsp.Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

// noNetworkTransport fails every request so tests never download the
// polyglot FFI archive; only an already-cached library can be opened.
type noNetworkTransport struct{}

func (noNetworkTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("network access disabled in tests")
}

// openTestPolyglotClient opens the shared polyglot validator from the local
// cache. The test is skipped when the FFI library is not available on this
// machine, since fetching it would require network access.
func openTestPolyglotClient(t *testing.T) *polyglot.Client {
	t.Helper()
	client, _, err := sqllsp.OpenPolyglotClient(context.Background(), sqllsp.PolyglotFFIOptions{
		Client: &http.Client{Transport: noNetworkTransport{}},
	})
	if err != nil {
		t.Skipf("polyglot FFI library unavailable: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("closing polyglot client: %v", err)
		}
	})
	return client
}

func TestSQLLSPServiceReportsPolyglotSyntaxDiagnostics(t *testing.T) {
	client := openTestPolyglotClient(t)
	state := model.WorkspaceState{
		Pipelines: []model.Pipeline{{
			ID:   "pipeline",
			Name: "analytics",
			Assets: []model.Asset{{
				ID:      "report",
				Name:    "analytics.report",
				Type:    "duckdb.sql",
				Path:    "analytics/assets/report.sql",
				Content: "select 1 as order_id",
			}},
		}},
	}
	service := NewSQLLSPService(SQLLSPDependencies{
		WorkspaceRoot:  t.TempDir(),
		CurrentState:   func() model.WorkspaceState { return state },
		PolyglotClient: func() *polyglot.Client { return client },
	})

	invalid, apiErr := service.Diagnostics(context.Background(), SQLLSPRequest{
		AssetID: "report",
		Content: "select from from",
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	found := false
	for _, diagnostic := range invalid.Diagnostics {
		if diagnostic.Source == "polyglot" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a polyglot syntax diagnostic for invalid SQL, got %#v", invalid.Diagnostics)
	}

	valid, apiErr := service.Diagnostics(context.Background(), SQLLSPRequest{
		AssetID: "report",
		Content: "select 1 as order_id",
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	for _, diagnostic := range valid.Diagnostics {
		if diagnostic.Source == "polyglot" {
			t.Fatalf("expected no polyglot diagnostics for valid SQL, got %#v", diagnostic)
		}
	}
}
