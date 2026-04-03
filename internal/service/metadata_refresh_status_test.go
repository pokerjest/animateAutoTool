package service

import "testing"

func TestRefreshStatusTrackerBlocksConcurrentStartAndResetsAfterFinish(t *testing.T) {
	tracker := &refreshStatusTracker{}

	if !tracker.TryStart() {
		t.Fatal("expected first refresh start to succeed")
	}
	if tracker.TryStart() {
		t.Fatal("expected second concurrent refresh start to be rejected")
	}

	tracker.SetTotal(3)
	tracker.UpdateProgress(2, "Example")

	snapshot := tracker.Snapshot()
	if !snapshot.IsRunning {
		t.Fatal("expected tracker to report running state")
	}
	if snapshot.Total != 3 || snapshot.Current != 2 || snapshot.CurrentTitle != "Example" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}

	final := tracker.Finish("done")
	if final.IsRunning {
		t.Fatal("expected tracker to stop after finish")
	}
	if final.LastResult != "done" {
		t.Fatalf("expected final result to be recorded, got %q", final.LastResult)
	}

	if !tracker.TryStart() {
		t.Fatal("expected tracker to allow a new refresh after finish")
	}
}
