package api

import (
	"errors"
	"mime/multipart"
	"strings"
	"testing"
)

func TestSaveUploadedBackupRejectsOversizedHeader(t *testing.T) {
	_, err := saveUploadedBackup(&multipart.FileHeader{Size: maxBackupFileBytes + 1})
	if !errors.Is(err, errBackupFileTooLarge) {
		t.Fatalf("expected oversized backup error, got %v", err)
	}
}

func TestR2ConnectionTestKeyIsUniqueAndIsolated(t *testing.T) {
	first := r2ConnectionTestKey()
	second := r2ConnectionTestKey()
	if first == second {
		t.Fatalf("connection test keys must be unique: %q", first)
	}
	for _, key := range []string{first, second} {
		if !strings.HasPrefix(key, ".animate-auto-tool/connection-tests/") || !strings.HasSuffix(key, ".txt") {
			t.Fatalf("unexpected connection test key %q", key)
		}
	}
}
