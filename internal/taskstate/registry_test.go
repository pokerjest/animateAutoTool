package taskstate

import (
	"errors"
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistryTracksProgressCompletionAndPublishesTypedEvents(t *testing.T) {
	registry := NewRegistry()
	events := make(chan Task, 4)
	subscriptionID := event.GlobalBus.Subscribe(event.EventTaskUpdate, func(message event.Event) {
		if task, ok := message.Payload.(Task); ok && task.TaskID == "scan-1" {
			events <- task
		}
	})
	t.Cleanup(func() { event.GlobalBus.Unsubscribe(event.EventTaskUpdate, subscriptionID) })

	registry.Start("scan-1", "scan", "本地扫描", "准备扫描")
	registry.Progress("scan-1", "正在扫描", 3, 7)
	completed := registry.Complete("scan-1", "扫描完成")

	assert.Equal(t, StatusCompleted, completed.Status)
	assert.Equal(t, int64(7), completed.Current)
	assert.Equal(t, int64(7), completed.Total)
	require.Len(t, registry.List(), 1)

	deadline := time.After(2 * time.Second)
	seenCompleted := false
	for received := 0; received < 3; received++ {
		select {
		case update := <-events:
			seenCompleted = seenCompleted || update.Status == StatusCompleted
		case <-deadline:
			t.Fatal("timed out waiting for task_update events")
		}
	}
	assert.True(t, seenCompleted)
}

func TestRegistryPreservesTaskIdentityOnFailure(t *testing.T) {
	registry := NewRegistry()
	registry.Start("sync-1", "sync", "立即同步", "正在同步")
	failed := registry.Fail("sync-1", errors.New("网络不可用"))

	assert.Equal(t, StatusError, failed.Status)
	assert.Equal(t, "sync", failed.Kind)
	assert.Equal(t, "立即同步", failed.Title)
	assert.Equal(t, "网络不可用", failed.Message)
}
