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

type SubscriptionLocalIdentityResult struct {
	ExternalMatch          bool
	Conflict               bool
	TitleMatch             bool
	Provider               string
	SubscriptionProviderID int
	LocalProviderID        int
}

type metadataProviderIdentity struct {
	provider string
	id       int
}

// EvaluateSubscriptionLocalIdentity compares namespaced provider IDs and
// explicit season evidence before title heuristics are allowed to participate.
func EvaluateSubscriptionLocalIdentity(sub *model.Subscription, anime *model.LocalAnime) SubscriptionLocalIdentityResult {
	if sub == nil || anime == nil {
		return SubscriptionLocalIdentityResult{}
	}

	subscriptionMetadata := subscriptionMetadataForMatch(sub)
	localMetadata := localAnimeMetadataForMatch(anime)
	titleMatch := localIdentityMatchesReferences(
		sub,
		[]string{strings.TrimSpace(sub.Title)},
		localAnimeIdentityTitles(anime.Title, anime.Path),
	)
	var matched metadataProviderIdentity
	var conflict *SubscriptionLocalIdentityResult
	if subscriptionMetadata != nil && localMetadata != nil {
		subscriptionProviders := metadataProviderIdentities(subscriptionMetadata)
		localProviders := metadataProviderIdentities(localMetadata)
		for i := range subscriptionProviders {
			if subscriptionProviders[i].id == 0 {
				continue
			}
			for j := range localProviders {
				if localProviders[j].id == 0 || subscriptionProviders[i].provider != localProviders[j].provider {
					continue
				}
				// Provider IDs are namespaced. A Bangumi ID must only be
				// compared with another Bangumi ID, never with TMDB/AniList.
				if subscriptionProviders[i].id == localProviders[j].id {
					if matched.id == 0 {
						matched = subscriptionProviders[i]
					}
					continue
				}
				if conflict == nil {
					conflict = &SubscriptionLocalIdentityResult{
						Conflict:               true,
						Provider:               subscriptionProviders[i].provider,
						SubscriptionProviderID: subscriptionProviders[i].id,
						LocalProviderID:        localProviders[j].id,
					}
				}
			}
		}
	}

	if subscriptionSeason, localSeason, conflict := subscriptionLocalSeasonConflict(sub, anime); conflict {
		return SubscriptionLocalIdentityResult{
			ExternalMatch:          matched.id != 0,
			Conflict:               true,
			TitleMatch:             titleMatch,
			Provider:               "season",
			SubscriptionProviderID: subscriptionSeason,
			LocalProviderID:        localSeason,
		}
	}
	if conflict != nil {
		conflict.ExternalMatch = matched.id != 0
		conflict.TitleMatch = titleMatch
		return *conflict
	}
	if matched.id != 0 {
		return SubscriptionLocalIdentityResult{
			ExternalMatch:          true,
			TitleMatch:             titleMatch,
			Provider:               matched.provider,
			SubscriptionProviderID: matched.id,
			LocalProviderID:        matched.id,
		}
	}
	return SubscriptionLocalIdentityResult{TitleMatch: titleMatch}
}

func SubscriptionExternalIdentityKeys(sub *model.Subscription) []string {
	if sub == nil {
		return nil
	}
	return metadataExternalIdentityKeys(subscriptionMetadataForMatch(sub))
}

func LocalAnimeExternalIdentityKeys(anime *model.LocalAnime) []string {
	if anime == nil {
		return nil
	}
	return metadataExternalIdentityKeys(localAnimeMetadataForMatch(anime))
}

func metadataExternalIdentityKeys(metadata *model.AnimeMetadata) []string {
	providers := metadataProviderIdentities(metadata)
	keys := make([]string, 0, len(providers))
	for _, provider := range providers {
		if provider.id > 0 {
			keys = append(keys, provider.provider+":"+strconv.Itoa(provider.id))
		}
	}
	return keys
}

func metadataProviderIdentities(metadata *model.AnimeMetadata) []metadataProviderIdentity {
	if metadata == nil {
		return nil
	}
	return []metadataProviderIdentity{
		{provider: "bangumi", id: metadata.BangumiID},
		{provider: "tmdb", id: metadata.TMDBID},
		{provider: "anilist", id: metadata.AniListID},
	}
}

