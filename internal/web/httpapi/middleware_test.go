package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	webscheduler "renart/internal/web/scheduler"
	"renart/internal/web/service"
)

func TestSessionTokenAuthenticatesCLIRunOrigin(t *testing.T) {
	t.Parallel()
	var origin webscheduler.RunTrigger
	handler := SameOriginGuardWithToken("server-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin = service.ExecutionOrigin(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "/api/run", nil)
	request.Header.Set("X-Renart-Token", "server-secret")
	request.Header.Set("X-Renart-UI-Execution-Origin", "manual")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assert.Equal(t, http.StatusNoContent, response.Code)
	assert.Equal(t, webscheduler.RunTriggerCLI, origin)
}

func TestSameOriginBrowserCanMarkManualRunOrigin(t *testing.T) {
	t.Parallel()
	var origin webscheduler.RunTrigger
	handler := SameOriginGuardWithToken("server-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin = service.ExecutionOrigin(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/run", nil)
	request.Header.Set("Origin", "http://127.0.0.1:5173")
	request.Header.Set("X-Renart-UI-Execution-Origin", "manual")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assert.Equal(t, http.StatusNoContent, response.Code)
	assert.Equal(t, webscheduler.RunTriggerManual, origin)
}

func TestHeaderWithoutBrowserOriginCannotMarkManualRunOrigin(t *testing.T) {
	t.Parallel()
	var origin webscheduler.RunTrigger
	handler := SameOriginGuardWithToken("server-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin = service.ExecutionOrigin(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "/api/run", nil)
	request.Header.Set("X-Renart-UI-Execution-Origin", "manual")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assert.Equal(t, http.StatusNoContent, response.Code)
	assert.Equal(t, webscheduler.RunTriggerAPI, origin)
}

func TestOrdinaryHTTPRunOriginRemainsAPI(t *testing.T) {
	t.Parallel()
	var origin webscheduler.RunTrigger
	handler := SameOriginGuardWithToken("server-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin = service.ExecutionOrigin(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "/api/run", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assert.Equal(t, http.StatusNoContent, response.Code)
	assert.Equal(t, webscheduler.RunTriggerAPI, origin)
}
