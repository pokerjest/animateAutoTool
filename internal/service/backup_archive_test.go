package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	securezip "github.com/yeka/zip"
)

func TestEncryptedBackupArchiveRoundTrip(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.db")
	source := append([]byte("SQLite format 3\x00"), []byte("backup payload\n")...)
	if err := os.WriteFile(sourcePath, source, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	archivePath := filepath.Join(t.TempDir(), "backup.zip")
	password := "  password with spaces  "

	if err := CreateEncryptedBackupArchive(sourcePath, archivePath, BackupModeFull, password); err != nil {
		t.Fatalf("create archive: %v", err)
	}

	reader, err := securezip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	if len(reader.File) != 2 {
		reader.Close()
		t.Fatalf("archive entry count = %d, want 2", len(reader.File))
	}
	for _, entry := range reader.File {
		if !entry.IsEncrypted() {
			reader.Close()
			t.Fatalf("entry %q is not encrypted", entry.Name)
		}
	}
	_ = reader.Close()

	extractedPath := filepath.Join(t.TempDir(), "extracted.db")
	manifest, err := ExtractEncryptedBackupArchive(archivePath, password, extractedPath)
	if err != nil {
		t.Fatalf("extract archive: %v", err)
	}
	if manifest.Encryption != "AES-256" || manifest.BackupMode != BackupModeFull {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	got, err := os.ReadFile(extractedPath)
	if err != nil {
		t.Fatalf("read extracted database: %v", err)
	}
	if string(got) != string(source) {
		t.Fatalf("extracted database differs from source")
	}
}

func TestEncryptedBackupArchiveRejectsWrongPassword(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.db")
	if err := os.WriteFile(sourcePath, []byte("SQLite format 3\x00payload"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	archivePath := filepath.Join(t.TempDir(), "backup.zip")
	if err := CreateEncryptedBackupArchive(sourcePath, archivePath, BackupModeSettings, "correct-password"); err != nil {
		t.Fatalf("create archive: %v", err)
	}

	_, err := ExtractEncryptedBackupArchive(archivePath, "wrong-password", filepath.Join(t.TempDir(), "out.db"))
	if !errors.Is(err, ErrBackupArchivePassword) {
		t.Fatalf("wrong password error = %v, want ErrBackupArchivePassword", err)
	}
}

func TestEncryptedBackupArchiveRejectsMissingPassword(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.db")
	if err := os.WriteFile(sourcePath, []byte("SQLite format 3\x00payload"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	archivePath := filepath.Join(t.TempDir(), "backup.zip")
	if err := CreateEncryptedBackupArchive(sourcePath, archivePath, BackupModeFull, "password"); err != nil {
		t.Fatalf("create archive: %v", err)
	}

	_, err := ExtractEncryptedBackupArchive(archivePath, "", filepath.Join(t.TempDir(), "out.db"))
	if err == nil {
		t.Fatal("expected missing password error")
	}
}
