package service

import (
	"context"
	"errors"
	"testing"
)

func TestScanAllWithProgressContextRejectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewScannerService().ScanAllWithProgressContext(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}
