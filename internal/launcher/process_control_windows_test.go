//go:build windows

package launcher

import (
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestProcessControlConfiguresHiddenWindow(t *testing.T) {
	control := newProcessControl()
	defer func() { _ = control.close() }()

	cmd := exec.Command("cmd.exe", "/d", "/c", "exit 0")
	control.configure(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatal("managed Windows process was not configured to hide its window")
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatal("managed Windows process is missing CREATE_NO_WINDOW")
	}
}

func TestProcessControlKillsAttachedProcessWhenClosed(t *testing.T) {
	control := newProcessControl()
	if control.job == 0 {
		t.Skip("Windows Job Object is unavailable")
	}

	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 30")
	control.configure(cmd)
	if err := cmd.Start(); err != nil {
		_ = control.close()
		t.Fatalf("start child process: %v", err)
	}
	if err := control.attach(cmd); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = control.close()
		t.Skipf("cannot attach child process to Job Object in this environment: %v", err)
	}

	waited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(waited)
	}()
	if err := control.close(); err != nil {
		t.Fatalf("close Job Object: %v", err)
	}
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("attached process survived Job Object close")
	}
}
