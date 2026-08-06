package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"gorm.io/gorm"
)

const (
	subscriptionToneWarning     = "warning"
	subscriptionToneSuccess     = "success"
	subscriptionResolution1080p = "1080p"
)

func populateSubscriptionStats(subs []model.Subscription) {
	libraryIndex, err := loadSubscriptionLibraryIndex()
	if err != nil {
		log.Printf("subscription library index load failed: %v", err)
		libraryIndex = &subscriptionLibraryIndex{}
	}
	for i := range subs {
		populateSubscriptionStatWithLibraryIndex(&subs[i], libraryIndex)
	}
}
func loadSubscriptionTrendReport(windowDays int) SubscriptionTrendReport {
	if windowDays <= 0 {
		windowDays = 7
	}

	report := SubscriptionTrendReport{
		WindowLabel: fmt.Sprintf("近 %d 天", windowDays),
	}
	if db.DB == nil {
		return report
	}

	cutoff := time.Now().AddDate(0, 0, -windowDays)
	var recentSubs []model.Subscription
	if err := db.DB.Where("last_check_at IS NOT NULL AND last_check_at >= ?", cutoff).
		Order("last_check_at DESC").
		Find(&recentSubs).Error; err != nil {
		return report
	}

	var topIssues []SubscriptionTrendItem
	var recentWinners []SubscriptionTrendItem
	for _, sub := range recentSubs {
		report.CheckedCount++
		report.RecentNewDownloads += sub.LastNewDownloads
		switch sub.LastRunStatus {
		case service.SubscriptionRunStatusSuccess:
			report.SuccessCount++
		case service.SubscriptionRunStatusIdle:
			// An idle run completed its RSS check successfully but found no
			// new item (for example, all entries were already tracked). Count
			// it as a normal check so the overview does not misleadingly show
			// "success 0" for healthy subscriptions.
			report.SuccessCount++
		case service.SubscriptionRunStatusWarning:
			report.WarningCount++
			report.ActiveIssueCount++
			topIssues = append(topIssues, newSubscriptionTrendItem(sub))
		case service.SubscriptionRunStatusError:
			report.ErrorCount++
			report.ActiveIssueCount++
			topIssues = append(topIssues, newSubscriptionTrendItem(sub))
		}

		if sub.LastNewDownloads > 0 {
			recentWinners = append(recentWinners, newSubscriptionTrendItem(sub))
		}
	}

	sort.Slice(topIssues, func(i, j int) bool {
		if topIssues[i].Status != topIssues[j].Status {
			return topIssues[i].Status == service.SubscriptionRunStatusError
		}
		return topIssues[i].LastNewDownloads < topIssues[j].LastNewDownloads
	})
	if len(topIssues) > 5 {
		topIssues = topIssues[:5]
	}
	report.TopIssueSubscriptions = topIssues

	sort.Slice(recentWinners, func(i, j int) bool {
		if recentWinners[i].LastNewDownloads != recentWinners[j].LastNewDownloads {
			return recentWinners[i].LastNewDownloads > recentWinners[j].LastNewDownloads
		}
		return recentWinners[i].Title < recentWinners[j].Title
	})
	if len(recentWinners) > 5 {
		recentWinners = recentWinners[:5]
	}
	report.RecentWinningSubscriptions = recentWinners

	db.DB.Model(&model.DownloadLog{}).
		Where("created_at >= ?", cutoff).
		Count(&report.DownloadLogCount)
	db.DB.Model(&model.DownloadLog{}).
		Where("status = ? AND updated_at >= ?", "completed", cutoff).
		Count(&report.CompletedCount)

	return report
}

func newSubscriptionTrendItem(sub model.Subscription) SubscriptionTrendItem {
	item := SubscriptionTrendItem{
		ID:               sub.ID,
		Title:            sub.Title,
		Status:           sub.LastRunStatus,
		StatusLabel:      subscriptionStatusLabel(sub.LastRunStatus),
		LastRunSummary:   normalizeSubscriptionRunSummary(sub.LastRunSummary),
		LastError:        sub.LastError,
		LastNewDownloads: sub.LastNewDownloads,
		LastCheckLabel:   "未知",
	}
	if sub.LastCheckAt != nil {
		item.LastCheckLabel = humanizeTimeAgo(time.Since(*sub.LastCheckAt))
	}
	return item
}

func subscriptionStatusLabel(status string) string {
	switch status {
	case service.SubscriptionRunStatusSuccess:
		return "正常"
	case service.SubscriptionRunStatusWarning:
		return "警告"
	case service.SubscriptionRunStatusError:
		return "失败"
	case service.SubscriptionRunStatusIdle:
		return "无更新"
	default:
		return "未知"
	}
}

func populateSubscriptionStat(sub *model.Subscription) {
	populateSubscriptionStatWithLibraryIndex(sub, nil)
}

func populateSubscriptionStatWithLibraryIndex(sub *model.Subscription, libraryIndex *subscriptionLibraryIndex) {
	if sub == nil {
		return
	}
	sub.LastRunSummary = normalizeSubscriptionRunSummary(sub.LastRunSummary)
	logStore := downloadLogStore()
	if logStore == nil {
		return
	}

	count, _ := logStore.CountDistinctEpisodesBySubscription(sub.ID, []string{"downloading", "completed", "renamed"})
	sub.DownloadedCount = count
	if db.DB != nil {
		var resources []model.SubscriptionResource
		if err := db.DB.Where("subscription_id = ?", sub.ID).Find(&resources).Error; err == nil {
			sub.RSSCount,
				sub.CanonicalEpisodeCount,
				sub.ConfirmedCount,
				sub.DownloadingCount,
				sub.CompletedCount,
				sub.FailedCount,
				sub.UnresolvedCount = service.ResourceSummary(resources)
			for _, resource := range resources {
				if service.ResourceNeedsAttention(resource) {
					sub.NeedsAttention = true
					break
				}
			}
		}
	}
	if libraryIndex == nil {
		populateSubscriptionActionHints(sub)
		return
	}
	populateSubscriptionActionHintsWithLibraryIndex(sub, libraryIndex)
}
func normalizeSubscriptionRunSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	var total int
	if _, err := fmt.Sscanf(summary, "检查到 %d 集，但都被过滤规则跳过", &total); err == nil {
		legacy := fmt.Sprintf("检查到 %d 集，但都被过滤规则跳过", total)
		if strings.HasPrefix(summary, legacy) {
			return fmt.Sprintf("本次 RSS 返回 %d 条资源，均被过滤规则跳过", total) + strings.TrimPrefix(summary, legacy)
		}
	}
	if _, err := fmt.Sscanf(summary, "检查到 %d 集，但都已经在下载记录中", &total); err == nil {
		legacy := fmt.Sprintf("检查到 %d 集，但都已经在下载记录中", total)
		if strings.HasPrefix(summary, legacy) {
			return fmt.Sprintf("本次 RSS 返回 %d 条资源，均已存在于历史下载记录", total) + strings.TrimPrefix(summary, legacy)
		}
	}

	var filtered, duplicate int
	if _, err := fmt.Sscanf(summary, "未发现新剧集（过滤 %d，已存在 %d）", &filtered, &duplicate); err == nil {
		legacy := fmt.Sprintf("未发现新剧集（过滤 %d，已存在 %d）", filtered, duplicate)
		if strings.HasPrefix(summary, legacy) {
			return fmt.Sprintf("本次 RSS 返回 %d 条资源（过滤 %d，已存在 %d），未发现新增", filtered+duplicate, filtered, duplicate) + strings.TrimPrefix(summary, legacy)
		}
	}
	return summary
}

func loadSubscriptionCard(id uint) (model.Subscription, error) {
	sub, err := subscriptionWithMetadataByID(id)
	if err != nil {
		return model.Subscription{}, err
	}

	populateSubscriptionStat(sub)
	return *sub, nil
}

func populateSubscriptionActionHints(sub *model.Subscription) {
	populateSubscriptionActionHintsWithLibraryIndex(sub, nil)
}

