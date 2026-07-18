package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/fingerprint"
	"renart/internal/web/matlog"
	"renart/internal/web/scheduler"
	"renart/internal/web/staleness"
)

// The frontend sends Date.toISOString() values ("...T10:00:00.000Z"); the
// selected time range silently falling out of the staleness computation on
// a parse failure would be invisible, so pin the accepted formats.
func TestParseQueryTimeAcceptsFrontendFormats(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw      string
		expected time.Time
		ok       bool
	}{
		{"2026-06-12T10:00:00.000Z", time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC), true},
		{"2026-06-12T10:00:00Z", time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC), true},
		{"2026-06-12T12:00:00+02:00", time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC), true},
		{"", time.Time{}, false},
		{"not-a-time", time.Time{}, false},
	}
	for _, testCase := range cases {
		// URL-encode like the frontend's URLSearchParams does.
		request := httptest.NewRequest("GET", "/api/x?start="+url.QueryEscape(testCase.raw), nil)
		parsed, ok := parseQueryTime(request, "start")
		require.Equal(t, testCase.ok, ok, "input %q", testCase.raw)
		if ok {
			assert.True(t, parsed.Equal(testCase.expected), "input %q parsed to %s", testCase.raw, parsed)
		}
	}
}

func TestStalenessHTTPIncludesDataStateSnapshotContract(t *testing.T) {
	t.Parallel()
	schedulerStore, err := scheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = schedulerStore.Close() })

	pl := &pipeline.Pipeline{
		LegacyID:       "pipeline-uuid",
		Name:           "orders",
		DefinitionFile: pipeline.DefinitionFile{Path: "/workspace/orders/pipeline.yml"},
		Assets: []*pipeline.Asset{{
			Name:            "orders.daily",
			Type:            "duckdb.sql",
			ExecutableFile:  pipeline.ExecutableFile{Path: "/workspace/orders/orders.sql", Content: "select 1"},
			Materialization: pipeline.Materialization{Type: pipeline.MaterializationTypeTable},
		}},
	}
	const target = "renart-physical-target-v1:http"
	service := staleness.New(staleness.Dependencies{
		Store:  matlog.NewStore(schedulerStore.DB()),
		Engine: fingerprint.NewEngine(),
		Resolve: func(context.Context, string) (*pipeline.Pipeline, error) {
			return pl, nil
		},
		ResolveTargets: func(context.Context, staleness.Selection, *pipeline.Pipeline) (map[string]staleness.PhysicalTarget, error) {
			return map[string]staleness.PhysicalTarget{
				"pipeline-uuid:orders.daily": {Identity: target, Exact: true},
			}, nil
		},
	})

	router := chi.NewRouter()
	RegisterStalenessRoutes(router, &StalenessAPI{
		Service: service,
		ResolvePipelineUUID: func(pipelineID string) (string, bool) {
			return "pipeline-uuid", pipelineID == "encoded-pipeline"
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/pipelines/encoded-pipeline/staleness?environment=dev", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)

	var payload struct {
		PipelineID     string                  `json:"pipeline_id"`
		PipelineUUID   string                  `json:"pipeline_uuid"`
		Environment    string                  `json:"environment"`
		DataStateToken string                  `json:"data_state_token"`
		Assets         []staleness.AssetStatus `json:"assets"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	assert.Equal(t, "encoded-pipeline", payload.PipelineID)
	assert.Equal(t, "pipeline-uuid", payload.PipelineUUID)
	assert.Equal(t, "dev", payload.Environment)
	assert.NotEmpty(t, payload.DataStateToken)
	require.Len(t, payload.Assets, 1)
	assert.Equal(t, staleness.TargetFidelityExact, payload.Assets[0].TargetFidelity)
	assert.Equal(t, target, payload.Assets[0].TargetIdentity)
}
