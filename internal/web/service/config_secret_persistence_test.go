package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/secretstore"
)

type statefulSecretProvider struct {
	mu     sync.Mutex
	values map[string][]byte
	putErr error
}

func newStatefulSecretResolver(t *testing.T) (*secretstore.Resolver, *statefulSecretProvider) {
	t.Helper()
	provider := &statefulSecretProvider{values: make(map[string][]byte)}
	resolver, err := secretstore.NewResolver(provider)
	require.NoError(t, err)
	return resolver, provider
}

func (p *statefulSecretProvider) Name() string { return "local" }

func (p *statefulSecretProvider) key(request secretstore.ResolveRequest) string {
	return request.ProjectID + "\x00" + request.Environment + "\x00" + request.Reference.String()
}

func (p *statefulSecretProvider) Stat(
	_ context.Context,
	request secretstore.ResolveRequest,
) (secretstore.Status, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := secretstore.StatusMissing
	if _, found := p.values[p.key(request)]; found {
		state = secretstore.StatusConfigured
	}
	return secretstore.Status{
		State: state, Provider: p.Name(), Reference: request.Reference.String(),
		Writable: true, Rotatable: true,
	}, nil
}

func (p *statefulSecretProvider) Resolve(
	_ context.Context,
	request secretstore.ResolveRequest,
) (secretstore.Lease, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	value, found := p.values[p.key(request)]
	if !found {
		return nil, secretstore.ErrNotFound
	}
	return &testSecretLease{value: append([]byte(nil), value...)}, nil
}

func (p *statefulSecretProvider) Put(
	_ context.Context,
	request secretstore.PutRequest,
) (secretstore.Status, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.putErr != nil {
		return secretstore.Status{}, p.putErr
	}
	key := request.ProjectID + "\x00" + request.Environment + "\x00" + request.Reference.String()
	p.values[key] = append([]byte(nil), request.Value...)
	return secretstore.Status{
		State: secretstore.StatusConfigured, Provider: p.Name(),
		Reference: request.Reference.String(), Writable: true, Rotatable: true,
	}, nil
}

func (p *statefulSecretProvider) Delete(
	_ context.Context,
	request secretstore.DeleteRequest,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := request.ProjectID + "\x00" + request.Environment + "\x00" + request.Reference.String()
	delete(p.values, key)
	return nil
}

func TestPersistedConnectionSecretsUseCredentialStoreAndTrackedBinding(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	resolver, provider := newStatefulSecretResolver(t)
	service := NewConfigService(
		root,
		filepath.Join(root, ".bruin.yml"),
		WithSecretResolver(resolver),
	)
	change, err := service.CreateConnectionAndPersist(t.Context(), UpsertWorkspaceConnectionParams{
		EnvironmentName: "default",
		Name:            "warehouse",
		Type:            "postgres",
		Values: map[string]any{
			"host": "localhost", "port": 5432, "database": "analytics", "username": "renart",
		},
		SecretChanges: map[string]WorkspaceConnectionSecretChange{
			"password": {Action: "replace", Value: "credential-store-canary"},
		},
	})
	require.NoError(t, err)

	configContents, err := os.ReadFile(filepath.Join(root, ".bruin.yml"))
	require.NoError(t, err)
	assert.NotContains(t, string(configContents), "credential-store-canary")
	assert.Contains(t, string(configContents), "${RENART_WAREHOUSE_PASSWORD}")
	configInfo, err := os.Stat(filepath.Join(root, ".bruin.yml"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), configInfo.Mode().Perm())

	manifest, err := secretstore.LoadManifest(filepath.Join(root, ".renart", "secrets.yml"))
	require.NoError(t, err)
	binding, found := manifest.Binding("default", "warehouse", "password")
	require.True(t, found)
	assert.Equal(t, "RENART_WAREHOUSE_PASSWORD", binding.Symbol)
	assert.Equal(t, "local:warehouse/password", binding.Reference.String())

	projectID := service.ProjectIdentity().ID
	provider.mu.Lock()
	assert.Equal(
		t,
		[]byte("credential-store-canary"),
		provider.values[projectID+"\x00default\x00local:warehouse/password"],
	)
	provider.mu.Unlock()

	response := service.BuildResponse(change.ConfigPath, change.Config)
	require.Len(t, response.Environments, 1)
	descriptor := response.Environments[0].Connections[0].SecretFields["password"]
	assert.Equal(t, "configured", descriptor.Status)
	assert.Equal(t, "local", descriptor.Provider)
	assert.Equal(t, "local:warehouse/password", descriptor.Reference)

	factory := NewResolvedConnectionFactory(
		root,
		filepath.Join(root, ".bruin.yml"),
		projectID,
		resolver,
	)
	resolved, err := factory.ResolveConfig(
		t.Context(),
		change.Config,
		"default",
		secretstore.PurposeNotebookQuery,
	)
	require.NoError(t, err)
	assert.Equal(
		t,
		"credential-store-canary",
		resolved.Config.SelectedEnvironment.Connections.Postgres[0].Password,
	)
	require.NoError(t, resolved.Close(t.Context()))
}

