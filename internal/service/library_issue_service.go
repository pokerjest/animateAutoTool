package service

import (
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
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

	now := time.Now()
	issue := model.LibraryIssue{}
	err := db.DB.Where("issue_key = ?", strings.TrimSpace(input.IssueKey)).First(&issue).Error
	switch err {
	case nil:
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
		if err := db.DB.Model(&model.LibraryIssue{}).Where("id = ?", issue.ID).Updates(updates).Error; err != nil {
			return err
		}
	case gorm.ErrRecordNotFound:
		issue = model.LibraryIssue{
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
		if err := db.DB.Create(&issue).Error; err != nil {
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

	var issue model.LibraryIssue
	err := db.DB.Where("issue_key = ? AND status = ?", strings.TrimSpace(issueKey), LibraryIssueStatusOpen).First(&issue).Error
	if err == gorm.ErrRecordNotFound {
		return nil
	}
	if err != nil {
		return err
	}

	now := time.Now()
	if err := db.DB.Model(&model.LibraryIssue{}).
		Where("id = ?", issue.ID).
		Updates(map[string]interface{}{
			"status":      LibraryIssueStatusResolved,
			"resolved_at": &now,
		}).Error; err != nil {
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

	var issues []model.LibraryIssue
	err := db.DB.Where("status = ?", LibraryIssueStatusOpen).
		Order("last_seen_at DESC").
		Limit(limit).
		Find(&issues).Error
	return issues, err
}