func populateSubscriptionActionHintsWithLibraryIndex(sub *model.Subscription, libraryIndex *subscriptionLibraryIndex) {
	if sub == nil {
		return
	}

	resetSubscriptionActionHints(sub)
	populateSubscriptionProgressHints(sub)
	populateSubscriptionStaleHints(sub)
	if libraryIndex == nil {
		populateSubscriptionLibraryState(sub)
	} else {
		populateSubscriptionLibraryStateFromIndex(sub, libraryIndex)
	}
	sub.HasRepairActions = subscriptionHasRepairActions(sub)
	if sub.LastRunStatus != service.SubscriptionRunStatusIdle {
		populateSubscriptionLifecycle(sub)
		return
	}
	if strings.Contains(sub.LastRunSummary, emptySubgroupFeedHint) {
		baseRSS, ok := deriveBaseRSSURL(sub.RSSUrl)
		if ok {
			sub.CanUseBaseRSS = true
			sub.BaseRSSURL = baseRSS
		}
	}
	if (strings.Contains(sub.LastRunSummary, filteredAllHint) || strings.Contains(sub.LastRunSummary, legacyFilteredAllHint)) && subscriptionHasReleaseFilters(sub) {
		sub.CanClearFilter = true
	}
	if (strings.Contains(sub.LastRunSummary, duplicateOnlyHint) || strings.Contains(sub.LastRunSummary, legacyDuplicateOnlyHint)) && hasResettableSubscriptionLogs(sub.ID, staleLogResetAge) {
		sub.CanResetStaleLogs = true
	}
	sub.HasRepairActions = subscriptionHasRepairActions(sub)
	populateSubscriptionLifecycle(sub)
}
func subscriptionHasReleaseFilters(sub *model.Subscription) bool {
	if sub == nil {
		return false
	}
	return strings.TrimSpace(sub.FilterRule) != "" ||
		strings.TrimSpace(sub.ExcludeRule) != "" ||
		strings.TrimSpace(sub.ResolutionFilter) != "" ||
		strings.TrimSpace(sub.SubtitleLanguage) != ""
}

func resetSubscriptionActionHints(sub *model.Subscription) {
	sub.CanUseBaseRSS = false
	sub.BaseRSSURL = ""
	sub.CanClearFilter = false
	sub.CanResetStaleLogs = false
	sub.CanRetryMissing = false
	sub.CanRetryStale = false
	sub.CanRetryUpgrade = false
	sub.CanRefreshLibrary = false
	sub.HasRepairActions = false
	sub.StrategyHint = ""
	sub.LifecycleStage = ""
	sub.LifecycleTone = ""
	sub.LibraryStage = ""
	sub.LibraryTone = ""
	sub.LibraryHint = ""
	sub.LocalAnimeID = 0
	sub.LibraryEpisodeCount = 0
	sub.Playable = false
	sub.LastErrorDisplay = humanizeOperationError(sub.LastError)
}

func populateSubscriptionProgressHints(sub *model.Subscription) {
	if sub.ExpectedEpisodes > 0 && sub.LastEp < sub.ExpectedEpisodes {
		appendStrategyHint(sub, fmt.Sprintf("当前已追到 %d / %d 集，还差 %d 集达到完结目标。", sub.LastEp, sub.ExpectedEpisodes, sub.ExpectedEpisodes-sub.LastEp))
	}
	if missing := missingEpisodeSummary(sub); missing != "" {
		sub.CanRetryMissing = true
		appendStrategyHint(sub, missing)
	}
	if sub.AutoDisableOnDone && sub.ExpectedEpisodes > 0 && sub.LastEp >= sub.ExpectedEpisodes {
		appendStrategyHint(sub, "这部番剧的目标集数已经完成；如果还在运行，可以直接自动停用。")
	}
}

func populateSubscriptionStaleHints(sub *model.Subscription) {
	if sub.StaleAfterHours > 0 && sub.LastSuccessAt != nil && time.Since(*sub.LastSuccessAt) > time.Duration(sub.StaleAfterHours)*time.Hour {
		sub.CanRetryStale = true
		appendStrategyHint(sub, fmt.Sprintf("已经超过 %d 小时没有出现新进展，建议检查 RSS 源、下载器或备用 RSS。", sub.StaleAfterHours))
	}
	if staleCount := countResettableSubscriptionLogs(sub.ID, staleLogResetAge); staleCount > 0 {
		appendStrategyHint(sub, fmt.Sprintf("有 %d 条下载记录已经卡住超过 24 小时，可直接清理阻塞后重试。", staleCount))
	}
}

func subscriptionHasRepairActions(sub *model.Subscription) bool {
	// Quality upgrades and media-server refreshes are optional recommendations.
	// They can remain available when no better release exists or before Jellyfin
	// has written its IDs back, so they must not create a permanent red issue.
	return sub.CanUseBaseRSS || sub.CanClearFilter || sub.CanResetStaleLogs || sub.CanRetryMissing || sub.CanRetryStale
}

func populateSubscriptionLifecycle(sub *model.Subscription) {
	if sub == nil {
		return
	}

	switch {
	case sub.CanRetryMissing:
		sub.LifecycleStage = "疑似缺集"
		sub.LifecycleTone = subscriptionToneWarning
	case sub.CanResetStaleLogs:
		sub.LifecycleStage = "下载阻塞"
		sub.LifecycleTone = "danger"
	case sub.CanRetryStale:
		sub.LifecycleStage = "长期无进展"
		sub.LifecycleTone = subscriptionToneWarning
	case !sub.IsActive && sub.AutoDisableOnDone && sub.ExpectedEpisodes > 0 && sub.LastEp >= sub.ExpectedEpisodes:
		sub.LifecycleStage = "已完结停用"
		sub.LifecycleTone = subscriptionToneSuccess
	case sub.ExpectedEpisodes > 0 && sub.LastEp >= sub.ExpectedEpisodes:
		sub.LifecycleStage = "已追平目标"
		sub.LifecycleTone = subscriptionToneSuccess
	case sub.ExpectedEpisodes > 0 && sub.LastEp > 0:
		sub.LifecycleStage = "追更中"
		sub.LifecycleTone = "info"
	case sub.IsActive:
		sub.LifecycleStage = "运行中"
		sub.LifecycleTone = "neutral"
	default:
		sub.LifecycleStage = "待配置"
		sub.LifecycleTone = "neutral"
	}
}

func appendStrategyHint(sub *model.Subscription, hint string) {
	if sub == nil {
		return
	}
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return
	}
	if sub.StrategyHint == "" {
		sub.StrategyHint = hint
		return
	}
	sub.StrategyHint = sub.StrategyHint + " " + hint
}

type subscriptionEpisodeStats struct {
	LocalAnimeID             uint  `gorm:"column:local_anime_id"`
	EpisodeCount             int64 `gorm:"column:episode_count"`
	JellyfinEpisodeCount     int64 `gorm:"column:jellyfin_episode_count"`
	LowResolutionEpisodeNums []int
}

type subscriptionLibraryIndex struct {
	localAnimes                     []model.LocalAnime
	statsByAnime                    map[uint]subscriptionEpisodeStats
	candidateIndexesByKey           map[string][]int
	providerIndexesByKey            map[string][]int
	fallbackIndexes                 []int
	pathLinkedIndexesBySubscription map[uint][]int
}

type subscriptionEpisodeResolution struct {
	LocalAnimeID    uint `gorm:"column:local_anime_id"`
	EpisodeNum      int  `gorm:"column:episode_num"`
	ResolutionScore int  `gorm:"column:resolution_score"`
}

type subscriptionLocalPathLink struct {
	SubscriptionID uint `gorm:"column:subscription_id"`
	LocalAnimeID   uint `gorm:"column:local_anime_id"`
}

type subscriptionLocalMatch struct {
	AnimeID      uint
	EpisodeCount int64
	Playable     bool
}

const subscriptionProviderSeason = "season"

