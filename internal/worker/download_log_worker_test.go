package worker

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

func TestPathWithinRoot(t *testing.T) {
	root := "/media/anime"

	cases := []struct {
		path string
		want bool
	}{
		{path: "/media/anime/Show/episode01.mkv", want: true},
		{path: "/media/anime", want: true},
		{path: "/media/other/Show/episode01.mkv", want: false},
		{path: "/media/anime/../other/Show/episode01.mkv", want: false},
		{path: "/media/anime/Show/Season 1/ep.mkv", want: true},
		{path: "/media/anime2/Show/ep.mkv", want: false},
	}

	for _, tc := range cases {
		if got := pathWithinRoot(tc.path, root); got != tc.want {
			t.Fatalf("pathWithinRoot(%q, %q) = %v, want %v", tc.path, root, got, tc.want)
		}
	}
}

func TestAutoScanCompletedDownloadsEarlyReturns(t *testing.T) {
	// Empty targets is a no-op even when DB is nil.
	prev := db.DB
	db.DB = nil
	defer func() { db.DB = prev }()

	autoScanCompletedDownloads(nil)
	autoScanCompletedDownloads([]string{})
	// Non-empty target with nil DB short-circuits before any query.
	autoScanCompletedDownloads([]string{"/tmp/does-not-matter"})
}

func TestAutoScanCompletedDownloadsNoDirectories(t *testing.T) {
	db.InitDB(":memory:")
	t.Cleanup(func() {
		_ = db.CloseDB()
		db.DB = nil
	})

	tmp := t.TempDir()
	// No LocalAnimeDirectory rows -> early return after Find.
	autoScanCompletedDownloads([]string{filepath.Join(tmp, "missing.mkv")})
}

func TestPublishCompletedDownloadEvents(t *testing.T) {
	db.InitDB(":memory:")
	t.Cleanup(func() {
		_ = db.CloseDB()
		db.DB = nil
	})

	tmp := t.TempDir()
	target := filepath.Join(tmp, "Show", "ep01.mkv")

	anime := model.LocalAnime{Title: "Show A", Path: filepath.Join(tmp, "Show")}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("create anime: %v", err)
	}
	episode := model.LocalEpisode{
		LocalAnimeID: anime.ID,
		Title:        "Episode 1",
		Path:         target,
	}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatalf("create episode: %v", err)
	}

	var (
		mu     sync.Mutex
		got    []map[string]interface{}
		notify = make(chan struct{}, 4)
	)
	event.GlobalBus.Subscribe(event.EventDownloadReady, func(e event.Event) {
		payload, ok := e.Payload.(map[string]interface{})
		if !ok {
			return
		}
		mu.Lock()
		got = append(got, payload)
		mu.Unlock()
		notify <- struct{}{}
	})

	// Duplicate target should still produce only one event for the same anime.
	publishCompletedDownloadEvents([]string{target, target, "  ", filepath.Join(tmp, "missing.mkv")})

	select {
	case <-notify:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for download_ready event")
	}

	// Drain any other events that might briefly arrive (none expected for missing path).
	timeout := time.After(100 * time.Millisecond)
drain:
	for {
		select {
		case <-notify:
		case <-timeout:
			break drain
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 event, got %d (%+v)", len(got), got)
	}
	if got[0]["title"] != "Show A" {
		t.Fatalf("expected title 'Show A', got %v", got[0]["title"])
	}
	if got[0]["target_file"] != target {
		t.Fatalf("expected target_file %q, got %v", target, got[0]["target_file"])
	}
}
