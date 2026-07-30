package service

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"gorm.io/gorm"
)

// LocalAnimeMatchesSubscription verifies that a local series is independently
// related to a subscription. A shared metadata_id is deliberately not enough:
// historical bad links must not make an unrelated local series appear
// playable or satisfy a missing download.
func LocalAnimeMatchesSubscription(sub *model.Subscription, anime *model.LocalAnime) bool {
	if sub == nil || anime == nil {
		return false
	}
	return localIdentityMatchesReferences(
		sub,
		[]string{strings.TrimSpace(sub.Title)},
		localAnimeIdentityTitles(anime.Title, anime.Path),
	)
}

func localEpisodeCandidateMatchesSubscription(
	sub model.Subscription,
	releaseTitle string,
	candidate store.EpisodePathRow,
) bool {
	references := releaseIdentityTitles(releaseTitle)
	if len(references) == 0 {
		references = []string{strings.TrimSpace(sub.Title)}
	}
	return localIdentityMatchesReferences(
		&sub,
		references,
		localAnimeIdentityTitles(candidate.AnimeTitle, candidate.AnimePath),
	)
}

func localIdentityMatchesReferences(sub *model.Subscription, references, localTitles []string) bool {
	references = nonEmptyTitles(references)
	localTitles = nonEmptyTitles(localTitles)
	if len(references) == 0 || len(localTitles) == 0 {
		return false
	}

	for _, reference := range references {
		for _, localTitle := range localTitles {
			if titlesStronglyRelated(reference, localTitle) {
				return true
			}
		}
	}

	metadata := subscriptionMetadataForMatch(sub)
	aliases := metadataIdentityTitles(metadata)
	if len(aliases) == 0 {
		return false
	}

	// Metadata can bridge localized titles only when both independent sides
	// match an alias in that metadata row. Merely sharing the row's numeric ID
	// never proves that the two series are the same.
	referenceLinked := false
	for _, reference := range references {
		if titleMatchesAny(reference, aliases) {
			referenceLinked = true
			break
		}
	}
	if !referenceLinked {
		return false
	}
	for _, localTitle := range localTitles {
		if titleMatchesAny(localTitle, aliases) {
			return true
		}
	}
	return false
}

func releaseIdentityTitles(releaseTitle string) []string {
	releaseTitle = strings.TrimSpace(releaseTitle)
	if releaseTitle == "" {
		return nil
	}
	return nonEmptyTitles([]string{releaseTitle, parser.CleanTitle(releaseTitle)})
}

func localAnimeIdentityTitles(title, path string) []string {
	values := []string{strings.TrimSpace(title)}
	if path = strings.TrimSpace(path); path != "" {
		values = append(values, filepath.Base(filepath.Clean(path)))
	}
	return nonEmptyTitles(values)
}

func metadataIdentityTitles(metadata *model.AnimeMetadata) []string {
	if metadata == nil {
		return nil
	}
	return nonEmptyTitles([]string{
		metadata.Title,
		metadata.TitleCN,
		metadata.TitleEN,
		metadata.TitleJP,
		metadata.OriginalTitle,
		metadata.BangumiTitle,
		metadata.TMDBTitle,
		metadata.AniListTitle,
	})
}

