package service

import (
	"errors"
	"testing"
	"time"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/scheduler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmitDirectRunAssetEventIdentifiesQualityChecks(t *testing.T) {
	t.Parallel()
	asset := &pipeline.Asset{Name: "analytics.orders"}
	base := &scheduler.AssetInstance{Asset: asset}
	nonBlocking := false
	started := time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	finished := started.Add(time.Second)
	runErr := errors.New("assertion failed")

	var events []ExecutionAssetEvent
	emit := func(event ExecutionAssetEvent) error {
		events = append(events, event)
		return nil
	}
	require.NoError(t, emitDirectRunAssetEvent(
		emit,
		&scheduler.CustomCheckInstance{
			AssetInstance: base,
			Check: &pipeline.CustomCheck{
				Name: "no invalid orders", Blocking: pipeline.DefaultTrueBool{Value: &nonBlocking},
			},
		},
		"failed",
		started,
		finished,
		runErr,
	))
	require.NoError(t, emitDirectRunAssetEvent(
		emit,
		&scheduler.ColumnCheckInstance{
			AssetInstance: base,
			Column:        &pipeline.Column{Name: "order_id"},
			Check:         &pipeline.ColumnCheck{Name: "not_null"},
		},
		"success",
		started,
		finished,
		nil,
	))

	require.Len(t, events, 2)
	assert.Equal(t, ExecutionAssetEvent{
		Asset:         "analytics.orders",
		Status:        "failed",
		TaskKind:      executionTaskKindCustomCheck,
		CheckName:     "no invalid orders",
		CheckBlocking: false,
		StartedAt:     &started,
		FinishedAt:    &finished,
		Error:         runErr.Error(),
	}, events[0])
	assert.Equal(t, executionTaskKindColumnCheck, events[1].TaskKind)
	assert.Equal(t, "not_null", events[1].CheckName)
	assert.Equal(t, "order_id", events[1].CheckColumn)
	assert.True(t, events[1].CheckBlocking)
}
