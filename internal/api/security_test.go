package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildSafeRenamePathRejectsTraversal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "show")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("failed to create root dir: %v", err)
	}

	if _, err := buildSafeRenamePath(root, filepath.Join("Season 01", "Episode 01.mkv")); err != nil {
		t.Fatalf("expected nested path inside root to be allowed: %v", err)
	}

	if _, err := buildSafeRenamePath(root, filepath.Join("..", "escape.mkv")); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

func TestRestoreArtifactUsesOpaqueToken(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "restore-token-*.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	_ = tmpFile.Close()

	token := registerRestoreArtifact(tmpFile.Name())
	if token == "" {
		t.Fatal("expected restore token to be generated")
	}
	if token == tmpFile.Name() {
		t.Fatal("restore token must not expose the server file path")
	}

	resolvedPath, err := resolveRestoreArtifact(token)
	if err != nil {
		t.Fatalf("failed to resolve restore token: %v", err)
	}
	if resolvedPath != tmpFile.Name() {
		t.Fatalf("expected %s, got %s", tmpFile.Name(), resolvedPath)
	}
}

func TestRestoreArtifactExpires(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "restore-expire-*.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	path := tmpFile.Name()
	_ = tmpFile.Close()

	token := registerRestoreArtifact(path)
	restoreArtifacts.Store(token, restoreArtifact{
		Path:      path,
		CreatedAt: time.Now().Add(-restoreTokenTTL - time.Minute),
	})

	if _, err := resolveRestoreArtifact(token); err == nil {
		t.Fatal("expected expired restore token to be rejected")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatal("expected expired restore artifact to be cleaned up")
	}
}
