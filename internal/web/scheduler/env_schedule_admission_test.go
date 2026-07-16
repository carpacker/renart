package scheduler

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertEnvScheduleRejectsConflictingSourceChoices(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	var deployCalls atomic.Int32
	var validationCalls atomic.Int32
	service := New(Options{
		Store: store,
		DeployPipeline: func(context.Context, string) (string, error) {
			deployCalls.Add(1)
			return "deployed", nil
		},
		ValidateSnapshot: func(context.Context, string, string) error {
			validationCalls.Add(1)
			return nil
		},
	})

	_, err = service.UpsertEnvSchedule(context.Background(), "pipeline-uuid", UpsertEnvScheduleRequest{
		Environment:       "prod",
		Cron:              "@daily",
		DeployNow:         true,
		SnapshotVersionID: "snapshot-id",
	})
	require.ErrorContains(t, err, "mutually exclusive")
	assert.Zero(t, deployCalls.Load())
	assert.Zero(t, validationCalls.Load())

	rows, err := store.ListEnvSchedules(context.Background())
	require.NoError(t, err)
	assert.Empty(t, rows)
}