func loadSubscriptionLocalMatchIndex() (*subscriptionLibraryIndex, error) {
	localStore := localAnimeStore()
	if localStore == nil {
		return nil, gorm.ErrInvalidDB
	}
	localAnimes, err := localStore.ListForSubscriptionMatching()
	if err != nil {
		return nil, err
	}
	index := &subscriptionLibraryIndex{
		localAnimes:           localAnimes,
		candidateIndexesByKey: make(map[string][]int),
		providerIndexesByKey:  make(map[string][]int),
	}
	buildSubscriptionCandidateIndex(index)
	if resolved, cleanupErr := resolveStaleSubscriptionProviderConflicts(index); cleanupErr != nil {
		log.Printf("WARN: failed to reconcile stale subscription provider conflicts: %v", cleanupErr)
	} else if resolved > 0 {
		log.Printf("INFO: resolved %d stale subscription provider conflict issues", resolved)
	}
	return index, nil
}

func loadSubscriptionLibraryIndex() (*subscriptionLibraryIndex, error) {
	index, err := loadSubscriptionLocalMatchIndex()
	if err != nil {
		return nil, err
	}
	index.statsByAnime = make(map[uint]subscriptionEpisodeStats, len(index.localAnimes))
	index.pathLinkedIndexesBySubscription = make(map[uint][]int)
	if len(index.localAnimes) == 0 {
		return index, nil
	}
	if err := loadSubscriptionPathLinksIntoIndex(index); err != nil {
		return nil, err
	}
	animeIDs := make([]uint, 0, len(index.localAnimes))
	for _, anime := range index.localAnimes {
		animeIDs = append(animeIDs, anime.ID)
	}
	if err := loadSubscriptionEpisodeStatsIntoIndex(index, animeIDs); err != nil {
		return nil, err
	}
	return index, nil
}

func loadSubscriptionLocalPathLinks(subscriptionID uint) ([]subscriptionLocalPathLink, error) {
	if db.DB == nil {
		return nil, gorm.ErrInvalidDB
	}
	var links []subscriptionLocalPathLink
	query := db.DB.Table("download_logs").
		Select("download_logs.subscription_id, local_episodes.local_anime_id").
		Joins(`JOIN local_episodes
			ON lower(local_episodes.path) = lower(download_logs.target_file)
			AND local_episodes.deleted_at IS NULL`).
		Where("download_logs.deleted_at IS NULL").
		Where("download_logs.status IN ?", []string{"completed", "renamed"}).
		Where("trim(download_logs.target_file) <> ''")
	if subscriptionID != 0 {
		query = query.Where("download_logs.subscription_id = ?", subscriptionID)
	}
	err := query.
		Group("download_logs.subscription_id, local_episodes.local_anime_id").
		Order("download_logs.subscription_id ASC, local_episodes.local_anime_id ASC").
		Scan(&links).Error
	return links, err
}

func loadSubscriptionPathLinksIntoIndex(index *subscriptionLibraryIndex) error {
	if index == nil || len(index.localAnimes) == 0 {
		return nil
	}
	links, err := loadSubscriptionLocalPathLinks(0)
	if err != nil {
		return err
	}
	localAnimeIndexByID := make(map[uint]int, len(index.localAnimes))
	for i := range index.localAnimes {
		localAnimeIndexByID[index.localAnimes[i].ID] = i
	}
	for _, link := range links {
		localAnimeIndex, ok := localAnimeIndexByID[link.LocalAnimeID]
		if !ok {
			continue
		}
		index.pathLinkedIndexesBySubscription[link.SubscriptionID] = append(
			index.pathLinkedIndexesBySubscription[link.SubscriptionID],
			localAnimeIndex,
		)
	}
	return nil
}

func loadSubscriptionEpisodeStatsIntoIndex(index *subscriptionLibraryIndex, animeIDs []uint) error {
	if index == nil || len(animeIDs) == 0 || db.DB == nil {
		return nil
	}
	var rows []subscriptionEpisodeStats
	if err := db.DB.Model(&model.LocalEpisode{}).
		Select("local_anime_id, COUNT(*) AS episode_count, SUM(CASE WHEN jellyfin_item_id <> '' THEN 1 ELSE 0 END) AS jellyfin_episode_count").
		Where("local_anime_id IN ?", animeIDs).
		Group("local_anime_id").
		Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		index.statsByAnime[row.LocalAnimeID] = row
	}

	resolutionScoreExpression := `MAX(CASE
		WHEN instr(lower(resolution), '2160') > 0 OR instr(lower(resolution), '4k') > 0 OR instr(lower(resolution), '3840x2160') > 0 THEN 4
		WHEN instr(lower(resolution), '1080') > 0 OR instr(lower(resolution), '1920x1080') > 0 OR instr(lower(resolution), 'fhd') > 0 THEN 3
		WHEN instr(lower(resolution), '720') > 0 OR instr(lower(resolution), '1280x720') > 0 OR instr(lower(resolution), 'hd') > 0 THEN 2
		WHEN instr(lower(resolution), '480') > 0 OR instr(lower(resolution), '360') > 0 THEN 1
		ELSE 0 END)`
	var resolutions []subscriptionEpisodeResolution
	if err := db.DB.Model(&model.LocalEpisode{}).
		Select("local_anime_id, episode_num, "+resolutionScoreExpression+" AS resolution_score").
		Where("local_anime_id IN ? AND episode_num > 0 AND resolution <> ''", animeIDs).
		Group("local_anime_id, episode_num").
		Having(resolutionScoreExpression+" > 0 AND "+resolutionScoreExpression+" < ?", resolutionScore(subscriptionResolution1080p)).
		Scan(&resolutions).Error; err != nil {
		return err
	}
	for _, row := range resolutions {
		stats := index.statsByAnime[row.LocalAnimeID]
		stats.LowResolutionEpisodeNums = append(stats.LowResolutionEpisodeNums, row.EpisodeNum)
		index.statsByAnime[row.LocalAnimeID] = stats
	}
	for animeID, stats := range index.statsByAnime {
		sort.Ints(stats.LowResolutionEpisodeNums)
		index.statsByAnime[animeID] = stats
	}
	return nil
}

func subscriptionMetadataForLibraryMatch(sub *model.Subscription) *model.AnimeMetadata {
	if sub == nil {
		return nil
	}
	if sub.Metadata != nil {
		return sub.Metadata
	}
	if sub.MetadataID == nil || *sub.MetadataID == 0 {
		return nil
	}
	metadataStore := animeMetadataStore()
	if metadataStore == nil {
		return nil
	}
	metadata, err := metadataStore.GetByID(*sub.MetadataID)
	if err != nil {
		return nil
	}
	sub.Metadata = metadata
	return metadata
}

func loadSubscriptionMatchLocalAnimesByIDs(animeIDs []uint) ([]model.LocalAnime, error) {
	if len(animeIDs) == 0 {
		return nil, nil
	}
	var localAnimes []model.LocalAnime
	err := db.DB.Preload("Metadata").
		Where("id IN ?", animeIDs).
		Order("local_animes.id ASC").
		Find(&localAnimes).Error
	return localAnimes, err
}

func findSubscriptionPathLinkedLocalAnimes(sub *model.Subscription) ([]model.LocalAnime, error) {
	if sub == nil || sub.ID == 0 {
		return nil, nil
	}
	links, err := loadSubscriptionLocalPathLinks(sub.ID)
	if err != nil {
		return nil, err
	}
	animeIDs := make([]uint, 0, len(links))
	for _, link := range links {
		animeIDs = append(animeIDs, link.LocalAnimeID)
	}
	return loadSubscriptionMatchLocalAnimesByIDs(animeIDs)
}

