package service

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/runcontext"
	"renart/internal/web/secretstore"
)

func TestConfigurationIdentityTracksBindingsButNotResolvedValues(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cfg := configWithPostgresPassword("${WAREHOUSE_PASSWORD}")
	manifest := secretstore.NewManifest()
	require.NoError(t, manifest.SetBinding("default", "warehouse", "password", secretstore.Binding{
		Symbol:    "WAREHOUSE_PASSWORD",
		Reference: secretstore.Ref{Provider: "local", Key: "warehouse/password"},
	}))
	require.NoError(t, secretstore.SaveManifest(filepath.Join(root, ".renart", "secrets.yml"), manifest))

	first := selectedConfigurationIdentityWithBindings(root, cfg, []string{"warehouse"})
	second := selectedConfigurationIdentityWithBindings(
		root,
		configWithPostgresPassword("${WAREHOUSE_PASSWORD}"),
		[]string{"warehouse"},
	)
	assert.Equal(t, runcontext.IdentityFidelityExact, first.Fidelity, first.Message)
	assert.Equal(t, first.Digest, second.Digest)

	require.NoError(t, manifest.SetBinding("default", "warehouse", "password", secretstore.Binding{
		Symbol:    "WAREHOUSE_PASSWORD",
		Reference: secretstore.Ref{Provider: "local", Key: "warehouse/rotated-binding"},
	}))
	require.NoError(t, secretstore.SaveManifest(filepath.Join(root, ".renart", "secrets.yml"), manifest))
	rebound := selectedConfigurationIdentityWithBindings(root, cfg, []string{"warehouse"})
	assert.NotEqual(t, first.Digest, rebound.Digest)
}

func TestConfigurationIdentityFailsClosedWhenBindingPlaceholderDrifts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifest := secretstore.NewManifest()
	require.NoError(t, manifest.SetBinding("default", "warehouse", "password", secretstore.Binding{
		Symbol:    "WAREHOUSE_PASSWORD",
		Reference: secretstore.Ref{Provider: "local", Key: "warehouse/password"},
	}))
	require.NoError(t, secretstore.SaveManifest(filepath.Join(root, ".renart", "secrets.yml"), manifest))

	identity := selectedConfigurationIdentityWithBindings(
		root,
		configWithPostgresPassword("legacy-inline-value"),
		[]string{"warehouse"},
	)
	assert.Equal(t, runcontext.IdentityFidelityRuntimeOnly, identity.Fidelity)
	assert.Empty(t, identity.Digest)
}
