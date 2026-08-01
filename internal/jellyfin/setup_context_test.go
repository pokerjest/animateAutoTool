package jellyfin

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAttemptZeroConfigContextStopsWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	_, err := AttemptZeroConfigContext(ctx, "http://127.0.0.1:1", "admin", "password")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("canceled zero-config took too long: %v", elapsed)
	}
}
