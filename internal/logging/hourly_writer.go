package logging

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const hourlyTimestampLayout = "20060102-15"

// HourlyWriter appends logs to one local-time file per hour. Rotation happens
// on the first write in a new hour, so the application does not need a timer.
type HourlyWriter struct {
	mu       sync.Mutex
	dir      string
	prefix   string
	maxFiles int
	now      func() time.Time
	hour     string
	file     *os.File
	closed   bool
}

// NewHourlyWriter creates an hourly writer and opens the file for the current
// hour. maxFiles limits retained hourly files; values <= 0 disable pruning.
func NewHourlyWriter(dir, prefix string, maxFiles int) (*HourlyWriter, error) {
	return newHourlyWriter(dir, prefix, maxFiles, time.Now)
}

func newHourlyWriter(dir, prefix string, maxFiles int, now func() time.Time) (*HourlyWriter, error) {
	dir = strings.TrimSpace(dir)
	prefix = strings.TrimSpace(prefix)
	if dir == "" || prefix == "" {
		return nil, errors.New("log directory and prefix are required")
	}
	dir = filepath.Clean(dir)
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	w := &HourlyWriter{
		dir:      dir,
		prefix:   prefix,
		maxFiles: maxFiles,
		now:      now,
	}
	if err := w.rotateLocked(now()); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *HourlyWriter) Write(p []byte) (int, error) {
	if w == nil {
		return 0, os.ErrInvalid
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, os.ErrClosed
	}
	if err := w.rotateLocked(w.now()); err != nil {
		return 0, err
	}
	return w.file.Write(p)
}

func (w *HourlyWriter) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		w.closed = true
		return nil
	}
	syncErr := w.file.Sync()
	err := w.file.Close()
	w.file = nil
	w.closed = true
	if syncErr != nil {
		return syncErr
	}
	return err
}

func (w *HourlyWriter) rotateLocked(now time.Time) error {
	hour := now.Format(hourlyTimestampLayout)
	if w.file != nil && w.hour == hour {
		return nil
	}

	path := filepath.Join(w.dir, w.prefix+"-"+hour+".log")
	//nolint:gosec // path is derived from an app-controlled log directory and fixed prefix.
	next, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	previous := w.file
	w.file = next
	w.hour = hour
	if previous != nil {
		_ = previous.Sync()
		_ = previous.Close()
	}
	_ = w.pruneLocked()
	return nil
}

func (w *HourlyWriter) pruneLocked() error {
	if w.maxFiles <= 0 {
		return nil
	}
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return err
	}

	prefix := w.prefix + "-"
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".log") {
			continue
		}
		timestamp := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".log")
		if _, err := time.ParseInLocation(hourlyTimestampLayout, timestamp, time.Local); err != nil {
			continue
		}
		names = append(names, name)
	}
	if len(names) <= w.maxFiles {
		return nil
	}

	sort.Strings(names)
	for _, name := range names[:len(names)-w.maxFiles] {
		if err := os.Remove(filepath.Join(w.dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
