package event

import (
	"testing"
	"time"
)

func TestPublishSurvivesHandlerPanic(t *testing.T) {
	bus := NewInMemoryBus()

	received := make(chan struct{}, 1)
	bus.Subscribe(EventScanRun, func(Event) {
		panic("boom")
	})
	bus.Subscribe(EventScanRun, func(Event) {
		received <- struct{}{}
	})

	bus.Publish(EventScanRun, map[string]string{"source": "test"})

	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("expected non-panicking handler to receive event")
	}
}
