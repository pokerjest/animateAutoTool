package launcher

import (
	"context"
	"testing"
	"time"
)

func TestManagerDatabaseReadySignal(t *testing.T) {
	manager := NewManager(context.Background())
	manager.NotifyDatabaseReady()
	if !manager.waitForDatabase(time.Second) {
		t.Fatal("database readiness signal was not observed")
	}
	manager.StopAll()
}

func TestManagerDatabaseWaitStopsOnCancellation(t *testing.T) {
	manager := NewManager(context.Background())
	manager.Cancel()
	if manager.waitForDatabase(time.Second) {
		t.Fatal("database wait succeeded after manager cancellation")
	}
	manager.StopAll()
}
