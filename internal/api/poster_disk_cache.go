package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/config"
)

const (
	posterDiskCacheMaxAge        = 30 * 24 * time.Hour
	posterDiskCacheMaxEntries    = 384
	posterDiskCacheMaxBytes      = 128 << 20
	posterDiskCacheFileMode      = 0o600
	posterDiskCacheDirectoryMode = 0o700
)

var (
	posterDiskCacheMu  sync.Mutex
	posterDiskCacheDir = func() string {
		return config.DataPath("cache", "posters", "remote")
	}
)

func loadPosterDiskCache(key string, maxBytes int) ([]byte, bool) {
	path := posterDiskCachePath(key)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || time.Since(info.ModTime()) > posterDiskCacheMaxAge {
		if err == nil && time.Since(info.ModTime()) > posterDiskCacheMaxAge {
			_ = os.Remove(path)
		}
		return nil, false
	}

	data, err := os.ReadFile(path) //nolint:gosec // path is a SHA-256 filename inside the app-controlled poster cache directory.
	if err != nil || len(data) == 0 || len(data) > maxBytes {
		return nil, false
	}
	if !isPosterImageData(data) {
		return nil, false
	}
	return data, true
}

func savePosterDiskCache(key string, data []byte, maxBytes int) {
	if len(data) == 0 || len(data) > maxBytes || !isPosterImageData(data) {
		return
	}

	posterDiskCacheMu.Lock()
	defer posterDiskCacheMu.Unlock()

	dir := posterDiskCacheDir()
	if err := os.MkdirAll(dir, posterDiskCacheDirectoryMode); err != nil {
		return
	}

	temp, err := os.CreateTemp(dir, ".poster-*.tmp")
	if err != nil {
		return
	}
	tempPath := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}
	if err := temp.Chmod(posterDiskCacheFileMode); err != nil {
		cleanup()
		return
	}
	if _, err := temp.Write(data); err != nil {
		cleanup()
		return
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return
	}
	if err := os.Rename(tempPath, posterDiskCachePath(key)); err != nil {
		_ = os.Remove(tempPath)
		return
	}

	prunePosterDiskCache(dir)
}

func posterDiskCachePath(key string) string {
	digest := sha256.Sum256([]byte(key))
	return filepath.Join(posterDiskCacheDir(), hex.EncodeToString(digest[:])+".img")
}

func isPosterImageData(data []byte) bool {
	return len(data) > 0 && len(data) <= 12<<20 && strings.HasPrefix(http.DetectContentType(data), "image/")
}

func prunePosterDiskCache(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	type cacheFile struct {
		path    string
		modTime time.Time
		size    int64
	}
	files := make([]cacheFile, 0, len(entries))
	var totalBytes int64
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".img" {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, cacheFile{
			path:    filepath.Join(dir, entry.Name()),
			modTime: info.ModTime(),
			size:    info.Size(),
		})
		totalBytes += info.Size()
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})
	for len(files) > posterDiskCacheMaxEntries || totalBytes > posterDiskCacheMaxBytes {
		oldest := files[0]
		files = files[1:]
		totalBytes -= oldest.size
		_ = os.Remove(oldest.path)
	}
}
