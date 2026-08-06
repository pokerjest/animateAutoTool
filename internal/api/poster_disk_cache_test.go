package api

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPosterDiskCachePersistsValidatedImages(t *testing.T) {
	originalDir := posterDiskCacheDir
	cacheDir := t.TempDir()
	posterDiskCacheDir = func() string { return cacheDir }
	t.Cleanup(func() { posterDiskCacheDir = originalDir })

	data := testPosterPNG(t, 12, 18)
	savePosterDiskCache("https://lain.bgm.tv/pic/cover/l/12.jpg", data, maxCalendarPosterBytes)

	got, ok := loadPosterDiskCache("https://lain.bgm.tv/pic/cover/l/12.jpg", maxCalendarPosterBytes)
	if !ok {
		t.Fatal("expected persisted poster cache hit")
	}
	if !bytes.Equal(got, data) {
		t.Fatal("persisted poster cache returned different bytes")
	}

	files, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("read cache directory: %v", err)
	}
	if len(files) != 1 || filepath.Ext(files[0].Name()) != ".img" {
		t.Fatalf("cache files = %v, want one .img file", files)
	}
}

func TestPosterDiskCacheExpiresStaleImages(t *testing.T) {
	originalDir := posterDiskCacheDir
	cacheDir := t.TempDir()
	posterDiskCacheDir = func() string { return cacheDir }
	t.Cleanup(func() { posterDiskCacheDir = originalDir })

	key := "https://lain.bgm.tv/pic/cover/l/stale.jpg"
	savePosterDiskCache(key, testPosterPNG(t, 12, 18), maxCalendarPosterBytes)
	path := posterDiskCachePath(key)
	staleTime := time.Now().Add(-posterDiskCacheMaxAge - time.Hour)
	if err := os.Chtimes(path, staleTime, staleTime); err != nil {
		t.Fatalf("age cache file: %v", err)
	}

	if _, ok := loadPosterDiskCache(key, maxCalendarPosterBytes); ok {
		t.Fatal("expected stale poster cache miss")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale cache file still exists, stat err=%v", err)
	}
}
