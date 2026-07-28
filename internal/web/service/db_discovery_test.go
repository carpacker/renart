package service

import (
	"context"
	"errors"
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInspectPipelineMaterializationsDistinguishesUnavailableConnection(t *testing.T) {
	t.Parallel()

	asset := &pipeline.Asset{
		Name:       "new.postgres_test",
		Type:       pipeline.AssetTypePostgresQuery,
		Connection: "postgres-default",
		Materialization: pipeline.Materialization{
			Type: pipeline.MaterializationTypeView,
		},
	}
	parsed := &pipeline.Pipeline{Assets: []*pipeline.Asset{asset}}
	key := MaterializationAssetKey(asset.Name, asset.Connection)

	t.Run("locked vault is unknown", func(t *testing.T) {
		t.Parallel()
		service := NewExecutionService(ExecutionDependencies{
			Executor: &stubExecutionExecutor{
				queryConnErr: errors.New("secret is not configured: encrypted vault is locked"),
			},
		})

		info := service.inspectPipelineMaterializations(context.Background(), parsed, "default")

		require.Contains(t, info, key)
		assert.False(t, info[key].VerificationAvailable)
		assert.False(t, info[key].IsMaterialized)
	})

	t.Run("successful empty warehouse is confirmed absent", func(t *testing.T) {
		t.Parallel()
		service := NewExecutionService(ExecutionDependencies{
			Executor: &stubExecutionExecutor{queryConnOutput: []byte("[]")},
		})

		info := service.inspectPipelineMaterializations(context.Background(), parsed, "default")

		require.Contains(t, info, key)
		assert.True(t, info[key].VerificationAvailable)
		assert.False(t, info[key].IsMaterialized)
	})
}

func TestFetchObjectsClearsAnEarlierDialectErrorAfterSuccessfulFallback(t *testing.T) {
	t.Parallel()

	call := 0
	executor := &stubExecutionExecutor{
		queryConnection: func(QueryConnectionRequest) ([]byte, error) {
			call++
			if call == 1 {
				return nil, errors.New("information_schema is unsupported")
			}
			return []byte("[]"), nil
		},
	}
	service := NewExecutionService(ExecutionDependencies{Executor: executor})

	objects, err := service.fetchObjectsForConnection(context.Background(), "warehouse", "default")

	require.NoError(t, err)
	assert.Empty(t, objects)
	assert.Equal(t, 2, call)
}
