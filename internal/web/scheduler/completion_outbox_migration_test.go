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

func TestCompletionOutboxMigrationAppliesAndDowngradesCleanly(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()

	assert.Equal(t, 1, countRows(t, store, `
		SELECT COUNT(*) FROM sqlite_schema
		WHERE type = 'table' AND name = 'renart_completion_outbox'`))
	assert.Equal(t, 5, countRows(t, store, `
		SELECT COUNT(*) FROM pragma_table_info('renart_completion_outbox')`))

	// The database contract independently rejects an invalid durable key even
	// when a caller bypasses the typed outbox store.
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO renart_completion_outbox
			(completion_id, version, body, enqueued_at)
		VALUES
			(' bad', 1, '{"version":1,"event":{"completion_id":" bad"}}', '2026-07-17T12:00:00Z')`)
	require.Error(t, err)

	migrations, err := fs.Sub(schedulerMigrations, "storedb/migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, store.db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 15)
	require.NoError(t, err)
	assert.Equal(t, 0, countRows(t, store, `
		SELECT COUNT(*) FROM sqlite_schema
		WHERE type = 'table' AND name = 'renart_completion_outbox'`))

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, countRows(t, store, `
		SELECT COUNT(*) FROM sqlite_schema
		WHERE type = 'table' AND name = 'renart_completion_outbox'`))
}