func nonEmptyTitles(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := compactRuleTitle(value)
		if key == "" {
			key = strings.ToLower(value)
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func titleMatchesAny(title string, candidates []string) bool {
	for _, candidate := range candidates {
		if titlesStronglyRelated(title, candidate) {
			return true
		}
	}
	return false
}

func subscriptionMetadataForMatch(sub *model.Subscription) *model.AnimeMetadata {
	if sub == nil {
		return nil
	}
	if sub.Metadata != nil {
		return sub.Metadata
	}
	if sub.MetadataID == nil || *sub.MetadataID == 0 {
		return nil
	}
	st := metadataStore()
	if st == nil {
		return nil
	}
	metadata, err := st.GetByID(*sub.MetadataID)
	if err != nil {
		return nil
	}
	return metadata
}

// invalidateMismatchedLocalDownloadLogs retracts compatibility records that
// were marked complete solely because an unrelated LocalAnime happened to
// share metadata_id and episode number. It never removes or renames media.
func invalidateMismatchedLocalDownloadLogs(sub model.Subscription) (int, error) {
	if sub.ID == 0 || db.DB == nil {
		return 0, nil
	}
	logStore := downloadLogStore()
	localStore := localAnimeStore()
	if logStore == nil || localStore == nil {
		return 0, nil
	}

	logs, err := logStore.ListBySubscriptionAndStatuses(
		sub.ID,
		[]string{downloadLogStatusCompleted, downloadLogStatusRenamed},
	)
	if err != nil {
		return 0, err
	}

	invalidated := 0
	for _, entry := range logs {
		target := strings.TrimSpace(entry.TargetFile)
		if target == "" {
			continue
		}
		candidate, err := localStore.EpisodePathByPath(target)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			cleanTarget := filepath.Clean(target)
			if cleanTarget != target {
				candidate, err = localStore.EpisodePathByPath(cleanTarget)
			}
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return invalidated, err
		}
		if recordedLocalTargetMatchesSubscription(entry, sub, *candidate) {
			continue
		}

		fingerprint := resourceFingerprintForLog(entry)
		if err := db.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&model.DownloadLog{}).Where("id = ?", entry.ID).Updates(map[string]any{
				"status":      downloadLogStatusArchived,
				"target_file": "",
				"info_hash":   "",
			}).Error; err != nil {
				return err
			}

			resourceUpdates := map[string]any{
				"state":        SubscriptionResourceStateSeen,
				"state_reason": "已撤销跨番剧的本地文件错误映射，等待重新下载",
				"last_error":   "",
				"task_hash":    "",
				"target_file":  "",
				"selected":     true,
				"submitted_at": nil,
				"completed_at": nil,
				"retry_after":  nil,
			}
			if entry.ResourceID != nil && *entry.ResourceID != 0 {
				return tx.Model(&model.SubscriptionResource{}).
					Where("id = ? AND subscription_id = ?", *entry.ResourceID, sub.ID).
					Updates(resourceUpdates).Error
			}
			if fingerprint != "" {
				return tx.Model(&model.SubscriptionResource{}).
					Where("subscription_id = ? AND fingerprint = ?", sub.ID, fingerprint).
					Updates(resourceUpdates).Error
			}
			return nil
		}); err != nil {
			return invalidated, err
		}

		invalidated++
		log.Printf(
			"WARN: subscription %q download log %d pointed to unrelated local series %q (%s); archived false completion so episode %s can be downloaded again",
			sub.Title,
			entry.ID,
			candidate.AnimeTitle,
			candidate.AnimePath,
			entry.Episode,
		)
		localAnimeID := candidate.LocalAnimeID
		_ = ReportLibraryIssue(LibraryIssueInput{
			IssueKey:      fmt.Sprintf("subscription-local-mismatch:%d:%d", sub.ID, candidate.LocalAnimeID),
			IssueType:     LibraryIssueTypeParse,
			Title:         sub.Title,
			DirectoryPath: candidate.AnimePath,
			LocalAnimeID:  &localAnimeID,
			Message:       fmt.Sprintf("订阅曾错误映射到另一部本地番剧：%s", candidate.AnimeTitle),
			Hint:          "系统已撤销错误完成记录并允许重新补下载；请检查这两部番剧的元数据匹配是否正确。",
		})
	}
	return invalidated, nil
}

func recordedLocalTargetMatchesSubscription(
	entry model.DownloadLog,
	sub model.Subscription,
	candidate store.EpisodePathRow,
) bool {
	if episode, err := strconv.Atoi(strings.TrimSpace(entry.Episode)); err == nil && episode > 0 {
		if candidate.EpisodeNum != episode {
			return false
		}
	}
	return localEpisodeCandidateMatchesSubscription(sub, entry.Title, candidate)
}
