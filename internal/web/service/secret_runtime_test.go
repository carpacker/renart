package service

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/secretstore"
)

type testSecretLease struct {
	value []byte
}

func (l *testSecretLease) Bytes() []byte        { return l.value }
func (l *testSecretLease) VersionID() string    { return "" }
func (l *testSecretLease) ExpiresAt() time.Time { return time.Time{} }
func (l *testSecretLease) Close(context.Context) error {
	for index := range l.value {
		l.value[index] = 0
	}
	l.value = nil
	return nil
}

type testSecretProvider struct {
	value string
}

func (p *testSecretProvider) Name() string { return "local" }
func (p *testSecretProvider) Stat(_ context.Context, request secretstore.ResolveRequest) (secretstore.Status, error) {
	return secretstore.Status{
		State: secretstore.StatusConfigured, Provider: p.Name(),
		Reference: request.Reference.String(), Writable: true, Rotatable: true,
	}, nil
}
func (p *testSecretProvider) Resolve(context.Context, secretstore.ResolveRequest) (secretstore.Lease, error) {
	return &testSecretLease{value: []byte(p.value)}, nil
}
func (p *testSecretProvider) Put(context.Context, secretstore.PutRequest) (secretstore.Status, error) {
	return secretstore.Status{}, nil
}
func (p *testSecretProvider) Delete(context.Context, secretstore.DeleteRequest) error {
	return nil
}

func TestResolvedConnectionFactoryOverlaysBoundSecretAndSeedsRedaction(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifest := secretstore.NewManifest()
	require.NoError(t, manifest.SetBinding("default", "warehouse", "password", secretstore.Binding{
		Symbol:    "WAREHOUSE_PASSWORD",
		Reference: secretstore.Ref{Provider: "local", Key: "warehouse/password"},
	}))
	require.NoError(t, secretstore.SaveManifest(filepath.Join(root, ".renart", "secrets.yml"), manifest))

	resolver, err := secretstore.NewResolver(&testSecretProvider{value: "provider-secret-canary"})
	require.NoError(t, err)
	factory := NewResolvedConnectionFactory(root, filepath.Join(root, ".bruin.yml"), "project-id", resolver)
	cfg := configWithPostgresPassword("${WAREHOUSE_PASSWORD}")

	resolved, err := factory.ResolveConfig(t.Context(), cfg, "default", secretstore.PurposeNotebookQuery)
	require.NoError(t, err)
	require.NotNil(t, resolved.Config.SelectedEnvironment)
	require.Len(t, resolved.Config.SelectedEnvironment.Connections.Postgres, 1)
	assert.Equal(t, "provider-secret-canary", resolved.Config.SelectedEnvironment.Connections.Postgres[0].Password)
	assert.Equal(t, "****", resolved.Redactor.Mask("provider-secret-canary"))
	require.NoError(t, resolved.Close(t.Context()))
}

func TestResolvedConnectionFactorySupportsUnboundEnvironmentPlaceholders(t *testing.T) {
	t.Setenv("WAREHOUSE_PASSWORD", "environment-secret")
	factory := NewResolvedConnectionFactory(
		t.TempDir(),
		filepath.Join(t.TempDir(), ".bruin.yml"),
		"project-id",
		secretstore.NewDefaultResolver(),
	)
	cfg := configWithPostgresPassword("${WAREHOUSE_PASSWORD}")

	resolved, err := factory.ResolveConfig(t.Context(), cfg, "default", secretstore.PurposeQuery)
	require.NoError(t, err)
	assert.Equal(t, "environment-secret", resolved.Config.SelectedEnvironment.Connections.Postgres[0].Password)
	require.NoError(t, resolved.Close(t.Context()))
}

func TestResolvedConnectionFactoryRejectsManifestSymbolDrift(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifest := secretstore.NewManifest()
	require.NoError(t, manifest.SetBinding("default", "warehouse", "password", secretstore.Binding{
		Symbol:    "OTHER_PASSWORD",
		Reference: secretstore.Ref{Provider: "local", Key: "warehouse/password"},
	}))
	require.NoError(t, secretstore.SaveManifest(filepath.Join(root, ".renart", "secrets.yml"), manifest))
	resolver, err := secretstore.NewResolver(&testSecretProvider{value: "secret"})
	require.NoError(t, err)
	factory := NewResolvedConnectionFactory(root, filepath.Join(root, ".bruin.yml"), "project-id", resolver)

	_, err = factory.ResolveConfig(
		t.Context(),
		configWithPostgresPassword("${WAREHOUSE_PASSWORD}"),
		"default",
		secretstore.PurposeQuery,
	)
	require.ErrorContains(t, err, "expects ${OTHER_PASSWORD}")
}

