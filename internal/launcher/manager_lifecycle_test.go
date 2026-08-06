package launcher

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStartAllStopsStartedServicesWhenQBStartFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	manager := &Manager{Ctx: ctx, Cancel: cancel}
	workerStopped := make(chan struct{})
	manager.startAlistFn = func() error {
		manager.wg.Add(1)
		go func() {
			defer manager.wg.Done()
			<-manager.Ctx.Done()
			close(workerStopped)
		}()
		return nil
	}
	expected := errors.New("qBittorrent start failed")
	manager.startQBFunc = func() error { return expected }
	manager.startJellyfinFunc = func() error {
		t.Fatal("Jellyfin must not start after qBittorrent failure")
		return nil
	}

	if err := manager.StartAll(); !errors.Is(err, expected) {
		t.Fatalf("StartAll error = %v, want %v", err, expected)
	}
	select {
	case <-workerStopped:
	case <-time.After(time.Second):
		t.Fatal("started service was not stopped before StartAll returned")
	}
	manager.StopAll()
}
