package scheduler

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotOrdinalMigrationBackfillsPerPipelineDeterministically(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenStore(path)
	require.NoError(t, err)

	ctx := context.Background()
	migrations, err := fs.Sub(schedulerMigrations, "storedb/migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, store.db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 19)
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO renart_snapshots
			(version_id, pipeline_id, merkle_root, manifest, created_at)
		VALUES
			('version-z', 'pipeline-a', 'root-z', '{}', '2026-07-16T10:00:00Z'),
			('version-a', 'pipeline-a', 'root-a', '{}', '2026-07-16T10:00:00Z'),
			('version-old', 'pipeline-a', 'root-old', '{}', '2026-07-15T10:00:00Z'),
			('version-b', 'pipeline-b', 'root-b', '{}', '2026-07-18T10:00:00Z')`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = OpenStore(path)
	require.NoError(t, err)
	defer store.Close()

	rows, err := store.db.QueryContext(ctx, `
		SELECT version_id, ordinal
		FROM renart_snapshots
		ORDER BY pipeline_id, ordinal`)
	require.NoError(t, err)
	defer rows.Close()
	got := make([]string, 0, 4)
	for rows.Next() {
		var versionID string
		var ordinal int
		require.NoError(t, rows.Scan(&versionID, &ordinal))
		got = append(got, fmt.Sprintf("%s:%d", versionID, ordinal))
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"version-old:1", "version-a:2", "version-z:3", "version-b:1"}, got)

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO renart_snapshots
			(version_id, pipeline_id, ordinal, merkle_root, manifest, created_at)
		VALUES ('duplicate', 'pipeline-a', 3, 'root', '{}', '2026-07-18T11:00:00Z')`)
	require.Error(t, err, "one ordinal must identify exactly one deployment per pipeline")
}
