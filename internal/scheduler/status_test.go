package scheduler

import "testing"

func TestStatusTrackerBeginAndFinish(t *testing.T) {
	tracker := &statusTracker{}

	tracker.Begin("auto", 5)
	running := tracker.Snapshot()
	if !running.IsRunning {
		t.Fatal("expected scheduler status to be running after begin")
	}
	if running.TotalSubscriptions != 5 {
		t.Fatalf("expected total subscriptions to be 5, got %d", running.TotalSubscriptions)
	}

	final := tracker.Finish(3, 1, 1, 5, "auto", "sample error")
	if final.IsRunning {
		t.Fatal("expected scheduler status to stop after finish")
	}
	if final.SuccessCount != 3 || final.WarningCount != 1 || final.ErrorCount != 1 {
		t.Fatalf("unexpected final counts: %+v", final)
	}
	if final.LastSummary == "" {
		t.Fatal("expected final summary to be recorded")
	}
	if final.LastError != "sample error" {
		t.Fatalf("unexpected last error: %q", final.LastError)
	}
}

func TestSchedulerRunGuard(t *testing.T) {
	schedulerRunInProgress.Store(false)
	t.Cleanup(func() {
		schedulerRunInProgress.Store(false)
	})

	if !schedulerRunInProgress.CompareAndSwap(false, true) {
		t.Fatal("expected first scheduler run guard acquisition to succeed")
	}
	if schedulerRunInProgress.CompareAndSwap(false, true) {
		t.Fatal("expected second scheduler run guard acquisition to fail while running")
	}
	if !IsRunInProgress() {
		t.Fatal("expected scheduler run to report in progress")
	}

	schedulerRunInProgress.Store(false)
	if !schedulerRunInProgress.CompareAndSwap(false, true) {
		t.Fatal("expected scheduler run guard to be acquirable again after release")
	}
}
