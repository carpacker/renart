package bus

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBusDeliversInSubscriptionOrder(t *testing.T) {
	t.Parallel()
	b := New()
	var order []string
	b.OnRunCompleted(func(RunCompleted) error { order = append(order, "first"); return nil })
	b.OnRunCompleted(func(RunCompleted) error { order = append(order, "second"); return nil })

	assert.NoError(t, b.EmitRunCompleted(RunCompleted{PipelineUUID: "p", CompletedAt: time.Now()}))
	assert.Equal(t, []string{"first", "second"}, order)
}

func TestBusReturnsCompletionErrorsAfterCallingEverySubscriber(t *testing.T) {
	t.Parallel()
	b := New()
	var order []string
	b.OnRunCompleted(func(RunCompleted) error {
		order = append(order, "first")
		return errors.New("persist failed")
	})
	b.OnRunCompleted(func(RunCompleted) error {
		order = append(order, "second")
		return nil
	})

	err := b.EmitRunCompleted(RunCompleted{PipelineUUID: "p", CompletedAt: time.Now()})
	assert.EqualError(t, err, "persist failed")
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

func TestBusPublishesTargetWriteChangesAndUnsubscribes(t *testing.T) {
	t.Parallel()
	b := New()
	var events []TargetWriteChanged
	unsubscribe := b.OnTargetWriteChanged(func(event TargetWriteChanged) {
		events = append(events, event)
	})

	b.EmitTargetWriteChanged(TargetWriteChanged{PipelineUUID: "p", AssetID: "p:a"})
	unsubscribe()
	b.EmitTargetWriteChanged(TargetWriteChanged{PipelineUUID: "p", AssetID: "p:b"})

	assert.Equal(t, []TargetWriteChanged{{PipelineUUID: "p", AssetID: "p:a"}}, events)
}

func TestNilBusEmitIsSafe(t *testing.T) {
	t.Parallel()
	var b *Bus
	assert.NoError(t, b.EmitRunCompleted(RunCompleted{}))
	b.EmitAssetSaved(AssetSaved{})
	b.EmitTargetWriteChanged(TargetWriteChanged{})
}
