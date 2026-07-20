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

func TestEnsureProjectSelfAssignsOnFirstOpen(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()

	project, err := EnsureProject(fs, "/w/.renart/project.yml", "data_platform")
	require.NoError(t, err)
	require.NoError(t, uuid.Validate(project.ID))
	assert.Equal(t, "data_platform", project.Name)

	// Second open returns the same identity without rewriting.
	again, err := EnsureProject(fs, "/w/.renart/project.yml", "other-default")
	require.NoError(t, err)
	assert.Equal(t, project, again)
}

func TestEnsureProjectAssignsIDToExistingFileKeepingName(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/w/.renart/project.yml", []byte("name: my project\n"), 0o644))

	project, err := EnsureProject(fs, "/w/.renart/project.yml", "dirname")
	require.NoError(t, err)
	require.NoError(t, uuid.Validate(project.ID))
	assert.Equal(t, "my project", project.Name)
}

func TestEnsureProjectLeavesUnparseableFileUntouched(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	corrupt := "id: [unclosed\n"
	require.NoError(t, afero.WriteFile(fs, "/w/.renart/project.yml", []byte(corrupt), 0o644))

	_, err := EnsureProject(fs, "/w/.renart/project.yml", "dirname")
	require.Error(t, err)

	content, err := afero.ReadFile(fs, "/w/.renart/project.yml")
	require.NoError(t, err)
	assert.Equal(t, corrupt, string(content))
}

func TestNormalizeRetentionSettingsUsesDefaultsAndSupportsPartialPolicy(t *testing.T) {
	t.Parallel()

	defaults, err := NormalizeRetentionSettings(nil)
	require.NoError(t, err)
	assert.Equal(t, DefaultRetentionSettings(), defaults)

	partial, err := NormalizeRetentionSettings(&RetentionSettings{
		RunMetadata:              RetentionWindow{Days: 45, MinimumPerPipeline: 0},
		MaterializationFactsDays: 30,
	})
	require.NoError(t, err)
	assert.Equal(t, RetentionWindow{Days: 45, MinimumPerPipeline: 0}, partial.RunMetadata)
	assert.Equal(t, DefaultRetentionSettings().FullLogs, partial.FullLogs)
	assert.Equal(t, 30, partial.MaterializationFactsDays)
	assert.Equal(t, DefaultRetentionSettings().Deployments, partial.Deployments)
}

func TestNormalizeRetentionSettingsRejectsUnsafeValues(t *testing.T) {
	t.Parallel()

	_, err := NormalizeRetentionSettings(&RetentionSettings{
		RunMetadata: RetentionWindow{Days: -1, MinimumPerPipeline: 10},
	})
	require.ErrorContains(t, err, "run metadata retention")

	_, err = NormalizeRetentionSettings(&RetentionSettings{
		RunMetadata: RetentionWindow{Days: 1, MinimumPerPipeline: -1},
	})
	require.ErrorContains(t, err, "minimum per pipeline")

	_, err = NormalizeRetentionSettings(&RetentionSettings{
		TemporaryDirectoriesHours: -1,
	})
	require.ErrorContains(t, err, "temporary directory retention")
}
