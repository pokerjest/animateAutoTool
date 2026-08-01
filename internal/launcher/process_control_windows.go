//go:build windows

package launcher

import (
	"fmt"
	"log"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type processControl struct {
	mu  sync.Mutex
	job windows.Handle
}

func newProcessControl() *processControl {
	control := &processControl{}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		log.Printf("WARN: failed to create managed service job object: %v", err)
		return control
	}

	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), //nolint:gosec // SetInformationJobObject requires the documented structure pointer.
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		log.Printf("WARN: failed to configure managed service job object: %v", err)
		return control
	}
	control.job = job
	return control
}

func (c *processControl) configure(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}

func (c *processControl) attach(cmd *exec.Cmd) error {
	if c == nil || cmd == nil || cmd.Process == nil {
		return nil
	}
	c.mu.Lock()
	job := c.job
	c.mu.Unlock()
	if job == 0 {
		return nil
	}

	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		return fmt.Errorf("open process %d: %w", cmd.Process.Pid, err)
	}
	defer func() { _ = windows.CloseHandle(process) }()
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		return fmt.Errorf("assign process %d to job: %w", cmd.Process.Pid, err)
	}
	return nil
}

func (c *processControl) close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	job := c.job
	c.job = 0
	c.mu.Unlock()
	if job == 0 {
		return nil
	}
	return windows.CloseHandle(job)
}