func TestPersistedConnectionSecretsCanUseEnvironmentBindings(t *testing.T) {
	t.Setenv("RENART_TEST_WAREHOUSE_PASSWORD", "environment-canary")

	root := t.TempDir()
	localProvider := &statefulSecretProvider{values: make(map[string][]byte)}
	resolver, err := secretstore.NewResolver(
		localProvider,
		secretstore.NewEnvironmentProvider(),
	)
	require.NoError(t, err)
	service := NewConfigService(
		root,
		filepath.Join(root, ".bruin.yml"),
		WithSecretResolver(resolver),
	)
	change, err := service.CreateConnectionAndPersist(t.Context(), UpsertWorkspaceConnectionParams{
		EnvironmentName: "default",
		Name:            "warehouse",
		Type:            "postgres",
		Values: map[string]any{
			"host": "localhost", "port": 5432, "database": "analytics", "username": "renart",
		},
		SecretChanges: map[string]WorkspaceConnectionSecretChange{
			"password": {
				Action:  "replace",
				Binding: &WorkspaceConnectionSecretBinding{Ref: "env:RENART_TEST_WAREHOUSE_PASSWORD"},
			},
		},
	})
	require.NoError(t, err)

	configContents, err := os.ReadFile(filepath.Join(root, ".bruin.yml"))
	require.NoError(t, err)
	assert.NotContains(t, string(configContents), "environment-canary")
	assert.Contains(t, string(configContents), "${RENART_WAREHOUSE_PASSWORD}")

	manifest, err := secretstore.LoadManifest(filepath.Join(root, ".renart", "secrets.yml"))
	require.NoError(t, err)
	binding, found := manifest.Binding("default", "warehouse", "password")
	require.True(t, found)
	assert.Equal(t, "env:RENART_TEST_WAREHOUSE_PASSWORD", binding.Reference.String())
	assert.Empty(t, localProvider.values)

	response := service.BuildResponse(change.ConfigPath, change.Config)
	descriptor := response.Environments[0].Connections[0].SecretFields["password"]
	assert.Equal(t, "configured", descriptor.Status)
	assert.Equal(t, "env", descriptor.Provider)
	assert.Equal(t, "env:RENART_TEST_WAREHOUSE_PASSWORD", descriptor.Reference)
	assert.False(t, descriptor.Writable)

	factory := NewResolvedConnectionFactory(
		root,
		filepath.Join(root, ".bruin.yml"),
		service.ProjectIdentity().ID,
		resolver,
	)
	resolved, err := factory.ResolveConfig(
		t.Context(),
		change.Config,
		"default",
		secretstore.PurposeNotebookQuery,
	)
	require.NoError(t, err)
	assert.Equal(
		t,
		"environment-canary",
		resolved.Config.SelectedEnvironment.Connections.Postgres[0].Password,
	)
	require.NoError(t, resolved.Close(t.Context()))

	draftChanges, draftBundle, err := service.resolveDraftConnectionSecretChanges(
		t.Context(),
		"default",
		map[string]WorkspaceConnectionSecretChange{
			"password": {
				Action:  "replace",
				Binding: &WorkspaceConnectionSecretBinding{Ref: "env:RENART_TEST_WAREHOUSE_PASSWORD"},
			},
		},
	)
	require.NoError(t, err)
	require.NotNil(t, draftBundle)
	assert.Equal(t, "environment-canary", draftChanges["password"].Value)
	require.NoError(t, draftBundle.Close(t.Context()))
}

func TestEnvironmentBindingRejectsAnInlineValue(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	resolver, _ := newStatefulSecretResolver(t)
	service := NewConfigService(root, filepath.Join(root, ".bruin.yml"), WithSecretResolver(resolver))
	_, err := service.CreateConnectionAndPersist(t.Context(), UpsertWorkspaceConnectionParams{
		EnvironmentName: "default",
		Name:            "warehouse",
		Type:            "postgres",
		Values:          map[string]any{"host": "localhost"},
		SecretChanges: map[string]WorkspaceConnectionSecretChange{
			"password": {
				Action:  "replace",
				Value:   "must-not-be-accepted",
				Binding: &WorkspaceConnectionSecretBinding{Ref: "env:WAREHOUSE_PASSWORD"},
			},
		},
	})
	require.ErrorContains(t, err, "cannot include a secret value")

	configContents, readErr := os.ReadFile(filepath.Join(root, ".bruin.yml"))
	require.NoError(t, readErr)
	assert.NotContains(t, string(configContents), "must-not-be-accepted")
}

