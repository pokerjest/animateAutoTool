package worker

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/service"
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

func TestAutoScanCompletedDownloadsReturnsOnlyAffectedAnime(t *testing.T) {
	db.InitDB(":memory:")
	t.Cleanup(func() {
		_ = db.CloseDB()
		db.DB = nil
	})

	tmp := t.TempDir()
	root := filepath.Join(tmp, "library")
	showA := filepath.Join(root, "Show A", "Season 01")
	showB := filepath.Join(root, "Show B", "Season 01")
	writeWorkerFixture(t, filepath.Join(showA, "Show A - 01.mkv"))
	writeWorkerFixture(t, filepath.Join(showB, "Show B - 01.mkv"))
	directory := model.LocalAnimeDirectory{Path: root}
	if err := db.DB.Create(&directory).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	scanner := service.NewScannerService()
	if _, err := scanner.ScanDirectory(&directory); err != nil {
		t.Fatalf("initial scan: %v", err)
	}

	target := filepath.Join(showA, "Show A - 02.mkv")
	writeWorkerFixture(t, target)
	affected := autoScanCompletedDownloads([]string{target})
	if len(affected) != 1 {
		t.Fatalf("expected one affected anime, got %v", affected)
	}
	var animes []model.LocalAnime
	if err := db.DB.Preload("Episodes").Order("title").Find(&animes).Error; err != nil {
		t.Fatalf("load animes: %v", err)
	}
	if len(animes) != 2 || len(animes[0].Episodes) != 2 || len(animes[1].Episodes) != 1 {
		t.Fatalf("unexpected targeted scan result: %+v", animes)
	}
}

func TestCompletedDownloadRescanCoordinatorMergesBatches(t *testing.T) {
	var (
		mu       sync.Mutex
		runCount int
		got      []string
		gotIDs   []uint
		done     = make(chan struct{}, 1)
	)
	coordinator := newCompletedDownloadRescanCoordinator(20*time.Millisecond, func(_ context.Context, targets []string, ids []uint) {
		mu.Lock()
		runCount++
		got = append(got, targets...)
		gotIDs = append(gotIDs, ids...)
		mu.Unlock()
		done <- struct{}{}
	})
	coordinator.schedule(context.Background(), []string{"a", "a"}, []uint{1})
	coordinator.schedule(context.Background(), []string{"b"}, []uint{2})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for merged coordinator run")
	}
	mu.Lock()
	defer mu.Unlock()
	if runCount != 1 || len(got) != 2 || len(gotIDs) != 2 {
		t.Fatalf("expected one merged run, count=%d targets=%v ids=%v", runCount, got, gotIDs)
	}
}

func TestCompletedDownloadJellyfinBatchWaitsForEveryPendingSeries(t *testing.T) {
	if completedDownloadJellyfinBatchSettled(service.JellyfinLibrarySyncResult{
		PendingSeries: 2,
		MatchedSeries: 1,
	}) {
		t.Fatal("partial Jellyfin match must keep polling for the other completed series")
	}
	if !completedDownloadJellyfinBatchSettled(service.JellyfinLibrarySyncResult{
		PendingSeries: 1,
		MatchedSeries: 1,
	}) {
		t.Fatal("batch should settle after every pending series is matched")
	}
	if !completedDownloadJellyfinBatchSettled(service.JellyfinLibrarySyncResult{}) {
		t.Fatal("batch with no pending series should settle immediately")
	}
}

func TestDownloadLogWorkerCycleRecoversPanicAndLogsStack(t *testing.T) {
	var output bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&output)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	})

	runDownloadLogSyncCycleWith(context.Background(), "fault-injection", func(context.Context) {
		panic("worker-boom")
	})

	logged := output.String()
	for _, expected := range []string{
		"DownloadLogWorker: cycle panic",
		"trigger=fault-injection",
		"worker-boom",
		"download_log_worker_test.go",
		"recovery_action=continue_next_cycle",
	} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("worker panic log missing %q: %s", expected, logged)
		}
	}
}

func writeWorkerFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	if err := os.WriteFile(path, []byte("video"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
