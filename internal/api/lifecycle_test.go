package api

import (
	"context"
	"sync"
	"testing"
	"time"
)

func resetBackgroundTasksForTest(t *testing.T) {
	t.Helper()
	appBackgroundTasks.mu.Lock()
	if appBackgroundTasks.cancel != nil {
		appBackgroundTasks.cancel()
	}
	appBackgroundTasks.mu.Unlock()
	appBackgroundTasks.wg.Wait()
	appBackgroundTasks.mu.Lock()
	appBackgroundTasks.ctx = nil
	appBackgroundTasks.cancel = nil
	appBackgroundTasks.accepting = false
	appBackgroundTasks.started = false
	appBackgroundTasks.mu.Unlock()
	t.Cleanup(func() {
		StopBackgroundTasks()
		WaitBackgroundTasks()
		appBackgroundTasks.mu.Lock()
		appBackgroundTasks.ctx = nil
		appBackgroundTasks.cancel = nil
		appBackgroundTasks.accepting = false
		appBackgroundTasks.started = false
		appBackgroundTasks.mu.Unlock()
	})
}

func TestGoBackgroundUsesTrackedDefaultBeforeExplicitStart(t *testing.T) {
	resetBackgroundTasksForTest(t)

	done := make(chan struct{})
	if !GoBackground(func(context.Context) { close(done) }) {
		t.Fatal("expected default background registry to accept work")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("background task did not run")
	}
	WaitBackgroundTasks()
}

func TestStopBackgroundTasksCancelsWaitsAndRejectsNewWork(t *testing.T) {
	resetBackgroundTasksForTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	StartBackgroundTasks(ctx)

	started := make(chan struct{})
	finished := make(chan struct{})
	var once sync.Once
	if !GoBackground(func(taskCtx context.Context) {
		once.Do(func() { close(started) })
		<-taskCtx.Done()
		close(finished)
	}) {
		t.Fatal("expected started registry to accept work")
	}
	<-started
	StopBackgroundTasks()
	if GoBackground(func(context.Context) {}) {
		t.Fatal("expected stopped registry to reject new work")
	}
	WaitBackgroundTasks()
	select {
	case <-finished:
	default:
		t.Fatal("expected cancellation-aware task to finish before Wait returned")
	}
}
