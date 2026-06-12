package bus

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBusDeliversInSubscriptionOrder(t *testing.T) {
	t.Parallel()
	b := New()
	var order []string
	b.OnRunCompleted(func(RunCompleted) { order = append(order, "first") })
	b.OnRunCompleted(func(RunCompleted) { order = append(order, "second") })

	b.EmitRunCompleted(RunCompleted{PipelineUUID: "p", CompletedAt: time.Now()})
	assert.Equal(t, []string{"first", "second"}, order)
}

func TestBusUnsubscribe(t *testing.T) {
	t.Parallel()
	b := New()
	calls := 0
	unsubscribe := b.OnAssetSaved(func(AssetSaved) { calls++ })

	b.EmitAssetSaved(AssetSaved{AssetID: "p:a"})
	unsubscribe()
	b.EmitAssetSaved(AssetSaved{AssetID: "p:a"})
	assert.Equal(t, 1, calls)
}

func TestNilBusEmitIsSafe(t *testing.T) {
	t.Parallel()
	var b *Bus
	b.EmitRunCompleted(RunCompleted{})
	b.EmitAssetSaved(AssetSaved{})
}
