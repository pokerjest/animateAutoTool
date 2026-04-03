package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

func TestScanAllResolvesRootIssueAfterDirectoryRecovers(t *testing.T) {
	withServiceTestDB(t)

	originalBus := event.GlobalBus
	bus := event.NewInMemoryBus()
	event.GlobalBus = bus
	t.Cleanup(func() {
		event.GlobalBus = originalBus
	})

	GlobalScanStatus.Skip("")

	root := t.TempDir()
	goodRoot := filepath.Join(root, "library")
	if err := os.MkdirAll(filepath.Join(goodRoot, "Show A"), 0o755); err != nil {
		t.Fatalf("failed to create good root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(goodRoot, "Show A", "[Group] Show A - 01.mkv"), []byte("video"), 0o600); err != nil {
		t.Fatalf("failed to create sample video: %v", err)
	}

	missingRoot := filepath.Join(root, "missing-library")
	dirs := []model.LocalAnimeDirectory{
		{Path: missingRoot},
		{Path: goodRoot},
	}
	for i := range dirs {
		if err := db.DB.Create(&dirs[i]).Error; err != nil {
			t.Fatalf("failed to create directory record %s: %v", dirs[i].Path, err)
		}
	}

	issueEvents := make(chan map[string]interface{}, 8)
	subID := event.GlobalBus.Subscribe(event.EventLibraryIssue, func(evt event.Event) {
		if payload, ok := evt.Payload.(map[string]interface{}); ok {
			select {
			case issueEvents <- payload:
			default:
			}
		}
	})
	t.Cleanup(func() {
		event.GlobalBus.Unsubscribe(event.EventLibraryIssue, subID)
		close(issueEvents)
	})

	scanner := NewScannerService()
	if err := scanner.ScanAll(); err != nil {
		t.Fatalf("scan all failed: %v", err)
	}

	var animeCount int64
	if err := db.DB.Model(&model.LocalAnime{}).Count(&animeCount).Error; err != nil {
		t.Fatalf("failed to count anime: %v", err)
	}
	if animeCount != 1 {
		t.Fatalf("expected 1 scanned anime, got %d", animeCount)
	}

	issues, err := ListOpenLibraryIssues(10)
	if err != nil {
		t.Fatalf("list open issues failed: %v", err)
	}
	if len(issues) != 1 || issues[0].IssueKey != "scan:"+filepath.Clean(missingRoot) {
		t.Fatalf("expected missing root scan issue, got %#v", issues)
	}

	status := GlobalScanStatus.Snapshot()
	if status.TotalDirectories != 2 || status.ProcessedDirectories != 2 || status.FailedDirectories != 1 {
		t.Fatalf("unexpected scan status after first run: %+v", status)
	}

	if err := os.MkdirAll(missingRoot, 0o755); err != nil {
		t.Fatalf("failed to recover missing root: %v", err)
	}

	if err := scanner.ScanAll(); err != nil {
		t.Fatalf("second scan all failed: %v", err)
	}

	issues, err = ListOpenLibraryIssues(10)
	if err != nil {
		t.Fatalf("list open issues after recovery failed: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected recovered scan issue to disappear, got %#v", issues)
	}

	var sawResolved bool
	timeout := time.After(2 * time.Second)
	for !sawResolved {
		select {
		case payload := <-issueEvents:
			if payload["status"] == LibraryIssueStatusResolved && payload["directoryPath"] == missingRoot {
				sawResolved = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for resolved scan issue event")
		}
	}
}