func TestClearingManagedConnectionSecretRemovesBindingAndCredential(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	resolver, provider := newStatefulSecretResolver(t)
	service := NewConfigService(root, filepath.Join(root, ".bruin.yml"), WithSecretResolver(resolver))
	_, err := service.CreateConnectionAndPersist(t.Context(), UpsertWorkspaceConnectionParams{
		EnvironmentName: "default",
		Name:            "warehouse",
		Type:            "postgres",
		Values: map[string]any{
			"host": "localhost", "port": 5432, "database": "analytics", "username": "renart",
		},
		SecretChanges: map[string]WorkspaceConnectionSecretChange{
			"password": {Action: "replace", Value: "temporary-secret"},
		},
	})
	require.NoError(t, err)

	_, err = service.UpdateConnectionAndPersist(t.Context(), UpsertWorkspaceConnectionParams{
		EnvironmentName: "default",
		CurrentName:     "warehouse",
		Name:            "warehouse",
		Type:            "postgres",
		Values: map[string]any{
			"host": "localhost", "port": 5432, "database": "analytics", "username": "renart",
		},
		SecretChanges: map[string]WorkspaceConnectionSecretChange{
			"password": {Action: "clear"},
		},
	})
	require.NoError(t, err)

	manifest, err := secretstore.LoadManifest(filepath.Join(root, ".renart", "secrets.yml"))
	require.NoError(t, err)
	_, found := manifest.Binding("default", "warehouse", "password")
	assert.False(t, found)
	provider.mu.Lock()
	assert.Empty(t, provider.values)
	provider.mu.Unlock()
}

func TestCredentialStoreFailureDoesNotPersistSecretOrBinding(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	resolver, provider := newStatefulSecretResolver(t)
	provider.putErr = errors.New("credential store locked")
	service := NewConfigService(root, filepath.Join(root, ".bruin.yml"), WithSecretResolver(resolver))

	_, err := service.CreateConnectionAndPersist(t.Context(), UpsertWorkspaceConnectionParams{
		EnvironmentName: "default",
		Name:            "warehouse",
		Type:            "postgres",
		Values: map[string]any{
			"host": "localhost", "port": 5432, "database": "analytics", "username": "renart",
		},
		SecretChanges: map[string]WorkspaceConnectionSecretChange{
			"password": {Action: "replace", Value: "must-not-persist"},
		},
	})
	require.ErrorContains(t, err, "credential store locked")

	configContents, readErr := os.ReadFile(filepath.Join(root, ".bruin.yml"))
	require.NoError(t, readErr)
	assert.NotContains(t, string(configContents), "must-not-persist")
	_, statErr := os.Stat(filepath.Join(root, ".renart", "secrets.yml"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestEnvironmentChangesMoveCopyAndDeleteManagedSecrets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	resolver, provider := newStatefulSecretResolver(t)
	service := NewConfigService(root, filepath.Join(root, ".bruin.yml"), WithSecretResolver(resolver))
	_, err := service.CreateConnectionAndPersist(t.Context(), UpsertWorkspaceConnectionParams{
		EnvironmentName: "default",
		Name:            "warehouse",
		Type:            "postgres",
		Values: map[string]any{
			"host": "localhost", "port": 5432, "database": "analytics", "username": "renart",
		},
		SecretChanges: map[string]WorkspaceConnectionSecretChange{
			"password": {Action: "replace", Value: "environment-secret"},
		},
	})
	require.NoError(t, err)

	_, err = service.UpdateEnvironmentAndPersist(
		t.Context(),
		"default",
		"production",
		"",
		true,
	)
	require.NoError(t, err)
	manifest, err := secretstore.LoadManifest(filepath.Join(root, ".renart", "secrets.yml"))
	require.NoError(t, err)
	_, oldFound := manifest.Binding("default", "warehouse", "password")
	_, productionFound := manifest.Binding("production", "warehouse", "password")
	assert.False(t, oldFound)
	assert.True(t, productionFound)

	projectID := service.ProjectIdentity().ID
	provider.mu.Lock()
	assert.NotContains(t, provider.values, projectID+"\x00default\x00local:warehouse/password")
	assert.Equal(
		t,
		[]byte("environment-secret"),
		provider.values[projectID+"\x00production\x00local:warehouse/password"],
	)
	provider.mu.Unlock()

	_, err = service.CloneEnvironmentAndPersist(
		t.Context(),
		"production",
		"staging",
		"stage",
		false,
	)
	require.NoError(t, err)
	manifest, err = secretstore.LoadManifest(filepath.Join(root, ".renart", "secrets.yml"))
	require.NoError(t, err)
	_, stagingFound := manifest.Binding("staging", "warehouse", "password")
	assert.True(t, stagingFound)
	provider.mu.Lock()
	assert.Equal(
		t,
		[]byte("environment-secret"),
		provider.values[projectID+"\x00staging\x00local:warehouse/password"],
	)
	provider.mu.Unlock()

	_, err = service.DeleteEnvironmentAndPersist(t.Context(), "staging")
	require.NoError(t, err)
	manifest, err = secretstore.LoadManifest(filepath.Join(root, ".renart", "secrets.yml"))
	require.NoError(t, err)
	_, stagingFound = manifest.Binding("staging", "warehouse", "password")
	assert.False(t, stagingFound)
	provider.mu.Lock()
	assert.NotContains(t, provider.values, projectID+"\x00staging\x00local:warehouse/password")
	assert.Contains(t, provider.values, projectID+"\x00production\x00local:warehouse/password")
	provider.mu.Unlock()
}
