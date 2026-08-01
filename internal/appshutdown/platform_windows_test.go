//go:build windows

package appshutdown

import (
	"context"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsControlEventRequestsShutdown(t *testing.T) {
	drainRequests()
	t.Cleanup(drainRequests)

	ctx, cancel := context.WithCancel(context.Background())
	cleanup, err := StartPlatformListener(ctx)
	if err != nil {
		t.Fatalf("StartPlatformListener failed: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		cleanup()
	})

	name, err := windows.UTF16PtrFromString(windowsControlEventName(os.Getpid()))
	if err != nil {
		t.Fatalf("encode event name: %v", err)
	}
	event, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		t.Fatalf("OpenEvent failed: %v", err)
	}
	defer func() { _ = windows.CloseHandle(event) }()
	if err := windows.SetEvent(event); err != nil {
		t.Fatalf("SetEvent failed: %v", err)
	}

	select {
	case reason := <-Requests():
		if reason != "windows-control-event" {
			t.Fatalf("shutdown reason = %q", reason)
		}
	case <-time.After(time.Second):
		t.Fatal("Windows control event did not request shutdown")
	}
}
