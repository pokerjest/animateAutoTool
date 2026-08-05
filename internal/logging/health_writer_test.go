package logging

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRedactingWriterProtectsPersistentLogSink(t *testing.T) {
	var output bytes.Buffer
	writer := RedactingWriter{Writer: &output}
	input := []byte("request token=secret-value url=https://user:password@example.test/path\n")
	written, err := writer.Write(input)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if written != len(input) {
		t.Fatalf("expected original byte count %d, got %d", len(input), written)
	}
	logged := output.String()
	if strings.Contains(logged, "secret-value") || strings.Contains(logged, "password@") {
		t.Fatalf("persistent sink leaked secret: %s", logged)
	}
	if !strings.Contains(logged, "[REDACTED]") {
		t.Fatalf("expected redaction marker: %s", logged)
	}
}

func TestHealthWriterCreatesFilesOnlyForIssues(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.Local)
	w, err := newHealthWriter(dir, "health", 24, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newHealthWriter: %v", err)
	}
	if _, err := w.Write([]byte("2026/07/27 scan completed with failed=0 errors=0\n")); err != nil {
		t.Fatalf("write healthy line: %v", err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("healthy log created files: entries=%v err=%v", entries, err)
	}

	_, _ = w.Write([]byte("2026/07/27 save enriched anime: database is locked (SQLITE_BUSY)\n"))
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	//nolint:gosec // the path is constructed inside t.TempDir for this test.
	content, err := os.ReadFile(filepath.Join(dir, "health-20260727-12.log"))
	if err != nil {
		t.Fatalf("read health log: %v", err)
	}
	if string(content) != "2026/07/27 save enriched anime: database is locked (SQLITE_BUSY)\n" {
		t.Fatalf("unexpected content %q", content)
	}
}

func TestHealthWriterRedactsSecretsAndRotates(t *testing.T) {
	dir := t.TempDir()
	current := time.Date(2026, 7, 27, 12, 0, 0, 0, time.Local)
	w, err := newHealthWriter(dir, "health", 24, func() time.Time { return current })
	if err != nil {
		t.Fatalf("newHealthWriter: %v", err)
	}
	_, _ = w.Write([]byte("ERROR request https://api.test/x?api_key=secret-value&x=1 password=hidden\n"))
	current = current.Add(time.Hour)
	_, _ = w.Write([]byte("WARN proxy http://user:private@example.test connection reset\n"))
	_ = w.Close()

	//nolint:gosec // the path is constructed inside t.TempDir for this test.
	first, err := os.ReadFile(filepath.Join(dir, "health-20260727-12.log"))
	if err != nil {
		t.Fatalf("read first health log: %v", err)
	}
	//nolint:gosec // the path is constructed inside t.TempDir for this test.
	second, err := os.ReadFile(filepath.Join(dir, "health-20260727-13.log"))
	if err != nil {
		t.Fatalf("read second health log: %v", err)
	}
	for _, value := range []string{string(first), string(second)} {
		if containsAny(value, "secret-value", "hidden", "private") {
			t.Fatalf("secret was not redacted: %s", value)
		}
	}
	if !containsAny(string(first), "api_key=[REDACTED]", "password=[REDACTED]") || !containsAny(string(second), "[REDACTED]@") {
		t.Fatalf("redaction markers missing: first=%q second=%q", first, second)
	}
}

func TestIsHealthIssueLineRecognizesTaskAndHTTPFailures(t *testing.T) {
	cases := []string{
		"Worker: metadata refresh failed: timeout",
		"[GIN] 2026/07/27 | 500 | 2ms | GET /api/v1/health",
		"媒体目录无写权限",
	}
	for _, line := range cases {
		if !IsHealthIssueLine(line) {
			t.Fatalf("expected issue line: %q", line)
		}
	}
	for _, line := range []string{"scan completed", "rename finished failed=0", "walk errors=0"} {
		if IsHealthIssueLine(line) {
			t.Fatalf("unexpected issue line: %q", line)
		}
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
