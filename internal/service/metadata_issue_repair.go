package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"gorm.io/gorm"
)

type MetadataIssueRepairProgress struct {
	Phase   string
	Message string
	Current int64
	Total   int64
}

type MetadataIssueRepairProgressFunc func(MetadataIssueRepairProgress)

type MetadataIssueRepairResult struct {
	Scanned  int
	Repaired int
	Failed   int
}

func (s *AgentService) RepairDatabaseMetadataIssues(
	ctx context.Context,
	report MetadataIssueRepairProgressFunc,
) (MetadataIssueRepairResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if db.DB == nil {
		return MetadataIssueRepairResult{}, gorm.ErrInvalidDB
	}

	issues, err := store.NewLibraryIssueStore(db.DB).ListOpenByType(LibraryIssueStatusOpen, LibraryIssueTypeScrape)
	if err != nil {
		return MetadataIssueRepairResult{}, err
	}
	recoverable := make([]model.LibraryIssue, 0, len(issues))
	for _, issue := range issues {
		if issue.LocalAnimeID == nil || *issue.LocalAnimeID == 0 || !isMetadataDatabaseContention(issue.Message) {
			continue
		}
		recoverable = append(recoverable, issue)
	}

	result := MetadataIssueRepairResult{Scanned: len(recoverable)}
	total := int64(len(recoverable))
	if report != nil && total > 0 {
		report(MetadataIssueRepairProgress{
			Phase:   "repair",
			Message: fmt.Sprintf("正在准备修复 %d 条历史数据库冲突", len(recoverable)),
			Total:   total,
		})
	}
	laStore := localAnimeStore()
	if laStore == nil {
		return result, gorm.ErrInvalidDB
	}
	for index := range recoverable {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		issue := &recoverable[index]
		current := int64(index + 1)
		if report != nil {
			report(MetadataIssueRepairProgress{
				Phase:   "repair",
				Message: fmt.Sprintf("正在修复历史数据库冲突 %d/%d：%s", index+1, len(recoverable), issue.Title),
				Current: current,
				Total:   total,
			})
		}

		anime, loadErr := laStore.GetWithMetadata(*issue.LocalAnimeID)
		if loadErr != nil {
			if errors.Is(loadErr, gorm.ErrRecordNotFound) {
				if resolveErr := ResolveLibraryIssue(issue.IssueKey); resolveErr != nil {
					return result, resolveErr
				}
				result.Repaired++
				continue
			}
			return result, loadErr
		}

		if enrichErr := s.enrich(anime); enrichErr != nil {
			result.Failed++
			if reportErr := ReportLibraryIssue(LibraryIssueInput{
				IssueKey:      issue.IssueKey,
				IssueType:     LibraryIssueTypeScrape,
				Title:         anime.Title,
				DirectoryPath: anime.Path,
				LocalAnimeID:  &anime.ID,
				Message:       enrichErr.Error(),
				Hint:          MetadataIssueHint(enrichErr),
			}); reportErr != nil {
				return result, reportErr
			}
			continue
		}
		if resolveErr := ResolveLibraryIssue(issue.IssueKey); resolveErr != nil {
			return result, resolveErr
		}
		result.Repaired++
	}

	if report != nil && total > 0 {
		report(MetadataIssueRepairProgress{
			Phase:   "repair",
			Message: fmt.Sprintf("历史数据库冲突修复完成：成功 %d，仍需处理 %d", result.Repaired, result.Failed),
			Current: total,
			Total:   total,
		})
	}
	return result, nil
}

func isMetadataDatabaseContention(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	return strings.Contains(message, "sqlite_busy") ||
		strings.Contains(message, "database is locked") ||
		strings.Contains(message, "database table is locked")
}
