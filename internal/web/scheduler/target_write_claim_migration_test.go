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

func TestTargetWriteClaimMigrationCreatesConstrainedDurableTable(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()

	assert.Equal(t, 1, countRows(t, store, `
		SELECT COUNT(*)
		FROM sqlite_schema
		WHERE type = 'table' AND name = 'renart_target_write_claims'`))
	assert.Equal(t, 1, countRows(t, store, `
		SELECT COUNT(*)
		FROM sqlite_schema
		WHERE type = 'index' AND name = 'idx_renart_target_write_claims_target_state'`))

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO renart_target_write_claims
			(target_identity, completion_id, asset_id, state, claimed_at, updated_at)
		VALUES ('target', 'completion', 'asset', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO renart_target_write_claims
			(target_identity, completion_id, asset_id, state, claimed_at, updated_at)
		VALUES ('target-2', 'completion', 'asset', 'unknown', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	require.Error(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO renart_target_write_claims
			(target_identity, completion_id, asset_id, state, claimed_at, updated_at)
		VALUES ('target', 'completion', 'asset', 'dirty', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	require.Error(t, err, "one target/completion/asset coordinate must identify one durable claim")

	migrations, err := fs.Sub(schedulerMigrations, "storedb/migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, store.db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 14)
	require.NoError(t, err)
	assert.Equal(t, 0, countRows(t, store, `
		SELECT COUNT(*)
		FROM sqlite_schema
		WHERE type = 'table' AND name = 'renart_target_write_claims'`))
}
