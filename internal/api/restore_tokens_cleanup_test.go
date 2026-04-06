package api

import (
	"os"
	"testing"
	"time"
)

func TestCleanupExpiredRestoreArtifactsRemovesExpiredFile(t *testing.T) {
	clearRestoreArtifactsForTest()
	t.Cleanup(clearRestoreArtifactsForTest)

	tmp, err := os.CreateTemp("", "restore-expired-*.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	path := tmp.Name()
	_ = tmp.Close()

	token := "expired-token"
	restoreArtifacts.Store(token, restoreArtifact{
		Path:      path,
		CreatedAt: time.Now().Add(-restoreTokenTTL - time.Minute),
	})

	cleanupExpiredRestoreArtifacts(time.Now())

	if _, ok := restoreArtifacts.Load(token); ok {
		t.Fatal("expected expired artifact to be removed from map")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatal("expected expired artifact file to be removed")
	}
}

func TestCleanupExpiredRestoreArtifactsKeepsFreshFile(t *testing.T) {
	clearRestoreArtifactsForTest()
	t.Cleanup(clearRestoreArtifactsForTest)

	tmp, err := os.CreateTemp("", "restore-fresh-*.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	path := tmp.Name()
	_ = tmp.Close()
	t.Cleanup(func() { _ = os.Remove(path) })

	token := "fresh-token"
	restoreArtifacts.Store(token, restoreArtifact{
		Path:      path,
		CreatedAt: time.Now(),
	})

	cleanupExpiredRestoreArtifacts(time.Now())

	if _, ok := restoreArtifacts.Load(token); !ok {
		t.Fatal("expected fresh artifact to stay in map")
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("expected fresh artifact file to exist: %v", statErr)
	}
}

func clearRestoreArtifactsForTest() {
	restoreArtifacts.Range(func(key, _ interface{}) bool {
		restoreArtifacts.Delete(key)
		return true
	})
}
