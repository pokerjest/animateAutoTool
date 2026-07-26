package service

import (
	"errors"
	"testing"
	"time"
)

func TestGlobalScanStatusTracksAggregateSummary(t *testing.T) {
	tracker := &scanStatusTracker{}

	status := tracker.Begin(3)
	if !status.IsRunning || status.TotalDirectories != 3 {
		t.Fatalf("expected tracker to start with 3 directories, got %+v", status)
	}

	status = tracker.Advance("/library/A", &ScanResult{DiscoveredFiles: 24, CandidateSeries: 2, Added: 2, Updated: 1}, nil)
	if status.ProcessedDirectories != 1 || status.DiscoveredFiles != 24 || status.CandidateSeries != 2 || status.AddedCount != 2 || status.UpdatedCount != 1 {
		t.Fatalf("unexpected tracker progress after first directory: %+v", status)
	}

	status = tracker.Advance("/library/B", &ScanResult{WalkErrors: 1}, errors.New("permission denied"))
	if status.FailedDirectories != 1 || status.LastError != "permission denied" {
		t.Fatalf("expected tracker to record failed directory, got %+v", status)
	}

	time.Sleep(10 * time.Millisecond)
	status = tracker.Finish()
	if status.IsRunning {
		t.Fatalf("expected tracker to stop running after finish, got %+v", status)
	}
	if status.LastSummary == "" || status.LastDuration == "" {
		t.Fatalf("expected tracker to produce summary and duration, got %+v", status)
	}
}
