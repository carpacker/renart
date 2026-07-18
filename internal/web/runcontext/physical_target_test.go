package runcontext

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPhysicalTargetIdentityIsStableAndSensitiveToPhysicalCoordinates(t *testing.T) {
	t.Parallel()

	target := PhysicalTargetCoordinates{
		Kind:     "relation",
		Platform: "postgres",
		Host:     "warehouse.internal",
		Port:     5432,
		Catalog:  "analytics",
		Schema:   "public",
		Object:   "customers",
	}
	first := PhysicalTargetIdentity(target)
	second := PhysicalTargetIdentity(target)

	require.Equal(t, IdentityFidelityExact, first.Fidelity, first.Message)
	require.Equal(t, IdentityFidelityExact, second.Fidelity, second.Message)
	assert.NotEmpty(t, first.Digest)
	assert.Equal(t, first.Digest, second.Digest)

	target.Host = "other.internal"
	other := PhysicalTargetIdentity(target)
	require.Equal(t, IdentityFidelityExact, other.Fidelity, other.Message)
	assert.NotEqual(t, first.Digest, other.Digest)
}

func TestPhysicalTargetIdentityUsesItsOwnNamespace(t *testing.T) {
	t.Parallel()

	target := PhysicalTargetCoordinates{Kind: "none", Platform: "none"}
	physical := PhysicalTargetIdentity(target)
	generic := SecretFreeCanonicalIdentity("another-target-version", target)

	require.Equal(t, IdentityFidelityExact, physical.Fidelity, physical.Message)
	require.Equal(t, IdentityFidelityExact, generic.Fidelity, generic.Message)
	assert.NotEqual(t, physical.Digest, generic.Digest)
}