type environmentSecretProvider struct {
	values map[string]string
}

func (p *environmentSecretProvider) Name() string { return "local" }
func (p *environmentSecretProvider) Stat(
	_ context.Context,
	request secretstore.ResolveRequest,
) (secretstore.Status, error) {
	return secretstore.Status{
		State: secretstore.StatusConfigured, Provider: p.Name(),
		Reference: request.Reference.String(), Writable: true, Rotatable: true,
	}, nil
}
func (p *environmentSecretProvider) Resolve(
	_ context.Context,
	request secretstore.ResolveRequest,
) (secretstore.Lease, error) {
	return &testSecretLease{value: []byte(p.values[request.Environment])}, nil
}
func (p *environmentSecretProvider) Put(
	context.Context,
	secretstore.PutRequest,
) (secretstore.Status, error) {
	return secretstore.Status{}, nil
}
func (p *environmentSecretProvider) Delete(context.Context, secretstore.DeleteRequest) error {
	return nil
}

func TestResolvedConnectionFactoryDoesNotCrossTalkBetweenEnvironments(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifest := secretstore.NewManifest()
	for _, environment := range []string{"development", "production"} {
		require.NoError(t, manifest.SetBinding(environment, "warehouse", "password", secretstore.Binding{
			Symbol:    "WAREHOUSE_PASSWORD",
			Reference: secretstore.Ref{Provider: "local", Key: "warehouse/password"},
		}))
	}
	require.NoError(t, secretstore.SaveManifest(filepath.Join(root, ".renart", "secrets.yml"), manifest))
	resolver, err := secretstore.NewResolver(&environmentSecretProvider{values: map[string]string{
		"development": "development-canary",
		"production":  "production-canary",
	}})
	require.NoError(t, err)
	factory := NewResolvedConnectionFactory(root, filepath.Join(root, ".bruin.yml"), "project-id", resolver)

	type result struct {
		environment string
		value       string
		err         error
	}
	results := make(chan result, 2)
	var start sync.WaitGroup
	start.Add(1)
	for _, environment := range []string{"development", "production"} {
		environment := environment
		go func() {
			start.Wait()
			cfg := configWithPostgresPasswordForEnvironment(
				environment,
				"${WAREHOUSE_PASSWORD}",
			)
			resolved, resolveErr := factory.ResolveConfig(
				t.Context(),
				cfg,
				environment,
				secretstore.PurposeInspect,
			)
			if resolveErr != nil {
				results <- result{environment: environment, err: resolveErr}
				return
			}
			value := resolved.Config.SelectedEnvironment.Connections.Postgres[0].Password
			closeErr := resolved.Close(t.Context())
			results <- result{environment: environment, value: value, err: closeErr}
		}()
	}
	start.Done()

	got := make(map[string]string)
	for range 2 {
		item := <-results
		require.NoError(t, item.err)
		got[item.environment] = item.value
	}
	assert.Equal(t, "development-canary", got["development"])
	assert.Equal(t, "production-canary", got["production"])
}

func configWithPostgresPassword(password string) *config.Config {
	return configWithPostgresPasswordForEnvironment("default", password)
}

func configWithPostgresPasswordForEnvironment(environment, password string) *config.Config {
	cfg := &config.Config{
		DefaultEnvironmentName:  environment,
		SelectedEnvironmentName: environment,
		Environments: map[string]config.Environment{
			environment: {
				Connections: &config.Connections{
					Postgres: []config.PostgresConnection{{
						ConnectionMetadata: config.ConnectionMetadata{Name: "warehouse"},
						Host:               "localhost", Port: 5432,
						Database: "analytics", Username: "renart", Password: password,
					}},
				},
			},
		},
	}
	_ = cfg.SelectEnvironment(environment)
	return cfg
}
