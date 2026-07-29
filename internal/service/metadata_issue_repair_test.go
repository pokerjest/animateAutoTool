package service

import (
	"context"
	"errors"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

func TestRepairDatabaseMetadataIssuesRetriesOnlySQLiteContention(t *testing.T) {
	withServiceTestDB(t)

	directory := model.LocalAnimeDirectory{Path: t.TempDir()}
	if err := db.DB.Create(&directory).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	anime := model.LocalAnime{DirectoryID: directory.ID, Title: "Locked Show", Path: directory.Path}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("create anime: %v", err)
	}
	lockedID := anime.ID
	if err := db.DB.Create(&model.LibraryIssue{
		IssueKey:        "scrape:locked",
		IssueType:       LibraryIssueTypeScrape,
		Status:          LibraryIssueStatusOpen,
		Title:           anime.Title,
		LocalAnimeID:    &lockedID,
		Message:         "save enriched anime: database is locked (5) (SQLITE_BUSY)",
		OccurrenceCount: 1,
	}).Error; err != nil {
		t.Fatalf("create locked issue: %v", err)
	}
	if err := db.DB.Create(&model.LibraryIssue{
		IssueKey:        "scrape:network",
		IssueType:       LibraryIssueTypeScrape,
		Status:          LibraryIssueStatusOpen,
		Title:           "Network Show",
		LocalAnimeID:    &lockedID,
		Message:         "request timeout",
		OccurrenceCount: 1,
	}).Error; err != nil {
		t.Fatalf("create network issue: %v", err)
	}

	calls := 0
	agent := &AgentService{
		enrichAnime: func(target *model.LocalAnime) error {
			calls++
			if target.ID != anime.ID {
				return errors.New("unexpected anime")
			}
			return nil
		},
	}
	result, err := agent.RepairDatabaseMetadataIssues(context.Background(), nil)
	if err != nil {
		t.Fatalf("repair database metadata issues: %v", err)
	}
	if calls != 1 || result.Scanned != 1 || result.Repaired != 1 || result.Failed != 0 {
		t.Fatalf("unexpected repair result: calls=%d result=%+v", calls, result)
	}

	var locked, network model.LibraryIssue
	if err := db.DB.Where("issue_key = ?", "scrape:locked").First(&locked).Error; err != nil {
		t.Fatalf("reload locked issue: %v", err)
	}
	if err := db.DB.Where("issue_key = ?", "scrape:network").First(&network).Error; err != nil {
		t.Fatalf("reload network issue: %v", err)
	}
	if locked.Status != LibraryIssueStatusResolved {
		t.Fatalf("expected locked issue resolved, got %q", locked.Status)
	}
	if network.Status != LibraryIssueStatusOpen {
		t.Fatalf("expected non-database issue to remain open, got %q", network.Status)
	}
}