func findSubscriptionExternalIDLocalAnimes(sub *model.Subscription) ([]model.LocalAnime, error) {
	metadata := subscriptionMetadataForLibraryMatch(sub)
	if metadata == nil || (metadata.BangumiID == 0 && metadata.TMDBID == 0 && metadata.AniListID == 0) {
		return nil, nil
	}
	var localAnimes []model.LocalAnime
	err := db.DB.Model(&model.LocalAnime{}).
		Joins("JOIN anime_metadata ON anime_metadata.id = local_animes.metadata_id AND anime_metadata.deleted_at IS NULL").
		Where(`(
			(? > 0 AND anime_metadata.bangumi_id = ?) OR
			(? > 0 AND anime_metadata.tmdb_id = ?) OR
			(? > 0 AND anime_metadata.ani_list_id = ?)
		)`, metadata.BangumiID, metadata.BangumiID, metadata.TMDBID, metadata.TMDBID, metadata.AniListID, metadata.AniListID).
		Where("EXISTS (?)", db.DB.Model(&model.LocalEpisode{}).
			Select("1").
			Where("local_episodes.local_anime_id = local_animes.id")).
		Preload("Metadata").
		Order("local_animes.id ASC").
		Find(&localAnimes).Error
	if err != nil {
		return nil, err
	}
	return filterSubscriptionLocalAnimes(sub, localAnimes), nil
}

func filterSubscriptionPathLinkedLocalAnimes(sub *model.Subscription, candidates []model.LocalAnime) []model.LocalAnime {
	matched := make([]model.LocalAnime, 0, len(candidates))
	for i := range candidates {
		identity := service.EvaluateSubscriptionLocalIdentity(sub, &candidates[i])
		if identity.Conflict {
			// Path links can be stale or point at an unrelated local row. Only
			// report conflicts when the candidate has independent identity evidence.
			if shouldReportSubscriptionLocalIdentityConflict(sub, &candidates[i], identity) {
				reportSubscriptionLocalIdentityConflict(sub, &candidates[i], identity)
			}
			continue
		}
		matched = append(matched, candidates[i])
	}
	return matched
}

func disambiguateSubscriptionExternalMatches(
	sub *model.Subscription,
	providerMatches, pathLinked []model.LocalAnime,
) []model.LocalAnime {
	if len(providerMatches) <= 1 {
		reconcileSubscriptionDuplicateExternalIdentityIssues(sub, nil)
		return providerMatches
	}
	pathLinkedIDs := make(map[uint]struct{}, len(pathLinked))
	for _, anime := range pathLinked {
		pathLinkedIDs[anime.ID] = struct{}{}
	}
	pathSelected := make([]model.LocalAnime, 0, len(providerMatches))
	for _, anime := range providerMatches {
		if _, ok := pathLinkedIDs[anime.ID]; ok {
			pathSelected = append(pathSelected, anime)
		}
	}
	if len(pathSelected) == 1 {
		reconcileSubscriptionDuplicateExternalIdentityIssues(sub, nil)
		return pathSelected
	}

	titleSelected := make([]model.LocalAnime, 0, 1)
	for i := range providerMatches {
		if service.SubscriptionLocalTitleMatchScore(sub, &providerMatches[i]) == 100 {
			titleSelected = append(titleSelected, providerMatches[i])
		}
	}
	if len(titleSelected) == 1 {
		reconcileSubscriptionDuplicateExternalIdentityIssues(sub, nil)
		return titleSelected
	}

	reportSubscriptionDuplicateExternalIdentity(sub, providerMatches)
	reconcileSubscriptionDuplicateExternalIdentityIssues(sub, providerMatches)
	return nil
}

func reportSubscriptionLocalIdentityConflict(
	sub *model.Subscription,
	anime *model.LocalAnime,
	identity service.SubscriptionLocalIdentityResult,
) {
	if sub == nil || anime == nil || !identity.Conflict {
		return
	}
	providerLabel := strings.ToUpper(identity.Provider)
	issueType := service.LibraryIssueTypeScrape
	message := fmt.Sprintf(
		"订阅“%s”与本地番剧“%s”的 %s ID 冲突：订阅 %d，本地 %d",
		sub.Title,
		anime.Title,
		providerLabel,
		identity.SubscriptionProviderID,
		identity.LocalProviderID,
	)
	hint := "请在本地番剧详情中修正来源匹配；系统不会在来源 ID 冲突时自动显示播放按钮。"
	if identity.Provider == subscriptionProviderSeason {
		providerLabel = subscriptionProviderSeason
		issueType = service.LibraryIssueTypeParse
		message = fmt.Sprintf(
			"订阅“%s”与本地番剧“%s”的季度冲突：订阅第 %d 季，本地第 %d 季",
			sub.Title,
			anime.Title,
			identity.SubscriptionProviderID,
			identity.LocalProviderID,
		)
		hint = "请检查订阅标题、保存目录和本地番剧季度；系统不会跨季度自动关联。"
	}
	localAnimeID := anime.ID
	_ = service.ReportLibraryIssue(service.LibraryIssueInput{
		IssueKey:      fmt.Sprintf("subscription-provider-conflict:%d:%d:%s", sub.ID, anime.ID, providerLabel),
		IssueType:     issueType,
		Title:         sub.Title,
		DirectoryPath: anime.Path,
		LocalAnimeID:  &localAnimeID,
		Message:       message,
		Hint:          hint,
	})
}

func reportSubscriptionDuplicateExternalIdentity(sub *model.Subscription, matches []model.LocalAnime) {
	if sub == nil || len(matches) < 2 {
		return
	}
	identityKeys := service.SubscriptionExternalIdentityKeys(sub)
	sort.Strings(identityKeys)
	titles := make([]string, 0, len(matches))
	for _, anime := range matches {
		titles = append(titles, anime.Title)
	}
	message := fmt.Sprintf(
		"订阅外部身份 %s 同时匹配到多个本地番剧：%s",
		strings.Join(identityKeys, ", "),
		strings.Join(titles, "、"),
	)
	for i := range matches {
		localAnimeID := matches[i].ID
		_ = service.ReportLibraryIssue(service.LibraryIssueInput{
			IssueKey:      fmt.Sprintf("subscription-provider-duplicate:%d:%d", sub.ID, matches[i].ID),
			IssueType:     service.LibraryIssueTypeScrape,
			Title:         sub.Title,
			DirectoryPath: matches[i].Path,
			LocalAnimeID:  &localAnimeID,
			Message:       message,
			Hint:          "请修正重复的来源 ID，或保留一条可由完成下载路径唯一确认的本地番剧记录。",
		})
	}
}

func reconcileSubscriptionDuplicateExternalIdentityIssues(sub *model.Subscription, activeMatches []model.LocalAnime) {
	if sub == nil || sub.ID == 0 || db.DB == nil {
		return
	}
	var issues []model.LibraryIssue
	prefix := fmt.Sprintf("subscription-provider-duplicate:%d:", sub.ID)
	if err := db.DB.
		Where("status = ? AND issue_key LIKE ?", service.LibraryIssueStatusOpen, prefix+"%").
		Find(&issues).Error; err != nil {
		log.Printf("WARN: failed to reconcile duplicate provider issues for subscription_id=%d: %v", sub.ID, err)
		return
	}
	activeIDs := make(map[uint]struct{}, len(activeMatches))
	for i := range activeMatches {
		activeIDs[activeMatches[i].ID] = struct{}{}
	}
	for i := range issues {
		if issues[i].LocalAnimeID != nil {
			if _, keepOpen := activeIDs[*issues[i].LocalAnimeID]; keepOpen {
				continue
			}
		}
		if err := service.ResolveLibraryIssue(issues[i].IssueKey); err != nil {
			log.Printf("WARN: failed to resolve duplicate provider issue %q: %v", issues[i].IssueKey, err)
		}
	}
}

