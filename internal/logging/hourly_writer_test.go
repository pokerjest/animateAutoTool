package logging

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHourlyWriterRotatesAtHourBoundary(t *testing.T) {
	dir := t.TempDir()
	current := time.Date(2026, time.July, 26, 10, 59, 0, 0, time.Local)
	w, err := newHourlyWriter(dir, "server", 168, func() time.Time { return current })
	if err != nil {
		t.Fatalf("newHourlyWriter: %v", err)
	}

	if _, err := w.Write([]byte("first hour\n")); err != nil {
		t.Fatalf("write first hour: %v", err)
	}
	current = current.Add(time.Minute)
	if _, err := w.Write([]byte("second hour\n")); err != nil {
		t.Fatalf("write second hour: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	assertLogContent(t, filepath.Join(dir, "server-20260726-10.log"), "first hour\n")
	assertLogContent(t, filepath.Join(dir, "server-20260726-11.log"), "second hour\n")
}

func TestHourlyWriterAppendsWithinSameHour(t *testing.T) {
	dir := t.TempDir()
	current := time.Date(2026, time.July, 26, 11, 10, 0, 0, time.Local)
	now := func() time.Time { return current }

	first, err := newHourlyWriter(dir, "server", 168, now)
	if err != nil {
		t.Fatalf("create first writer: %v", err)
	}
	if _, err := first.Write([]byte("first process\n")); err != nil {
		t.Fatalf("write first process: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first writer: %v", err)
	}

	second, err := newHourlyWriter(dir, "server", 168, now)
	if err != nil {
		t.Fatalf("create second writer: %v", err)
	}
	if _, err := second.Write([]byte("second process\n")); err != nil {
		t.Fatalf("write second process: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second writer: %v", err)
	}

	assertLogContent(t, filepath.Join(dir, "server-20260726-11.log"), "first process\nsecond process\n")
}

func TestHourlyWriterPrunesOldestFiles(t *testing.T) {
	dir := t.TempDir()
	current := time.Date(2026, time.July, 26, 10, 0, 0, 0, time.Local)
	w, err := newHourlyWriter(dir, "server", 2, func() time.Time { return current })
	if err != nil {
		t.Fatalf("newHourlyWriter: %v", err)
	}

	for range 3 {
		if _, err := w.Write([]byte(current.Format(time.RFC3339) + "\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
		current = current.Add(time.Hour)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read log directory: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 retained files, got %d", len(entries))
	}
	if entries[0].Name() != "server-20260726-11.log" || entries[1].Name() != "server-20260726-12.log" {
		t.Fatalf("unexpected retained files: %s, %s", entries[0].Name(), entries[1].Name())
	}
}

func TestHourlyWriterRejectsMissingPathParts(t *testing.T) {
	if _, err := newHourlyWriter("", "server", 1, time.Now); err == nil {
		t.Fatal("expected empty directory to fail")
	}
	if _, err := newHourlyWriter(t.TempDir(), "", 1, time.Now); err == nil {
		t.Fatal("expected empty prefix to fail")
	}
}

func TestHourlyWriterDoesNotReopenAfterClose(t *testing.T) {
	w, err := newHourlyWriter(t.TempDir(), "server", 1, time.Now)
	if err != nil {
		t.Fatalf("newHourlyWriter: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := w.Write([]byte("late write")); err != os.ErrClosed {
		t.Fatalf("write after close error = %v, want os.ErrClosed", err)
	}
}

func TestHourlyWriterSerializesConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	current := time.Date(2026, time.July, 26, 11, 0, 0, 0, time.Local)
	w, err := newHourlyWriter(dir, "server", 1, func() time.Time { return current })
	if err != nil {
		t.Fatalf("newHourlyWriter: %v", err)
	}

	var writes sync.WaitGroup
	for range 100 {
		writes.Add(1)
		go func() {
			defer writes.Done()
			if _, err := w.Write([]byte("line\n")); err != nil {
				t.Errorf("concurrent write: %v", err)
			}
		}()
	}
	writes.Wait()
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	path := filepath.Join(dir, "server-20260726-11.log")
	//nolint:gosec // test path is created under t.TempDir.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read concurrent log: %v", err)
	}
	if got := strings.Count(string(data), "line\n"); got != 100 {
		t.Fatalf("concurrent line count = %d, want 100", got)
	}
}

func assertLogContent(t *testing.T, path, want string) {
	t.Helper()
	//nolint:gosec // test paths are created under t.TempDir.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if got := string(data); got != want {
		t.Fatalf("%s content = %q, want %q", path, got, want)
	}
	if strings.TrimSpace(string(data)) == "" {
		t.Fatalf("%s unexpectedly empty", path)
	}
}
