package scheduler

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaterializationSourceMigrationBackfillsRetainedSnapshotRuns(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenStore(path)
	require.NoError(t, err)

	ctx := context.Background()
	migrations, err := fs.Sub(schedulerMigrations, "storedb/migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, store.db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 25)
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO pipeline_runs
			(id, pipeline_id, pipeline, environment, trigger, status, snapshot_version_id)
		VALUES ('scheduled-run', 'pipeline-id', 'pipeline', 'prod', 'schedule', 'succeeded', 'snapshot-7')`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO renart_materializations
			(asset_id, environment, fingerprint, own_content, vars_hash,
			 target_identity, target_generation, interval_start, interval_end,
			 run_id, materialized_at, completion_id, completion_ordinal)
		VALUES (
			'p:a', 'prod', 'v2:fingerprint', 'v2:own', 'vars',
			'target-a', 1, '', '',
			'scheduled-run', '2026-07-28T10:00:00Z', 'scheduled-run', 0
		)`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO renart_latest_successful_writers
			(target_identity, target_generation, asset_id, environment,
			 fingerprint, vars_hash, run_id, materialized_at,
			 completion_id, completion_ordinal, ambiguous)
		VALUES (
			'target-a', 1, 'p:a', 'prod',
			'v2:fingerprint', 'vars', 'scheduled-run', '2026-07-28T10:00:00Z',
			'scheduled-run', 0, 0
		)`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = OpenStore(path)
	require.NoError(t, err)
	defer store.Close()

	var factSource string
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT snapshot_version_id
		FROM renart_materializations
		WHERE run_id = 'scheduled-run'`).Scan(&factSource))
	assert.Equal(t, "snapshot-7", factSource)

	var writerSource string
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT snapshot_version_id
		FROM renart_latest_successful_writers
		WHERE target_identity = 'target-a'`).Scan(&writerSource))
	assert.Equal(t, "snapshot-7", writerSource)

	// The source remains durable even if normal run-history retention removes
	// the row from which the migration originally recovered it.
	_, err = store.db.ExecContext(ctx, `DELETE FROM pipeline_runs WHERE id = 'scheduled-run'`)
	require.NoError(t, err)
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT snapshot_version_id
		FROM renart_latest_successful_writers
		WHERE target_identity = 'target-a'`).Scan(&writerSource))
	assert.Equal(t, "snapshot-7", writerSource)
}