func findSubscriptionLocalAnimes(sub *model.Subscription) ([]model.LocalAnime, error) {
	if sub == nil || db.DB == nil {
		return nil, gorm.ErrInvalidDB
	}

	pathLinked, err := findSubscriptionPathLinkedLocalAnimes(sub)
	if err != nil {
		return nil, err
	}
	providerMatches, err := findSubscriptionExternalIDLocalAnimes(sub)
	if err != nil {
		return nil, err
	}
	disambiguatedProviderMatches := disambiguateSubscriptionExternalMatches(sub, providerMatches, pathLinked)
	if len(providerMatches) > 0 {
		return backfillSubscriptionLocalAnimeMetadata(sub, disambiguatedProviderMatches), nil
	}
	if matched := filterSubscriptionPathLinkedLocalAnimes(sub, pathLinked); len(matched) > 0 {
		return backfillSubscriptionLocalAnimeMetadata(sub, matched), nil
	}

	var localAnimes []model.LocalAnime
	if sub.MetadataID != nil && *sub.MetadataID != 0 {
		if err := db.DB.Preload("Metadata").
			Where("metadata_id = ?", *sub.MetadataID).
			Order("local_animes.id ASC").
			Find(&localAnimes).Error; err != nil {
			return nil, err
		}
		if matched := filterSubscriptionLocalAnimes(sub, localAnimes); len(matched) > 0 {
			// A historical database can contain multiple local rows pointing
			// at one metadata record. Treat that as ambiguous unless a
			// completed download path identifies one row.
			return disambiguateSubscriptionExternalMatches(sub, matched, pathLinked), nil
		}
		if len(localAnimes) > 0 {
			log.Printf("WARN: subscription %q shares metadata_id=%d with %d unrelated local series; ignoring metadata-only library match",
				sub.Title, *sub.MetadataID, len(localAnimes))
		}
	}

	titles := subscriptionLocalTitleCandidates(sub)
	if len(titles) == 0 {
		return nil, nil
	}
	if err := db.DB.Preload("Metadata").
		Where("title IN ?", titles).
		Order("local_animes.id ASC").
		Find(&localAnimes).Error; err != nil {
		return nil, err
	}
	localAnimes = filterSubscriptionLocalAnimes(sub, localAnimes)
	if len(localAnimes) == 0 {
		// Local folders commonly retain season or year suffixes, so an exact SQL
		// title lookup can miss a series that the identity matcher recognizes.
		// Restrict the fallback to series with indexed episodes before applying
		// the same strong matcher used by the safer metadata paths above.
		if err := db.DB.Preload("Metadata").
			Where("local_animes.id IN (?)", db.DB.Model(&model.LocalEpisode{}).Select("local_anime_id")).
			Order("local_animes.id ASC").
			Find(&localAnimes).Error; err != nil {
			return nil, err
		}
		localAnimes = filterSubscriptionLocalAnimes(sub, localAnimes)
	}
	return backfillSubscriptionLocalAnimeMetadata(sub, localAnimes), nil
}
func backfillSubscriptionLocalAnimeMetadata(sub *model.Subscription, localAnimes []model.LocalAnime) []model.LocalAnime {
	if len(localAnimes) != 1 || db.DB == nil || sub == nil || sub.MetadataID == nil || *sub.MetadataID == 0 || localAnimes[0].MetadataID != nil {
		return localAnimes
	}
	identity := service.EvaluateSubscriptionLocalIdentity(sub, &localAnimes[0])
	pathLinked := false
	if sub.ID != 0 {
		if links, err := loadSubscriptionLocalPathLinks(sub.ID); err == nil {
			for _, link := range links {
				if link.LocalAnimeID == localAnimes[0].ID {
					pathLinked = true
					break
				}
			}
		}
	}
	if identity.Conflict || (!identity.ExternalMatch && !identity.TitleMatch && !pathLinked) {
		return localAnimes
	}
	result := db.DB.Model(&model.LocalAnime{}).Where("id = ? AND metadata_id IS NULL", localAnimes[0].ID).
		Update("metadata_id", *sub.MetadataID)
	if result.Error == nil && result.RowsAffected == 1 {
		metadataID := *sub.MetadataID
		localAnimes[0].MetadataID = &metadataID
	}
	return localAnimes
}

func buildSubscriptionCandidateIndex(index *subscriptionLibraryIndex) {
	if index == nil {
		return
	}
	if index.candidateIndexesByKey == nil {
		index.candidateIndexesByKey = make(map[string][]int)
	}
	if index.providerIndexesByKey == nil {
		index.providerIndexesByKey = make(map[string][]int)
	}
	for i := range index.localAnimes {
		keys, requiresFallback := service.LocalAnimeSubscriptionIndexKeys(&index.localAnimes[i])
		for _, key := range keys {
			if strings.Contains(key, ":") {
				index.providerIndexesByKey[key] = append(index.providerIndexesByKey[key], i)
				continue
			}
			index.candidateIndexesByKey[key] = append(index.candidateIndexesByKey[key], i)
		}
		if requiresFallback || len(keys) == 0 {
			index.fallbackIndexes = append(index.fallbackIndexes, i)
		}
	}
}

func subscriptionLocalAnimeCandidates(sub *model.Subscription, index *subscriptionLibraryIndex) []model.LocalAnime {
	if sub == nil || index == nil {
		return nil
	}
	keys, requiresFullScan := service.SubscriptionLocalMatchIndexKeys(sub)
	if requiresFullScan {
		return index.localAnimes
	}
	seen := make(map[int]struct{}, len(index.fallbackIndexes)+len(keys))
	candidateIndexes := append([]int(nil), index.fallbackIndexes...)
	for _, candidateIndex := range candidateIndexes {
		seen[candidateIndex] = struct{}{}
	}
	for _, key := range keys {
		for _, candidateIndex := range index.candidateIndexesByKey[key] {
			if _, exists := seen[candidateIndex]; exists {
				continue
			}
			seen[candidateIndex] = struct{}{}
			candidateIndexes = append(candidateIndexes, candidateIndex)
		}
	}
	sort.Ints(candidateIndexes)
	candidates := make([]model.LocalAnime, 0, len(candidateIndexes))
	for _, candidateIndex := range candidateIndexes {
		if candidateIndex >= 0 && candidateIndex < len(index.localAnimes) {
			candidates = append(candidates, index.localAnimes[candidateIndex])
		}
	}
	return candidates
}

func subscriptionProviderAnimeCandidates(sub *model.Subscription, index *subscriptionLibraryIndex) []model.LocalAnime {
	if sub == nil || index == nil {
		return nil
	}
	seen := make(map[int]struct{})
	candidateIndexes := make([]int, 0)
	for _, key := range service.SubscriptionExternalIdentityKeys(sub) {
		for _, candidateIndex := range index.providerIndexesByKey[key] {
			if _, exists := seen[candidateIndex]; exists {
				continue
			}
			seen[candidateIndex] = struct{}{}
			candidateIndexes = append(candidateIndexes, candidateIndex)
		}
	}
	sort.Ints(candidateIndexes)
	candidates := make([]model.LocalAnime, 0, len(candidateIndexes))
	for _, candidateIndex := range candidateIndexes {
		if candidateIndex >= 0 && candidateIndex < len(index.localAnimes) {
			candidates = append(candidates, index.localAnimes[candidateIndex])
		}
	}
	return candidates
}
func findSubscriptionLocalAnimesFromIndex(sub *model.Subscription, libraryIndex *subscriptionLibraryIndex) []model.LocalAnime {
	if sub == nil || libraryIndex == nil {
		return nil
	}

	pathLinkedIndexes := libraryIndex.pathLinkedIndexesBySubscription[sub.ID]
	pathLinked := make([]model.LocalAnime, 0, len(pathLinkedIndexes))
	for _, localAnimeIndex := range pathLinkedIndexes {
		if localAnimeIndex >= 0 && localAnimeIndex < len(libraryIndex.localAnimes) {
			pathLinked = append(pathLinked, libraryIndex.localAnimes[localAnimeIndex])
		}
	}

	providerMatches := filterSubscriptionLocalAnimes(sub, subscriptionProviderAnimeCandidates(sub, libraryIndex))
	disambiguatedProviderMatches := disambiguateSubscriptionExternalMatches(sub, providerMatches, pathLinked)
	var matched []model.LocalAnime
	switch {
	case len(providerMatches) > 0:
		matched = disambiguatedProviderMatches
	case len(pathLinked) > 0:
		matched = filterSubscriptionPathLinkedLocalAnimes(sub, pathLinked)
	default:
		matched = filterSubscriptionLocalAnimes(sub, subscriptionLocalAnimeCandidates(sub, libraryIndex))
	}
	matched = backfillSubscriptionLocalAnimeMetadata(sub, matched)
	if len(matched) == 1 && matched[0].MetadataID != nil {
		for i := range libraryIndex.localAnimes {
			if libraryIndex.localAnimes[i].ID == matched[0].ID && libraryIndex.localAnimes[i].MetadataID == nil {
				metadataID := *matched[0].MetadataID
				libraryIndex.localAnimes[i].MetadataID = &metadataID
				break
			}
		}
	}
	return matched
}
func filterSubscriptionLocalAnimes(sub *model.Subscription, candidates []model.LocalAnime) []model.LocalAnime {
	matched := make([]model.LocalAnime, 0, len(candidates))
	for i := range candidates {
		identity := service.EvaluateSubscriptionLocalIdentity(sub, &candidates[i])
		if identity.Conflict {
			// Full-library title fallback intentionally examines many
			// unrelated rows. Only report a provider conflict when the row is
			// independently related by title or already shares a provider ID;
			// otherwise the health dashboard would be flooded with false
			// positives for every local series.
			if shouldReportSubscriptionLocalIdentityConflict(sub, &candidates[i], identity) {
				reportSubscriptionLocalIdentityConflict(sub, &candidates[i], identity)
			}
			continue
		}
		if service.LocalAnimeMatchesSubscription(sub, &candidates[i]) {
			matched = append(matched, candidates[i])
		}
	}
	return matched
}

