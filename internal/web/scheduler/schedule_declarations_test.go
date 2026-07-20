package scheduler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduleDeclarationStoreRoundTripUsesStableKeysAndNoPin(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".renart", "schedules.yml")
	store := NewScheduleDeclarationStore(path)

	require.NoError(t, store.Set("pipeline-uuid", "production", ScheduleDeclaration{
		Cron:          "0 * * * *",
		Timezone:      "Europe/Berlin",
		CatchupPolicy: CatchupRunOnce,
		Variables:     map[string]any{"region": "eu", "limit": 100},
		SecretRefs:    map[string]string{"api_token": "env:RENART_API_TOKEN"},
	}))

	declarations, err := store.List()
	require.NoError(t, err)
	require.Len(t, declarations, 1)
	assert.Equal(t, "pipeline-uuid", declarations[0].PipelineUUID)
	assert.Equal(t, "production", declarations[0].Environment)
	assert.Equal(t, "env:RENART_API_TOKEN", declarations[0].Declaration.SecretRefs["api_token"])

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(data)
	assert.Contains(t, text, "version: 1")
	assert.Contains(t, text, "pipeline-uuid:")
	assert.Contains(t, text, "secret_refs:")
	assert.NotContains(t, text, "snapshot")
	assert.NotContains(t, text, "resolved-secret")

	require.NoError(t, store.Remove("pipeline-uuid", "production"))
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestScheduleDeclarationStoreRejectsUnknownFieldsAndUnsafeReferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.yml")
	for _, body := range []string{
		"version: 2\n",
		"version: 1\nschedules:\n  pipeline:\n    prod:\n      cron: '@daily'\n      snapshot_version_id: secret\n",
		"version: 1\nschedules:\n  pipeline:\n    prod:\n      cron: '@daily'\n      secret_refs:\n        token: file:/tmp/token\n",
		"version: 1\n---\nversion: 1\n",
	} {
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		_, err := NewScheduleDeclarationStore(path).Load()
		require.Error(t, err, body)
	}
}

func TestScheduleDeclarationStoreDoesNotReplaceInvalidExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.yml")
	original := "version: invalid\n"
	require.NoError(t, os.WriteFile(path, []byte(original), 0o644))

	err := NewScheduleDeclarationStore(path).Set("pipeline", "prod", ScheduleDeclaration{Cron: "@daily"})
	require.Error(t, err)
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, original, string(data))
}

func TestScheduleDeclarationRejectsConflictingVariableSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.yml")
	err := NewScheduleDeclarationStore(path).Set("pipeline", "prod", ScheduleDeclaration{
		Cron:       "@daily",
		Variables:  map[string]any{"token": "ordinary"},
		SecretRefs: map[string]string{"token": "env:TOKEN"},
	})
	require.ErrorContains(t, err, "both a value and a secret reference")
	assert.False(t, strings.Contains(err.Error(), "ordinary"))
}
