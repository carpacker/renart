package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/secretstore"
	"renart/internal/web/service"
)

type configHTTPSecretProvider struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (p *configHTTPSecretProvider) Name() string { return "local" }
func (p *configHTTPSecretProvider) key(project, environment string, ref secretstore.Ref) string {
	return project + "\x00" + environment + "\x00" + ref.String()
}
func (p *configHTTPSecretProvider) Stat(_ context.Context, request secretstore.ResolveRequest) (secretstore.Status, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := secretstore.StatusMissing
	if _, found := p.values[p.key(request.ProjectID, request.Environment, request.Reference)]; found {
		state = secretstore.StatusConfigured
	}
	return secretstore.Status{State: state, Provider: "local", Writable: true, Rotatable: true}, nil
}
func (p *configHTTPSecretProvider) Resolve(_ context.Context, request secretstore.ResolveRequest) (secretstore.Lease, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	value, found := p.values[p.key(request.ProjectID, request.Environment, request.Reference)]
	if !found {
		return nil, secretstore.ErrNotFound
	}
	return &configHTTPSecretLease{value: append([]byte(nil), value...)}, nil
}
func (p *configHTTPSecretProvider) Put(_ context.Context, request secretstore.PutRequest) (secretstore.Status, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.values[p.key(request.ProjectID, request.Environment, request.Reference)] = append([]byte(nil), request.Value...)
	return secretstore.Status{State: secretstore.StatusConfigured, Provider: "local", Writable: true, Rotatable: true}, nil
}
func (p *configHTTPSecretProvider) Delete(_ context.Context, request secretstore.DeleteRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.values, p.key(request.ProjectID, request.Environment, request.Reference))
	return nil
}

type configHTTPSecretLease struct{ value []byte }

func (l *configHTTPSecretLease) Bytes() []byte        { return l.value }
func (l *configHTTPSecretLease) VersionID() string    { return "" }
func (l *configHTTPSecretLease) ExpiresAt() time.Time { return time.Time{} }
func (l *configHTTPSecretLease) Close(context.Context) error {
	for index := range l.value {
		l.value[index] = 0
	}
	l.value = nil
	return nil
}

func TestWorkspaceConfigHTTPNeverReturnsConnectionSecrets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	configPath := filepath.Join(root, ".bruin.yml")
	provider := &configHTTPSecretProvider{values: make(map[string][]byte)}
	resolver, err := secretstore.NewResolver(provider)
	require.NoError(t, err)
	configService := service.NewConfigService(root, configPath, service.WithSecretResolver(resolver))
	cfg, _, err := configService.LoadForEditing()
	require.NoError(t, err)
	require.NoError(t, configService.AddConnection(cfg, service.UpsertWorkspaceConnectionParams{
		EnvironmentName: "default",
		Name:            "warehouse",
		Type:            "postgres",
		Values: map[string]any{
			"host": "localhost", "port": 5432, "database": "analytics", "username": "renart",
		},
		SecretChanges: map[string]service.WorkspaceConnectionSecretChange{
			"password": {Action: "replace", Value: "existing-http-secret-canary"},
		},
	}))
	_, err = configService.Persist(cfg)
	require.NoError(t, err)

	handlers := &ConfigHandlers{Service: configService}
	getResponse := httptest.NewRecorder()
	handlers.HandleGetWorkspaceConfig(
		getResponse,
		httptest.NewRequest(http.MethodGet, "/api/config", nil),
	)
	require.Equal(t, http.StatusOK, getResponse.Code)
	assert.NotContains(t, getResponse.Body.String(), "existing-http-secret-canary")
	assert.Contains(t, getResponse.Body.String(), `"password":{"status":"configured"`)

	createBody := []byte(`{
		"environment_name":"default",
		"name":"second",
		"type":"postgres",
		"values":{"host":"localhost","port":5432,"database":"analytics","username":"renart"},
		"secret_changes":{"password":{"action":"replace","value":"create-http-secret-canary"}}
	}`)
	createResponse := httptest.NewRecorder()
	handlers.HandleCreateWorkspaceConnection(
		createResponse,
		httptest.NewRequest(http.MethodPost, "/api/config/connections", bytes.NewReader(createBody)),
	)
	require.Equal(t, http.StatusOK, createResponse.Code, createResponse.Body.String())
	assert.NotContains(t, createResponse.Body.String(), "create-http-secret-canary")

	var response service.WorkspaceConfigResponse
	require.NoError(t, json.Unmarshal(createResponse.Body.Bytes(), &response))
	for _, environment := range response.Environments {
		for _, connection := range environment.Connections {
			assert.NotContains(t, connection.Values, "password")
		}
	}

	invalidBody := []byte(`{
		"environment_name":"default",
		"name":"invalid",
		"type":"postgres",
		"values":{"host":"localhost"},
		"secret_changes":{"password":{"action":"invalid","value":"error-http-secret-canary"}}
	}`)
	invalidResponse := httptest.NewRecorder()
	handlers.HandleCreateWorkspaceConnection(
		invalidResponse,
		httptest.NewRequest(http.MethodPost, "/api/config/connections", bytes.NewReader(invalidBody)),
	)
	require.Equal(t, http.StatusBadRequest, invalidResponse.Code)
	assert.NotContains(t, invalidResponse.Body.String(), "error-http-secret-canary")
}