func shouldReportSubscriptionLocalIdentityConflict(
	sub *model.Subscription,
	anime *model.LocalAnime,
	identity service.SubscriptionLocalIdentityResult,
) bool {
	if sub == nil || anime == nil || !identity.Conflict {
		return false
	}
	metadataLinked := sub.MetadataID != nil && anime.MetadataID != nil && *anime.MetadataID == *sub.MetadataID
	return identity.TitleMatch || identity.ExternalMatch ||
		(identity.Provider == subscriptionProviderSeason && metadataLinked)
}

func resolveStaleSubscriptionProviderConflicts(index *subscriptionLibraryIndex) (int, error) {
	if db.DB == nil || index == nil {
		return 0, nil
	}
	var issues []model.LibraryIssue
	if err := db.DB.
		Where("status = ? AND issue_key LIKE ?", service.LibraryIssueStatusOpen, "subscription-provider-conflict:%").
		Find(&issues).Error; err != nil {
		return 0, err
	}
	if len(issues) == 0 {
		return 0, nil
	}

	localByID := make(map[uint]*model.LocalAnime, len(index.localAnimes))
	for i := range index.localAnimes {
		localByID[index.localAnimes[i].ID] = &index.localAnimes[i]
	}
	subscriptionIDs := make(map[uint]struct{})
	type issueIdentity struct {
		subscriptionID uint
		provider       string
	}
	parsed := make(map[uint]issueIdentity, len(issues))
	for i := range issues {
		parts := strings.SplitN(issues[i].IssueKey, ":", 4)
		if len(parts) != 4 {
			continue
		}
		rawSubscriptionID, err := strconv.ParseUint(parts[1], 10, 32)
		if err != nil || rawSubscriptionID == 0 {
			continue
		}
		identity := issueIdentity{
			subscriptionID: uint(rawSubscriptionID),
			provider:       strings.ToLower(strings.TrimSpace(parts[3])),
		}
		parsed[issues[i].ID] = identity
		subscriptionIDs[identity.subscriptionID] = struct{}{}
	}

	ids := make([]uint, 0, len(subscriptionIDs))
	for id := range subscriptionIDs {
		ids = append(ids, id)
	}
	var subscriptions []model.Subscription
	if len(ids) > 0 {
		if err := db.DB.Preload("Metadata").Where("id IN ?", ids).Find(&subscriptions).Error; err != nil {
			return 0, err
		}
	}
	subByID := make(map[uint]*model.Subscription, len(subscriptions))
	for i := range subscriptions {
		subByID[subscriptions[i].ID] = &subscriptions[i]
	}

	resolved := 0
	for i := range issues {
		issue := &issues[i]
		parsedIdentity, ok := parsed[issue.ID]
		if !ok || issue.LocalAnimeID == nil || *issue.LocalAnimeID == 0 {
			continue
		}
		sub := subByID[parsedIdentity.subscriptionID]
		anime := localByID[*issue.LocalAnimeID]
		keepOpen := false
		if sub != nil && anime != nil {
			identity := service.EvaluateSubscriptionLocalIdentity(sub, anime)
			keepOpen = strings.EqualFold(identity.Provider, parsedIdentity.provider) &&
				shouldReportSubscriptionLocalIdentityConflict(sub, anime, identity)
		}
		if keepOpen {
			continue
		}
		if err := service.ResolveLibraryIssue(issue.IssueKey); err != nil {
			return resolved, err
		}
		resolved++
	}
	return resolved, nil
}

func subscriptionLocalTitleCandidates(sub *model.Subscription) []string {
	if sub == nil {
		return nil
	}
	values := []string{sub.Title, parser.CleanTitle(sub.Title)}
	if sub.Metadata != nil {
		values = append(values,
			sub.Metadata.Title,
			sub.Metadata.TitleCN,
			sub.Metadata.TitleEN,
			sub.Metadata.TitleJP,
			sub.Metadata.BangumiTitle,
			sub.Metadata.TMDBTitle,
			sub.Metadata.AniListTitle,
		)
	}
	seen := make(map[string]struct{}, len(values)*2)
	result := make([]string, 0, len(values)*2)
	for _, value := range values {
		for _, candidate := range []string{strings.TrimSpace(value), parser.CleanTitle(value)} {
			if candidate == "" {
				continue
			}
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			result = append(result, candidate)
		}
	}
	return result
}

func loadSubscriptionEpisodeStats(animeIDs []uint) (map[uint]subscriptionEpisodeStats, error) {
	var rows []subscriptionEpisodeStats
	err := db.DB.Model(&model.LocalEpisode{}).
		Select("local_anime_id, COUNT(*) AS episode_count, SUM(CASE WHEN jellyfin_item_id <> '' THEN 1 ELSE 0 END) AS jellyfin_episode_count").
		Where("local_anime_id IN ?", animeIDs).
		Group("local_anime_id").
		Scan(&rows).Error
	statsByAnime := make(map[uint]subscriptionEpisodeStats, len(rows))
	for _, item := range rows {
		statsByAnime[item.LocalAnimeID] = item
	}
	return statsByAnime, err
}

func selectSubscriptionLocalMatch(localAnimes []model.LocalAnime, statsByAnime map[uint]subscriptionEpisodeStats) subscriptionLocalMatch {
	selected := subscriptionLocalMatch{}
	selectedScore := -1
	for _, anime := range localAnimes {
		stats := statsByAnime[anime.ID]
		hasSeries := strings.TrimSpace(anime.JellyfinSeriesID) != ""
		playable := stats.EpisodeCount > 0 && (hasSeries || stats.JellyfinEpisodeCount > 0)
		score := 0
		if stats.EpisodeCount > 0 {
			score = 1
		}
		if playable {
			score = 2
		}
		if score > selectedScore || (score == selectedScore && stats.EpisodeCount > selected.EpisodeCount) {
			selectedScore = score
			selected = subscriptionLocalMatch{AnimeID: anime.ID, EpisodeCount: stats.EpisodeCount, Playable: playable}
		}
	}
	return selected
}

