package runcontext

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteResourceIdentitySeparatesKindsAndCoordinates(t *testing.T) {
	t.Parallel()
	duckdb := WriteResourceIdentity(WriteResourceCoordinates{
		Kind: "duckdb_database", FilePath: "/workspace/analytics.duckdb",
	})
	require.Equal(t, IdentityFidelityExact, duckdb.Fidelity, duckdb.Message)
	assert.Len(t, duckdb.Digest, 64)
	assert.Equal(t, duckdb.Digest, WriteResourceIdentity(WriteResourceCoordinates{
		Kind: "duckdb_database", FilePath: "/workspace/analytics.duckdb",
	}).Digest)
	assert.NotEqual(t, duckdb.Digest, WriteResourceIdentity(WriteResourceCoordinates{
		Kind: "local_file", FilePath: "/workspace/analytics.duckdb",
	}).Digest)
	assert.NotEqual(t, duckdb.Digest, WriteResourceIdentity(WriteResourceCoordinates{
		Kind: "duckdb_database", FilePath: "/workspace/other.duckdb",
	}).Digest)
}
