package service

import (
	"errors"
	"log"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"gorm.io/gorm"
)

const (
	LibraryIssueTypeScan   = "scan"
	LibraryIssueTypeScrape = "scrape"

	LibraryIssueStatusOpen     = "open"
	LibraryIssueStatusResolved = "resolved"
)

type LibraryIssueInput struct {
	IssueKey      string
	IssueType     string
	Title         string
	DirectoryPath string
	LocalAnimeID  *uint
	Message       string
	Hint          string
}

func ReportLibraryIssue(input LibraryIssueInput) error {
	if db.DB == nil || strings.TrimSpace(input.IssueKey) == "" {
		return nil
	}
	log.Printf("WARN: health issue reported type=%s key=%s title=%q path=%q message=%s hint=%s",
		strings.TrimSpace(input.IssueType), strings.TrimSpace(input.IssueKey), strings.TrimSpace(input.Title),
		strings.TrimSpace(input.DirectoryPath), strings.TrimSpace(input.Message), strings.TrimSpace(input.Hint))

	now := time.Now()
	issueStore := store.NewLibraryIssueStore(db.DB)
	issue, err := issueStore.GetByKey(strings.TrimSpace(input.IssueKey))
	switch {
	case err == nil:
		updates := map[string]interface{}{
			"issue_type":       strings.TrimSpace(input.IssueType),
			"status":           LibraryIssueStatusOpen,
			"title":            strings.TrimSpace(input.Title),
			"directory_path":   strings.TrimSpace(input.DirectoryPath),
			"local_anime_id":   input.LocalAnimeID,
			"message":          strings.TrimSpace(input.Message),
			"hint":             strings.TrimSpace(input.Hint),
			"occurrence_count": issue.OccurrenceCount + 1,
			"last_seen_at":     now,
			"resolved_at":      nil,
		}
		if err := issueStore.UpdateByID(issue.ID, updates); err != nil {
			return err
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		newIssue := model.LibraryIssue{
			IssueKey:        strings.TrimSpace(input.IssueKey),
			IssueType:       strings.TrimSpace(input.IssueType),
			Status:          LibraryIssueStatusOpen,
			Title:           strings.TrimSpace(input.Title),
			DirectoryPath:   strings.TrimSpace(input.DirectoryPath),
			LocalAnimeID:    input.LocalAnimeID,
			Message:         strings.TrimSpace(input.Message),
			Hint:            strings.TrimSpace(input.Hint),
			OccurrenceCount: 1,
			LastSeenAt:      &now,
		}
		if err := issueStore.Create(&newIssue); err != nil {
			return err
		}
	default:
		return err
	}

	event.GlobalBus.Publish(event.EventLibraryIssue, map[string]interface{}{
		"type":          strings.TrimSpace(input.IssueType),
		"status":        LibraryIssueStatusOpen,
		"title":         strings.TrimSpace(input.Title),
		"message":       strings.TrimSpace(input.Message),
		"hint":          strings.TrimSpace(input.Hint),
		"directoryPath": strings.TrimSpace(input.DirectoryPath),
		"localAnimeId":  input.LocalAnimeID,
	})
	return nil
}

func ResolveLibraryIssue(issueKey string) error {
	if db.DB == nil || strings.TrimSpace(issueKey) == "" {
		return nil
	}

	issueStore := store.NewLibraryIssueStore(db.DB)
	issue, err := issueStore.GetOpenByKey(strings.TrimSpace(issueKey), LibraryIssueStatusOpen)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	now := time.Now()
	if err := issueStore.UpdateByID(issue.ID, map[string]interface{}{
		"status":      LibraryIssueStatusResolved,
		"resolved_at": &now,
	}); err != nil {
		return err
	}

	event.GlobalBus.Publish(event.EventLibraryIssue, map[string]interface{}{
		"type":          issue.IssueType,
		"status":        LibraryIssueStatusResolved,
		"title":         issue.Title,
		"message":       issue.Message,
		"hint":          issue.Hint,
		"directoryPath": issue.DirectoryPath,
		"localAnimeId":  issue.LocalAnimeID,
	})
	return nil
}

func ListOpenLibraryIssues(limit int) ([]model.LibraryIssue, error) {
	if db.DB == nil {
		return nil, nil
	}

	if limit <= 0 {
		limit = 20
	}

	issues, err := store.NewLibraryIssueStore(db.DB).ListOpen(LibraryIssueStatusOpen, limit)
	if err != nil {
		return nil, err
	}
	for i := range issues {
		if issues[i].IssueType != LibraryIssueTypeScrape || strings.TrimSpace(issues[i].Message) == "" {
			continue
		}
		hint := MetadataIssueHint(errors.New(issues[i].Message))
		if hint == "" || hint == issues[i].Hint {
			continue
		}
		issues[i].Hint = hint
	}
	return issues, err
}
