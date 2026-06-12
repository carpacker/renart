package identity

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsurePipelineIDGeneratesAndWritesBack(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	original := "name: my-pipeline\nschedule: daily\n"
	require.NoError(t, afero.WriteFile(fs, "/p/pipeline.yml", []byte(original), 0o644))

	id, generated, err := EnsurePipelineID(fs, "/p/pipeline.yml")
	require.NoError(t, err)
	assert.True(t, generated)
	require.NoError(t, uuid.Validate(id))

	content, err := afero.ReadFile(fs, "/p/pipeline.yml")
	require.NoError(t, err)
	assert.Equal(t, "id: "+id+"\n"+original, string(content))

	// Second call is a no-op returning the same ID.
	again, generated, err := EnsurePipelineID(fs, "/p/pipeline.yml")
	require.NoError(t, err)
	assert.False(t, generated)
	assert.Equal(t, id, again)
}

func TestEnsurePipelineIDKeepsExistingID(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	original := "name: my-pipeline\nid: existing-id\n"
	require.NoError(t, afero.WriteFile(fs, "/p/pipeline.yml", []byte(original), 0o644))

	id, generated, err := EnsurePipelineID(fs, "/p/pipeline.yml")
	require.NoError(t, err)
	assert.False(t, generated)
	assert.Equal(t, "existing-id", id)

	content, err := afero.ReadFile(fs, "/p/pipeline.yml")
	require.NoError(t, err)
	assert.Equal(t, original, string(content))
}

func TestEnsurePipelineIDEmptyFile(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/p/pipeline.yml", []byte(""), 0o644))

	id, generated, err := EnsurePipelineID(fs, "/p/pipeline.yml")
	require.NoError(t, err)
	assert.True(t, generated)
	require.NoError(t, uuid.Validate(id))

	content, err := afero.ReadFile(fs, "/p/pipeline.yml")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(content), "id: "+id))
}

func TestAssetIDRoundTrip(t *testing.T) {
	t.Parallel()
	id := AssetID("pipeline-uuid", "schema.my_table")
	assert.Equal(t, "pipeline-uuid:schema.my_table", id)

	pipelineUUID, assetName, ok := SplitAssetID(id)
	require.True(t, ok)
	assert.Equal(t, "pipeline-uuid", pipelineUUID)
	assert.Equal(t, "schema.my_table", assetName)

	_, _, ok = SplitAssetID("no-separator")
	assert.False(t, ok)
	_, _, ok = SplitAssetID(":missing-pipeline")
	assert.False(t, ok)
	_, _, ok = SplitAssetID("missing-asset:")
	assert.False(t, ok)
}
