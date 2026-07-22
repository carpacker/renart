package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	webmodel "renart/internal/web/model"
	"renart/internal/web/service"
)

func TestPipelinePythonDependenciesRoutesReadAndWritePipelineProject(t *testing.T) {
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	require.NoError(t, os.MkdirAll(pipelineRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	pipelineID := service.EncodeID("analytics")

	router := chi.NewRouter()
	RegisterPipelineRoutes(router, &PipelineHandlers{Service: service.NewPipelineService(workspaceRoot)})

	put := httptest.NewRequest(
		http.MethodPut,
		"/api/pipelines/"+pipelineID+"/python-dependencies",
		strings.NewReader(`{"dependencies":["pandas>=2","polars"]}`),
	)
	putResponse := httptest.NewRecorder()
	router.ServeHTTP(putResponse, put)
	require.Equal(t, http.StatusOK, putResponse.Code, putResponse.Body.String())

	get := httptest.NewRequest(http.MethodGet, "/api/pipelines/"+pipelineID+"/python-dependencies", nil)
	getResponse := httptest.NewRecorder()
	router.ServeHTTP(getResponse, get)
	require.Equal(t, http.StatusOK, getResponse.Code, getResponse.Body.String())
	var response webmodel.PipelinePythonDependenciesResponse
	require.NoError(t, json.Unmarshal(getResponse.Body.Bytes(), &response))
	assert.Equal(t, "analytics/pyproject.toml", response.Path)
	assert.Equal(t, []string{"pandas>=2", "polars"}, response.Dependencies)
}
