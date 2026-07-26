package logging

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCreateRecentArchiveIncludesNewestThreeHourlyLogs(t *testing.T) {
	dir := t.TempDir()
	logs := map[string]string{
		"server-20260726-10.log": "ten\n",
		"server-20260726-11.log": "eleven\n",
		"server-20260726-12.log": "twelve\n",
		"server-20260726-13.log": "thirteen\n",
		"server-current.log":     "invalid\n",
		"other-20260726-14.log":  "other\n",
	}
	for name, content := range logs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "server-20260726-14.log"), 0o700); err != nil {
		t.Fatalf("create misleading directory: %v", err)
	}

	now := time.Date(2026, time.July, 26, 13, 30, 45, 0, time.Local)
	path, filename, included, err := CreateRecentArchive(dir, "server", 3, now)
	if err != nil {
		t.Fatalf("CreateRecentArchive: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	if filename != "animate-auto-tool-logs-20260726-133045.zip" {
		t.Fatalf("filename = %q", filename)
	}
	wantNames := []string{"server-20260726-13.log", "server-20260726-12.log", "server-20260726-11.log"}
	if len(included) != len(wantNames) {
		t.Fatalf("included = %#v", included)
	}
	for index, want := range wantNames {
		if included[index] != want {
			t.Fatalf("included[%d] = %q, want %q", index, included[index], want)
		}
	}

	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer func() { _ = reader.Close() }()
	if len(reader.File) != 3 {
		t.Fatalf("archive contains %d files", len(reader.File))
	}
	for index, file := range reader.File {
		if file.Name != wantNames[index] {
			t.Fatalf("archive file %d = %q, want %q", index, file.Name, wantNames[index])
		}
		entry, openErr := file.Open()
		if openErr != nil {
			t.Fatalf("open %s: %v", file.Name, openErr)
		}
		content, readErr := io.ReadAll(entry)
		_ = entry.Close()
		if readErr != nil {
			t.Fatalf("read %s: %v", file.Name, readErr)
		}
		if string(content) != logs[file.Name] {
			t.Fatalf("%s content = %q", file.Name, content)
		}
	}
}

func TestCreateRecentArchiveReturnsNoLogs(t *testing.T) {
	_, _, _, err := CreateRecentArchive(t.TempDir(), "server", 3, time.Now())
	if !errors.Is(err, ErrNoHourlyLogs) {
		t.Fatalf("error = %v, want ErrNoHourlyLogs", err)
	}
}
