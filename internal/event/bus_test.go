package event

import (
	"sync/atomic"
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

func TestWaitDrainsHandlersAndRejectsLaterPublications(t *testing.T) {
	bus := NewInMemoryBus()
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int32

	bus.Subscribe(EventScanRun, func(Event) {
		calls.Add(1)
		started <- struct{}{}
		<-release
	})

	bus.Publish(EventScanRun, nil)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("expected first handler to start")
	}

	waitDone := make(chan struct{})
	go func() {
		bus.Wait()
		close(waitDone)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		bus.mu.RLock()
		closed := bus.closed
		bus.mu.RUnlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expected Wait to close the bus")
		}
		time.Sleep(time.Millisecond)
	}

	bus.Publish(EventScanRun, nil)
	close(release)

	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after the active handler completed")
	}

	bus.Publish(EventScanRun, nil)
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected only the pre-Wait publication to run, got %d calls", got)
	}
}
