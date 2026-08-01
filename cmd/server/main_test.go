package main

import (
	"strings"
	"testing"
	"time"
)

func TestWaitForShutdownTasksCompletes(t *testing.T) {
	finished := make(chan struct{})
	err := waitForShutdownTasks(time.Second, func() { close(finished) })
	if err != nil {
		t.Fatalf("waitForShutdownTasks returned error: %v", err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("shutdown task did not run")
	}
}

func TestWaitForShutdownTasksTimesOut(t *testing.T) {
	blocked := make(chan struct{})
	err := waitForShutdownTasks(20*time.Millisecond, func() { <-blocked })
	close(blocked)
	if err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("expected shutdown timeout, got %v", err)
	}
}
