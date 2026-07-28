package scheduler

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWarehouseResourceClaimMigrationPreservesExistingClaims(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenStore(path)
	require.NoError(t, err)

	ctx := context.Background()
	migrations, err := fs.Sub(schedulerMigrations, "storedb/migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, store.db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 24)
	require.NoError(t, err)

	localClaim := PipelineRunResourceClaim{
		Kind:     PipelineRunResourceKindLocalFile,
		Identity: strings.Repeat("a", 64),
	}
	localRun, localSpec, localPlan := resourceClaimAdmission(
		t,
		"local-run",
		"local-pipeline",
		"local-pipeline-uuid",
		PipelineRunPlanResources{
			Isolation: PipelineRunResourceIsolationResources,
			Claims:    []PipelineRunResourceClaim{localClaim},
		},
	)
	_, err = store.CreateWithSpecAndPlan(ctx, localRun, localSpec, localPlan)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = OpenStore(path)
	require.NoError(t, err)
	defer store.Close()

	assert.Equal(t, 1, countRows(
		t,
		store,
		`SELECT COUNT(*) FROM pipeline_run_resource_claims
		 WHERE run_id = ? AND kind = ? AND identity = ?`,
		localRun.ID,
		localClaim.Kind,
		localClaim.Identity,
	))

	warehouseClaim := PipelineRunResourceClaim{
		Kind:     PipelineRunResourceKindWarehouse,
		Identity: strings.Repeat("b", 64),
	}
	warehouseRun, warehouseSpec, warehousePlan := resourceClaimAdmission(
		t,
		"warehouse-run",
		"warehouse-pipeline",
		"warehouse-pipeline-uuid",
		PipelineRunPlanResources{
			Isolation: PipelineRunResourceIsolationResources,
			Claims:    []PipelineRunResourceClaim{warehouseClaim},
		},
	)
	_, err = store.CreateWithSpecAndPlan(ctx, warehouseRun, warehouseSpec, warehousePlan)
	require.NoError(t, err)
	assert.Equal(t, 1, countRows(
		t,
		store,
		`SELECT COUNT(*) FROM pipeline_run_resource_claims
		 WHERE run_id = ? AND kind = ? AND identity = ?`,
		warehouseRun.ID,
		warehouseClaim.Kind,
		warehouseClaim.Identity,
	))
}