func populateSubscriptionLibraryState(sub *model.Subscription) {
	if sub == nil || db.DB == nil {
		return
	}

	localAnimes, err := findSubscriptionLocalAnimes(sub)
	if err != nil || len(localAnimes) == 0 {
		return
	}
	animeIDs := make([]uint, 0, len(localAnimes))
	for _, anime := range localAnimes {
		animeIDs = append(animeIDs, anime.ID)
	}
	statsByAnime, err := loadSubscriptionEpisodeStats(animeIDs)
	if err != nil {
		return
	}
	localMatch := selectSubscriptionLocalMatch(localAnimes, statsByAnime)
	if localMatch.AnimeID == 0 {
		return
	}

	var episodes []model.LocalEpisode
	if err := db.DB.Select("episode_num", "resolution").
		Where("local_anime_id IN ?", animeIDs).
		Find(&episodes).Error; err != nil {
		return
	}
	applySubscriptionLibraryMatch(sub, localMatch, episodes)
}

func populateSubscriptionLibraryStateFromIndex(sub *model.Subscription, libraryIndex *subscriptionLibraryIndex) {
	if sub == nil || libraryIndex == nil {
		return
	}
	localAnimes := findSubscriptionLocalAnimesFromIndex(sub, libraryIndex)
	if len(localAnimes) == 0 {
		return
	}
	localMatch := selectSubscriptionLocalMatch(localAnimes, libraryIndex.statsByAnime)
	if localMatch.AnimeID == 0 {
		return
	}
	stats := libraryIndex.statsByAnime[localMatch.AnimeID]
	applySubscriptionLibraryMatchWithUpgradeEpisodes(sub, localMatch, stats.LowResolutionEpisodeNums)
}

func applySubscriptionLibraryMatch(sub *model.Subscription, localMatch subscriptionLocalMatch, episodes []model.LocalEpisode) {
	applySubscriptionLibraryMatchWithUpgradeEpisodes(sub, localMatch, collectUpgradeableEpisodes(episodes))
}

func applySubscriptionLibraryMatchWithUpgradeEpisodes(sub *model.Subscription, localMatch subscriptionLocalMatch, lowResolutionEpisodes []int) {
	sub.LocalAnimeID = localMatch.AnimeID
	sub.LibraryEpisodeCount = localMatch.EpisodeCount
	sub.Playable = localMatch.Playable
	jellyfinConfigured := strings.TrimSpace(configValue(model.ConfigKeyJellyfinUrl)) != "" &&
		strings.TrimSpace(configValue(model.ConfigKeyJellyfinApiKey)) != ""

	totalEpisodes := localMatch.EpisodeCount
	if totalEpisodes > 0 {
		sub.LibraryStage = "已入库"
		sub.LibraryTone = "info"
		sub.LibraryHint = fmt.Sprintf("本地媒体库已识别 %d 集。", totalEpisodes)
	}

	if sub.Playable {
		sub.LibraryStage = "可播放"
		sub.LibraryTone = subscriptionToneSuccess
		if totalEpisodes > 0 {
			sub.LibraryHint = fmt.Sprintf("本地已入库 %d 集，Jellyfin 已建立条目，可直接播放。", totalEpisodes)
		} else {
			sub.LibraryHint = "Jellyfin 已建立条目，可直接播放。"
		}
	} else if totalEpisodes > 0 && jellyfinConfigured {
		sub.LibraryStage = "等待 Jellyfin 扫描"
		sub.LibraryTone = subscriptionToneWarning
		sub.LibraryHint = fmt.Sprintf("本地已识别 %d 集，正在等待 Jellyfin 扫描媒体文件；点击“同步 Jellyfin”可立即请求扫描。", totalEpisodes)
		sub.CanRefreshLibrary = true
	}

	lowResEpisodes := lowResolutionEpisodes
	if len(lowResEpisodes) > 0 {
		sub.CanRetryUpgrade = true
		upgradeHint := formatUpgradeHint(lowResEpisodes)
		appendStrategyHint(sub, upgradeHint)
		if sub.LibraryHint == "" {
			sub.LibraryHint = upgradeHint
		} else {
			sub.LibraryHint = strings.TrimSpace(sub.LibraryHint + " " + upgradeHint)
		}
	}
}
func waitForSubscriptionJellyfinMatch(ctx context.Context, subscriptionID uint) error {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		if _, err := service.SyncJellyfinLibraryMappings(ctx); err != nil {
			return err
		}
		card, err := loadSubscriptionCard(subscriptionID)
		if err != nil {
			return err
		}
		if card.Playable {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("等待 Jellyfin 扫描超时：已接收扫描请求，但 50 秒内仍未识别该番剧；请确认媒体库包含下载目录，并检查 tvshow.nfo")
		case <-ticker.C:
		}
	}
}

func collectUpgradeableEpisodes(episodes []model.LocalEpisode) []int {
	bestByEpisode := make(map[int]int)
	for _, episode := range episodes {
		if episode.EpisodeNum <= 0 {
			continue
		}
		score := resolutionScore(episode.Resolution)
		if current, ok := bestByEpisode[episode.EpisodeNum]; !ok || score > current {
			bestByEpisode[episode.EpisodeNum] = score
		}
	}

	upgradeable := make([]int, 0)
	for episodeNum, score := range bestByEpisode {
		// An absent/unrecognised resolution is not evidence that the episode is
		// low quality. Treating it as such creates an upgrade warning that can
		// never be resolved by checking the feed again.
		if score == 0 {
			continue
		}
		if score >= resolutionScore(subscriptionResolution1080p) {
			continue
		}
		upgradeable = append(upgradeable, episodeNum)
	}
	sort.Ints(upgradeable)
	return upgradeable
}

func formatUpgradeHint(episodes []int) string {
	if len(episodes) == 0 {
		return ""
	}
	preview := make([]string, 0, min(len(episodes), 4))
	for i := 0; i < len(episodes) && i < 4; i++ {
		preview = append(preview, fmt.Sprintf("%02d", episodes[i]))
	}
	if len(episodes) > 4 {
		return fmt.Sprintf("检测到 %s 等 %d 集仍是较低分辨率，可尝试洗版检查更优片源。", strings.Join(preview, "、"), len(episodes))
	}
	return fmt.Sprintf("检测到 %s 仍是较低分辨率，可尝试洗版检查更优片源。", strings.Join(preview, "、"))
}

func resolutionScore(raw string) int {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(value, "2160"), strings.Contains(value, "4k"), strings.Contains(value, "3840x2160"):
		return 4
	case strings.Contains(value, "1080"), strings.Contains(value, "1920x1080"), strings.Contains(value, "fhd"):
		return 3
	case strings.Contains(value, "720"), strings.Contains(value, "1280x720"), strings.Contains(value, "hd"):
		return 2
	case strings.Contains(value, "480"), strings.Contains(value, "360"):
		return 1
	default:
		return 0
	}
}

func deriveBaseRSSURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}

	query := u.Query()
	if query.Get("subgroupid") == "" {
		return "", false
	}
	query.Del("subgroupid")
	u.RawQuery = query.Encode()
	return u.String(), true
}

func applyBaseRSSFallback(sub *model.Subscription, baseRSS string) {
	if sub == nil {
		return
	}

	previousGroup := strings.TrimSpace(sub.SubtitleGroup)
	sub.RSSUrl = baseRSS
	sub.SubtitleGroup = ""
	if previousGroup != "" && strings.TrimSpace(sub.FilterRule) == previousGroup {
		sub.FilterRule = ""
	}
	sub.LastRunSummary = repairPendingSummary(repairActionUseBaseRSS)
	sub.LastError = ""
}

func useBaseRSSAndRecheck(sub *model.Subscription, baseRSS string) error {
	if sub == nil {
		return fmt.Errorf("subscription is nil")
	}

	applyBaseRSSFallback(sub, baseRSS)
	return persistRepairAndRecheck(sub, repairActionUseBaseRSS)
}

func clearFilterAndRecheck(sub *model.Subscription) error {
	if sub == nil {
		return fmt.Errorf("subscription is nil")
	}

	sub.FilterRule = ""
	sub.ExcludeRule = ""
	sub.ResolutionFilter = ""
	sub.SubtitleLanguage = ""
	sub.LastRunSummary = repairPendingSummary(repairActionClearFilter)
	sub.LastError = ""
	return persistRepairAndRecheck(sub, repairActionClearFilter)
}

