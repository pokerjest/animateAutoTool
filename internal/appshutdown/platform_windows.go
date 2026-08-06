//go:build windows

package appshutdown

import (
	"context"
	"fmt"
	"os"
	"sync"

	"golang.org/x/sys/windows"
)

const windowsControlEventPrefix = `Local\AnimateAutoTool-Shutdown-`

func windowsControlEventName(pid int) string {
	return fmt.Sprintf("%s%d", windowsControlEventPrefix, pid)
}

// StartPlatformListener registers the Windows named event used by stop.bat.
func StartPlatformListener(parent context.Context) (func(), error) {
	if parent == nil {
		parent = context.Background()
	}

	name, err := windows.UTF16PtrFromString(windowsControlEventName(os.Getpid()))
	if err != nil {
		return func() {}, fmt.Errorf("encode Windows shutdown event name: %w", err)
	}
	event, err := windows.CreateEvent(nil, 0, 0, name)
	if err != nil {
		if event != 0 {
			_ = windows.CloseHandle(event)
		}
		return func() {}, fmt.Errorf("create Windows shutdown event: %w", err)
	}

	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, waitErr := windows.WaitForSingleObject(event, windows.INFINITE)
		if waitErr == nil && result == windows.WAIT_OBJECT_0 && ctx.Err() == nil {
			Request("windows-control-event")
		}
	}()

	var cleanupOnce sync.Once
	return func() {
		cleanupOnce.Do(func() {
			cancel()
			_ = windows.SetEvent(event)
			<-done
			_ = windows.CloseHandle(event)
		})
	}, nil
}
