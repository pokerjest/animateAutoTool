package service

import (
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

func TestReportAndResolveLibraryIssue(t *testing.T) {
	withServiceTestDB(t)

	originalBus := event.GlobalBus
	bus := event.NewInMemoryBus()
	event.GlobalBus = bus
	t.Cleanup(func() {
		event.GlobalBus = originalBus
	})

	events := make(chan map[string]interface{}, 4)
	subID := event.GlobalBus.Subscribe(event.EventLibraryIssue, func(evt event.Event) {
		if payload, ok := evt.Payload.(map[string]interface{}); ok {
			events <- payload
		}
	})
	t.Cleanup(func() {
		event.GlobalBus.Unsubscribe(event.EventLibraryIssue, subID)
		close(events)
	})

	animeID := uint(7)
	input := LibraryIssueInput{
		IssueKey:      "scrape:7",
		IssueType:     LibraryIssueTypeScrape,
		Title:         "Broken Show",
		DirectoryPath: "/library/Broken Show",
		LocalAnimeID:  &animeID,
		Message:       "tmdb token missing",
		Hint:          "检查元数据配置",
	}

	if err := ReportLibraryIssue(input); err != nil {
		t.Fatalf("report issue failed: %v", err)
	}
	if err := ReportLibraryIssue(input); err != nil {
		t.Fatalf("report duplicate issue failed: %v", err)
	}

	issues, err := ListOpenLibraryIssues(10)
	if err != nil {
		t.Fatalf("list issues failed: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 open issue, got %d", len(issues))
	}
	if issues[0].OccurrenceCount != 2 {
		t.Fatalf("expected occurrence count 2, got %d", issues[0].OccurrenceCount)
	}

	if err := ResolveLibraryIssue("scrape:7"); err != nil {
		t.Fatalf("resolve issue failed: %v", err)
	}

	issues, err = ListOpenLibraryIssues(10)
	if err != nil {
		t.Fatalf("list issues after resolve failed: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no open issues after resolve, got %d", len(issues))
	}

	var count int64
	db.DB.Table("library_issues").Where("status = ?", LibraryIssueStatusResolved).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 resolved issue row, got %d", count)
	}

	var sawResolved bool
	timeout := time.After(2 * time.Second)
	for !sawResolved {
		select {
		case payload := <-events:
			if payload["status"] == LibraryIssueStatusResolved {
				sawResolved = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for resolved library issue event")
		}
	}
}

func TestListOpenLibraryIssuesRefreshesLegacyMetadataHint(t *testing.T) {
	withServiceTestDB(t)

	issue := model.LibraryIssue{
		IssueKey:  "scrape:88",
		IssueType: LibraryIssueTypeScrape,
		Status:    LibraryIssueStatusOpen,
		Title:     "Locked Show",
		Message:   "save enriched anime: database is locked (5) (SQLITE_BUSY)",
		Hint:      "检查元数据源配置，或在详情里使用修正匹配手动关联番剧。",
	}
	if err := db.DB.Create(&issue).Error; err != nil {
		t.Fatalf("create legacy issue: %v", err)
	}

	issues, err := ListOpenLibraryIssues(10)
	if err != nil {
		t.Fatalf("list issues: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected one issue, got %d", len(issues))
	}
	want := "数据库正在处理其他任务，系统会自动重试；若仍失败，请等待扫描或整理结束后再次刷新元数据。"
	if issues[0].Hint != want {
		t.Fatalf("expected refreshed hint %q, got %q", want, issues[0].Hint)
	}
}