func hasResettableSubscriptionLogs(subscriptionID uint, maxAge time.Duration) bool {
	return countResettableSubscriptionLogs(subscriptionID, maxAge) > 0
}

func countResettableSubscriptionLogs(subscriptionID uint, maxAge time.Duration) int64 {
	if subscriptionID == 0 {
		return 0
	}
	logStore := downloadLogStore()
	if logStore == nil {
		return 0
	}
	cutoff := time.Now().Add(-maxAge)
	count, _ := logStore.CountResettable(subscriptionID, []string{"downloading", "failed"}, cutoff)
	return count
}

func missingEpisodeSummary(sub *model.Subscription) string {
	if sub == nil || sub.ID == 0 || sub.ExpectedEpisodes <= 1 {
		return ""
	}
	logStore := downloadLogStore()
	if logStore == nil {
		return ""
	}

	logs, err := logStore.ListBySubscriptionAndStatuses(sub.ID, []string{"downloading", "completed", "renamed"})
	if err != nil {
		return ""
	}

	observed := make(map[int]struct{}, len(logs))
	for _, logEntry := range logs {
		ep := parser.NormalizeEpisodeNumber(logEntry.Episode)
		if ep == "" {
			// Older download logs may not have persisted Episode yet. Reuse
			// the same title parser as subscription de-duplication so those
			// records still participate in missing-episode diagnostics.
			ep = parser.EpisodeNumberFromTitle(logEntry.Title)
		}
		if ep == "" || strings.Contains(ep, ".") {
			continue
		}
		n, err := strconv.Atoi(ep)
		if err != nil || n <= 0 {
			continue
		}
		observed[n] = struct{}{}
	}
	if len(observed) == 0 {
		return ""
	}

	missing := make([]int, 0)
	for i := 1; i <= sub.ExpectedEpisodes; i++ {
		if _, ok := observed[i]; ok {
			continue
		}
		missing = append(missing, i)
	}
	if len(missing) == 0 {
		return ""
	}

	preview := make([]string, 0, min(len(missing), 4))
	for i := 0; i < len(missing) && i < 4; i++ {
		preview = append(preview, fmt.Sprintf("%02d", missing[i]))
	}
	if len(missing) > 4 {
		return fmt.Sprintf("疑似缺集：%s 等 %d 集，建议触发一次重检或检查备用 RSS。", strings.Join(preview, "、"), len(missing))
	}
	return fmt.Sprintf("疑似缺集：%s，建议触发一次重检或检查备用 RSS。", strings.Join(preview, "、"))
}

func resetStaleLogsAndRecheck(sub *model.Subscription, maxAge time.Duration) error {
	if sub == nil {
		return fmt.Errorf("subscription is nil")
	}
	logStore := downloadLogStore()
	if logStore == nil {
		return gorm.ErrInvalidDB
	}

	cutoff := time.Now().Add(-maxAge)
	if err := logStore.MarkResettableArchived(sub.ID, []string{"downloading", "failed"}, cutoff, "archived"); err != nil {
		return err
	}

	sub.LastRunSummary = repairPendingSummary(repairActionResetStaleLog)
	sub.LastError = ""
	return persistRepairAndRecheck(sub, repairActionResetStaleLog)
}

func persistRepairAndRecheck(sub *model.Subscription, action repairAction) error {
	if sub == nil {
		return fmt.Errorf("subscription is nil")
	}

	if err := saveSubscription(sub); err != nil {
		return err
	}

	if err := runSubscriptionCheck(sub, "manual"); err != nil {
		log.Printf("Subscription repair auto recheck skipped for %s: %v", sub.Title, err)
		sub.LastRunStatus = service.SubscriptionRunStatusIdle
		sub.LastRunSummary = repairAutoRecheckFailureSummary(action)
		sub.LastError = err.Error()
		if saveErr := saveSubscription(sub); saveErr != nil {
			return saveErr
		}
	}
	return nil
}

func loadSubscriptionHistory(id uint) (SubscriptionHistoryData, error) {
	sub, err := loadSubscriptionCard(id)
	if err != nil {
		return SubscriptionHistoryData{}, err
	}

	logStore := downloadLogStore()
	if logStore == nil {
		return SubscriptionHistoryData{}, gorm.ErrInvalidDB
	}
	logs, err := logStore.ListRecentVisibleBySubscription(sub.ID, 12)
	if err != nil {
		return SubscriptionHistoryData{}, err
	}

	var runs []model.SubscriptionRunLog
	if err := db.DB.Where("subscription_id = ?", sub.ID).
		Order("checked_at DESC").
		Limit(10).
		Find(&runs).Error; err != nil {
		return SubscriptionHistoryData{}, err
	}
	var resources []model.SubscriptionResource
	if err := db.DB.Where("subscription_id = ?", sub.ID).
		Order("season_val ASC, episode ASC, selected DESC, candidate_rank ASC, id ASC").
		Limit(200).
		Find(&resources).Error; err != nil {
		return SubscriptionHistoryData{}, err
	}

	return SubscriptionHistoryData{
		Subscription: sub,
		Runs:         runs,
		Logs:         logs,
		Resources:    resources,
	}, nil
}

func SearchAnimeHandler(c *gin.Context) {
	keyword := c.Query("q")
	if keyword == "" {
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, `<div class="p-4 text-center text-gray-500">请输入关键词进行搜索</div>`)
		return
	}

	p := newConfiguredMikanParser()
	results, err := p.Search(keyword)
	if err != nil {
		log.Printf("Search error: %v", err)
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, `<div class="p-4 text-center text-red-500">搜索失败: `+err.Error()+`</div>`)
		return
	}

	c.HTML(http.StatusOK, "search_results.html", gin.H{
		"Results": results,
	})
}

func GetSubgroupsHandler(c *gin.Context) {
	bangumiID := c.Query("id")
	if bangumiID == "" {
		subscriptionJSONBadRequest(c, "缺少番剧 ID")
		return
	}

	p := newConfiguredMikanParser()
	subgroups, err := p.GetSubgroups(bangumiID)
	if err != nil {
		log.Printf("GetSubgroups error: %v", err)
		subscriptionJSONServerError(c, "获取字幕组列表", err)
		return
	}

	c.JSON(http.StatusOK, subgroups)
}

func PreviewRSSHandler(c *gin.Context) {
	url := c.Query("RSSUrl")
	if url == "" {
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, `<div class="p-4 text-center text-gray-500">请输入有效 RSS 链接</div>`)
		return
	}

	p := newConfiguredMikanParser()
	episodes, err := p.Parse(url)
	if err != nil {
		log.Printf("Preview error: %v", err)
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, `<div class="p-4 text-center text-red-500">解析失败: `+err.Error()+`</div>`)
		return
	}

	c.HTML(http.StatusOK, "preview_results.html", gin.H{
		"Episodes": episodes,
	})
}

func GetMikanDashboardHandler(c *gin.Context) {
	year := c.Query("year")
	season := c.Query("season")

	p := newConfiguredMikanParser()
	dashboard, err := p.GetDashboard(year, season)
	if err != nil {
		log.Printf("GetMikanDashboard error: %v", err)
		subscriptionJSONServerError(c, "获取 Mikan 仪表盘", err)
		return
	}

	c.JSON(http.StatusOK, dashboard)
}

func RefreshSubscriptionsHandler(c *gin.Context) {
	subs, err := listSubscriptionsWithMetadata()
	if err != nil {
		subscriptionJSONServerError(c, "读取订阅列表", err)
		return
	}

	updatedCount := 0
	metaSvc := service.NewMetadataService()

	for i := range subs {
		metaSvc.EnrichMetadata(subs[i].Metadata, subs[i].Title)
		if err := saveSubscription(&subs[i]); err == nil {
			updatedCount++
		}
		time.Sleep(200 * time.Millisecond)
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("刷新完成，更新了 %d 个订阅的元数据", updatedCount),
		"updated": updatedCount,
		"total":   len(subs),
	})
}
