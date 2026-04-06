package api

import (
	"testing"
	"time"
)

func TestCleanupStaleR2ProgressRemovesExpiredTerminalTasks(t *testing.T) {
	clearR2ProgressForTest()
	t.Cleanup(clearR2ProgressForTest)

	progressMap.Store("done", &DownloadProgress{
		TaskID:    "done",
		Status:    "completed",
		UpdatedAt: time.Now().Add(-r2ProgressTerminalTTL - time.Minute),
	})

	cleanupStaleR2Progress(time.Now())

	if _, ok := progressMap.Load("done"); ok {
		t.Fatal("expected expired completed task to be removed")
	}
}

func TestCleanupStaleR2ProgressRemovesExpiredActiveTasks(t *testing.T) {
	clearR2ProgressForTest()
	t.Cleanup(clearR2ProgressForTest)

	progressMap.Store("active-old", &DownloadProgress{
		TaskID:    "active-old",
		Status:    "downloading",
		UpdatedAt: time.Now().Add(-r2ProgressActiveTTL - time.Minute),
	})

	cleanupStaleR2Progress(time.Now())

	if _, ok := progressMap.Load("active-old"); ok {
		t.Fatal("expected expired active task to be removed")
	}
}

func TestCleanupStaleR2ProgressKeepsFreshTasks(t *testing.T) {
	clearR2ProgressForTest()
	t.Cleanup(clearR2ProgressForTest)

	progressMap.Store("active-fresh", &DownloadProgress{
		TaskID:    "active-fresh",
		Status:    "downloading",
		UpdatedAt: time.Now(),
	})
	progressMap.Store("done-fresh", &DownloadProgress{
		TaskID:    "done-fresh",
		Status:    "completed",
		UpdatedAt: time.Now().Add(-30 * time.Minute),
	})

	cleanupStaleR2Progress(time.Now())

	if _, ok := progressMap.Load("active-fresh"); !ok {
		t.Fatal("expected fresh active task to remain")
	}
	if _, ok := progressMap.Load("done-fresh"); !ok {
		t.Fatal("expected fresh completed task to remain")
	}
}

func clearR2ProgressForTest() {
	progressMap.Range(func(key, _ interface{}) bool {
		progressMap.Delete(key)
		return true
	})
}
