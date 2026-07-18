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

func TestMaterializationTargetMigrationPreservesLegacyRowsAsGenerationZero(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenStore(path)
	require.NoError(t, err)

	ctx := context.Background()
	migrations, err := fs.Sub(schedulerMigrations, "storedb/migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, store.db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 12)
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO renart_materializations
			(asset_id, environment, fingerprint, own_content, vars_hash,
			 interval_start, interval_end, run_id, materialized_at)
		VALUES ('p:a', 'prod', 'v1:legacy', 'own', 'vars', '', '', 'run', '2026-01-01T01:00:00Z')`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO renart_coverage
			(asset_id, environment, fingerprint, own_content, vars_hash,
			 interval_start, interval_end, materialized_at)
		VALUES ('p:a', 'prod', 'v1:legacy', 'own', 'vars', '', '', '2026-01-01T01:00:00Z')`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = OpenStore(path)
	require.NoError(t, err)
	defer store.Close()

	var factTarget string
	var factGeneration int64
	var factCompletionID string
	var factCompletionOrdinal int64
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT target_identity, target_generation, completion_id, completion_ordinal
		FROM renart_materializations
		WHERE asset_id = 'p:a'`).Scan(
		&factTarget,
		&factGeneration,
		&factCompletionID,
		&factCompletionOrdinal,
	))
	assert.Empty(t, factTarget)
	assert.Zero(t, factGeneration)
	assert.Empty(t, factCompletionID)
	assert.Zero(t, factCompletionOrdinal)

	var coverageTarget string
	var coverageGeneration int64
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT target_identity, target_generation
		FROM renart_coverage
		WHERE asset_id = 'p:a'`).Scan(&coverageTarget, &coverageGeneration))
	assert.Empty(t, coverageTarget)
	assert.Zero(t, coverageGeneration)
	assert.Equal(t, 0, countRows(t, store, `SELECT COUNT(*) FROM renart_latest_successful_writers`))

	// The rebuilt primary key keeps otherwise identical coverage for distinct
	// physical targets and generations separate.
	for _, row := range []struct {
		target     string
		generation int64
	}{
		{target: "target-a", generation: 1},
		{target: "target-a", generation: 2},
		{target: "target-b", generation: 1},
	} {
		_, err = store.db.ExecContext(ctx, `
			INSERT INTO renart_coverage
				(asset_id, environment, fingerprint, own_content, vars_hash,
				 target_identity, target_generation, interval_start, interval_end, materialized_at)
			VALUES ('p:a', 'prod', 'v1:legacy', 'own', 'vars', ?, ?, '', '', '2026-01-01T02:00:00Z')`,
			row.target,
			row.generation,
		)
		require.NoError(t, err)
	}
	assert.Equal(t, 4, countRows(t, store, `SELECT COUNT(*) FROM renart_coverage`))
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO renart_materializations
			(asset_id, environment, fingerprint, own_content, vars_hash,
			 target_identity, target_generation, interval_start, interval_end,
			 run_id, materialized_at, completion_id, completion_ordinal)
		VALUES (
			'p:a', 'prod', 'v1:targeted', 'own', 'vars',
			'target-a', 1, '', '', 'targeted-run', '2026-01-01T02:00:00Z',
			'targeted-completion', 2
		)`)
	require.NoError(t, err)
	assert.Equal(t, 2, countRows(t, store, `SELECT COUNT(*) FROM renart_materializations`))

	// Downgrading cannot safely collapse targeted generations into the legacy
	// key. It preserves only the original generation-zero row and removes the
	// target columns cleanly.
	migrations, err = fs.Sub(schedulerMigrations, "storedb/migrations")
	require.NoError(t, err)
	provider, err = goose.NewProvider(goose.DialectSQLite3, store.db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 12)
	require.NoError(t, err)
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM renart_coverage`))
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM renart_materializations`))
	assert.Equal(t, 1, countRows(t, store, `
		SELECT COUNT(*) FROM renart_materializations WHERE fingerprint = 'v1:legacy'`))
	assert.Equal(t, 0, countRows(t, store, `
		SELECT COUNT(*) FROM pragma_table_info('renart_coverage')
		WHERE name IN ('target_identity', 'target_generation')`))
	assert.Equal(t, 0, countRows(t, store, `
		SELECT COUNT(*) FROM pragma_table_info('renart_materializations')
		WHERE name IN ('target_identity', 'target_generation', 'completion_id', 'completion_ordinal')`))
	assert.Equal(t, 0, countRows(t, store, `
		SELECT COUNT(*) FROM sqlite_schema
		WHERE type = 'table' AND name = 'renart_latest_successful_writers'`))
}