func localAnimeMetadataForMatch(anime *model.LocalAnime) *model.AnimeMetadata {
	if anime == nil {
		return nil
	}
	if anime.Metadata != nil {
		return anime.Metadata
	}
	if anime.MetadataID == nil || *anime.MetadataID == 0 {
		return nil
	}
	st := metadataStore()
	if st == nil {
		return nil
	}
	metadata, err := st.GetByID(*anime.MetadataID)
	if err != nil {
		return nil
	}
	return metadata
}

func subscriptionLocalSeasonConflict(sub *model.Subscription, anime *model.LocalAnime) (int, int, bool) {
	subscriptionSeason, subscriptionExplicit := explicitSubscriptionSeason(sub)
	localSeason, localExplicit := explicitLocalAnimeSeason(anime)
	return subscriptionSeason, localSeason, subscriptionExplicit && localExplicit && subscriptionSeason != localSeason
}

func explicitSubscriptionSeason(sub *model.Subscription) (int, bool) {
	if sub == nil {
		return 0, false
	}
	if season, ok := explicitSeasonFromText(sub.Title); ok {
		return season, true
	}
	return explicitSeasonFromText(sub.Season)
}

func explicitLocalAnimeSeason(anime *model.LocalAnime) (int, bool) {
	if anime == nil {
		return 0, false
	}
	if season, ok := explicitSeasonFromText(anime.Title); ok {
		return season, true
	}
	segments := strings.FieldsFunc(strings.ReplaceAll(anime.Path, `\`, "/"), func(r rune) bool {
		return r == '/'
	})
	for i := len(segments) - 1; i >= 0; i-- {
		if season, ok := explicitSeasonFromText(segments[i]); ok {
			return season, true
		}
	}
	if anime.Season > 1 {
		return anime.Season, true
	}
	return 0, false
}

func explicitSeasonFromText(value string) (int, bool) {
	marker := seasonNoisePattern.FindString(strings.TrimSpace(value))
	if marker == "" {
		return 0, false
	}
	season := parser.ParseSeason(marker)
	if season <= 0 {
		return 0, false
	}
	return season, true
}

// LocalAnimeMatchesSubscription verifies that a local series is independently
// related to a subscription. A shared metadata_id is deliberately not enough:
// historical bad links must not make an unrelated local series appear
// playable or satisfy a missing download.
func LocalAnimeMatchesSubscription(sub *model.Subscription, anime *model.LocalAnime) bool {
	if sub == nil || anime == nil {
		return false
	}
	identity := EvaluateSubscriptionLocalIdentity(sub, anime)
	if identity.Conflict {
		return false
	}
	if identity.ExternalMatch {
		return true
	}
	return localIdentityMatchesReferences(
		sub,
		[]string{strings.TrimSpace(sub.Title)},
		localAnimeIdentityTitles(anime.Title, anime.Path),
	)
}

const subscriptionMatchIndexGramSize = 6

// SubscriptionLocalMatchIndexKeys returns conservative lookup keys used to
// narrow candidates before LocalAnimeMatchesSubscription performs final checks.
func SubscriptionLocalMatchIndexKeys(sub *model.Subscription) ([]string, bool) {
	if sub == nil {
		return nil, false
	}
	titles := []string{sub.Title}
	titles = append(titles, metadataIdentityTitles(subscriptionMetadataForMatch(sub))...)
	keys, requiresFallback := subscriptionMatchIndexKeys(titles)
	keys = append(keys, SubscriptionExternalIdentityKeys(sub)...)
	return keys, requiresFallback
}

func LocalAnimeSubscriptionIndexKeys(anime *model.LocalAnime) ([]string, bool) {
	if anime == nil {
		return nil, false
	}
	keys, requiresFallback := subscriptionMatchIndexKeys(localAnimeIdentityTitles(anime.Title, anime.Path))
	keys = append(keys, LocalAnimeExternalIdentityKeys(anime)...)
	return keys, requiresFallback
}

func subscriptionMatchIndexKeys(titles []string) ([]string, bool) {
	seen := make(map[string]struct{})
	keys := make([]string, 0)
	requiresFallback := false
	for _, title := range titles {
		for _, variant := range titleRuleVariants(title) {
			normalized := []rune(compactRuleTitle(variant))
			if len(normalized) == 0 {
				continue
			}
			if len(normalized) < subscriptionMatchIndexGramSize {
				requiresFallback = true
				continue
			}
			for i := 0; i+subscriptionMatchIndexGramSize <= len(normalized); i++ {
				key := string(normalized[i : i+subscriptionMatchIndexGramSize])
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				keys = append(keys, key)
			}
		}
	}
	return keys, requiresFallback
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
