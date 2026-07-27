package logging

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	healthSeverityPattern      = regexp.MustCompile(`(?i)(^|[\s\[:])(error|fatal|panic|warning|warn)([\s:\]]|$)`)
	healthHTTP5xxPattern       = regexp.MustCompile(`\|\s*5[0-9]{2}\s*\|`)
	healthZeroCountPattern     = regexp.MustCompile(`(?i)\b(errors?|failures?|failed)\s*[=:]\s*0\b`)
	healthQuerySecretPattern   = regexp.MustCompile(`(?i)([?&](?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|passwd|secret)=)[^&\s]+`)
	healthFieldSecretPattern   = regexp.MustCompile(`(?i)((?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|passwd|secret)["'\s]*[:=]["'\s]*)([^&\s"',}]+)`)
	healthAuthorizationPattern = regexp.MustCompile(`(?i)(authorization:\s*)(?:bearer\s+)?[^\s]+`)
	healthURLPasswordPattern   = regexp.MustCompile(`(?i)(https?://[^:/\s]+:)[^@\s]+@`)
)

// HealthWriter receives the normal application log stream but persists only
// lines that describe an abnormal condition. Files are opened lazily, so a
// healthy process creates no health-*.log file at all.
type HealthWriter struct {
	mu       sync.Mutex
	dir      string
	prefix   string
	maxFiles int
	now      func() time.Time
	pending  []byte
	closed   bool
}

func NewHealthWriter(dir, prefix string, maxFiles int) (*HealthWriter, error) {
	return newHealthWriter(dir, prefix, maxFiles, time.Now)
}

func newHealthWriter(dir, prefix string, maxFiles int, now func() time.Time) (*HealthWriter, error) {
	dir = strings.TrimSpace(dir)
	prefix = strings.TrimSpace(prefix)
	if dir == "" || prefix == "" {
		return nil, os.ErrInvalid
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(filepath.Clean(dir), 0o755); err != nil {
		return nil, err
	}
	return &HealthWriter{dir: filepath.Clean(dir), prefix: prefix, maxFiles: maxFiles, now: now}, nil
}

func (w *HealthWriter) Write(p []byte) (int, error) {
	if w == nil {
		return 0, os.ErrInvalid
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}

	w.pending = append(w.pending, p...)
	for {
		newline := bytes.IndexByte(w.pending, '\n')
		if newline < 0 {
			break
		}
		line := append([]byte(nil), w.pending[:newline+1]...)
		w.pending = w.pending[newline+1:]
		w.writeIssueLineLocked(line)
	}
	// This is a secondary diagnostics sink. A filesystem error must never make
	// the standard logger or an HTTP response fail.
	return len(p), nil
}

func (w *HealthWriter) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	if len(w.pending) > 0 {
		w.writeIssueLineLocked(w.pending)
		w.pending = nil
	}
	w.closed = true
	return nil
}

func (w *HealthWriter) writeIssueLineLocked(line []byte) {
	if !IsHealthIssueLine(string(line)) {
		return
	}
	now := w.now()
	path := filepath.Join(w.dir, w.prefix+"-"+now.Format(hourlyTimestampLayout)+".log")
	// Open per abnormal line so an exported file can be removed immediately on
	// Windows as well as Unix; the writer never keeps a health file locked.
	//nolint:gosec // path is derived from an app-controlled directory and prefix.
	file, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = file.WriteString(RedactHealthLogLine(string(line)))
	_ = file.Close()
	w.pruneLocked()
}

func (w *HealthWriter) pruneLocked() {
	if w.maxFiles <= 0 {
		return
	}
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	prefix := w.prefix + "-"
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".log") {
			continue
		}
		timestamp := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".log")
		if _, err := time.ParseInLocation(hourlyTimestampLayout, timestamp, time.Local); err == nil {
			names = append(names, name)
		}
	}
	if len(names) <= w.maxFiles {
		return
	}
	sort.Strings(names)
	for _, name := range names[:len(names)-w.maxFiles] {
		_ = os.Remove(filepath.Join(w.dir, name))
	}
}

// IsHealthIssueLine recognizes operational failures while avoiding summary
// counters such as "failed=0" and "errors=0".
func IsHealthIssueLine(line string) bool {
	normalized := strings.TrimSpace(healthZeroCountPattern.ReplaceAllString(line, ""))
	if normalized == "" {
		return false
	}
	lower := strings.ToLower(normalized)
	if healthSeverityPattern.MatchString(normalized) || healthHTTP5xxPattern.MatchString(normalized) {
		return true
	}
	markers := []string{
		" failed", " failure", "cannot ", "unable to ", "timed out", "timeout",
		"database is locked", "sqlite_busy", "not writable", "permission denied",
		"no space left", "out of memory", "connection reset", "broken pipe",
		"失败", "错误", "异常", "超时", "数据库锁", "无写权限",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// RedactHealthLogLine removes common credential forms before a health log can
// be exported to a developer.
func RedactHealthLogLine(line string) string {
	line = healthQuerySecretPattern.ReplaceAllString(line, `${1}[REDACTED]`)
	line = healthFieldSecretPattern.ReplaceAllString(line, `${1}[REDACTED]`)
	line = healthAuthorizationPattern.ReplaceAllString(line, `${1}[REDACTED]`)
	return healthURLPasswordPattern.ReplaceAllString(line, `${1}[REDACTED]@`)
}
