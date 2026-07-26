package logging

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrNoHourlyLogs = errors.New("no hourly log files")

type hourlyLogFile struct {
	name string
	path string
	hour time.Time
}

// CreateRecentArchive packages the newest valid hourly log files into a
// temporary ZIP. The caller owns the returned path and must remove it.
func CreateRecentArchive(dir, prefix string, limit int, now time.Time) (path, filename string, included []string, err error) {
	files, err := recentHourlyFiles(dir, prefix, limit)
	if err != nil {
		return "", "", nil, err
	}

	temp, err := os.CreateTemp("", "animate-auto-tool-logs-*.zip")
	if err != nil {
		return "", "", nil, err
	}
	tempPath := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}

	archive := zip.NewWriter(temp)
	for _, file := range files {
		if err := addLogToArchive(archive, file); err != nil {
			_ = archive.Close()
			cleanup()
			return "", "", nil, err
		}
		included = append(included, file.name)
	}
	if err := archive.Close(); err != nil {
		cleanup()
		return "", "", nil, err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", "", nil, err
	}

	return tempPath, fmt.Sprintf("animate-auto-tool-logs-%s.zip", now.Format("20060102-150405")), included, nil
}

func recentHourlyFiles(dir, prefix string, limit int) ([]hourlyLogFile, error) {
	dir = strings.TrimSpace(dir)
	prefix = strings.TrimSpace(prefix)
	if dir == "" || prefix == "" || limit <= 0 {
		return nil, errors.New("log directory, prefix, and positive limit are required")
	}

	entries, err := os.ReadDir(filepath.Clean(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoHourlyLogs
		}
		return nil, err
	}

	namePrefix := prefix + "-"
	files := make([]hourlyLogFile, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasPrefix(name, namePrefix) || !strings.HasSuffix(name, ".log") {
			continue
		}
		hourText := strings.TrimSuffix(strings.TrimPrefix(name, namePrefix), ".log")
		hour, parseErr := time.ParseInLocation(hourlyTimestampLayout, hourText, time.Local)
		if parseErr != nil {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, hourlyLogFile{name: name, path: filepath.Join(filepath.Clean(dir), name), hour: hour})
	}
	if len(files) == 0 {
		return nil, ErrNoHourlyLogs
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].hour.Equal(files[j].hour) {
			return files[i].name > files[j].name
		}
		return files[i].hour.After(files[j].hour)
	})
	if len(files) > limit {
		files = files[:limit]
	}
	return files, nil
}

func addLogToArchive(archive *zip.Writer, file hourlyLogFile) error {
	//nolint:gosec // file.path is selected from validated app-owned hourly log entries.
	source, err := os.Open(filepath.Clean(file.path))
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()

	info, err := source.Stat()
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = file.name
	header.Method = zip.Deflate
	target, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(target, source)
	return err
}
