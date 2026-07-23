package api

import (
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
	for i := range subs {
		populateSubscriptionStat(&subs[i])
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
		LastRunSummary:   sub.LastRunSummary,
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
	if sub == nil {
		return
	}
	logStore := downloadLogStore()
	if logStore == nil {
		return
	}

	count, _ := logStore.CountBySubscription(sub.ID)
	sub.DownloadedCount = count
	populateSubscriptionActionHints(sub)
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
	if sub == nil {
		return
	}

	resetSubscriptionActionHints(sub)
	populateSubscriptionProgressHints(sub)
	populateSubscriptionStaleHints(sub)
	populateSubscriptionLibraryState(sub)
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
	if strings.Contains(sub.LastRunSummary, filteredAllHint) && strings.TrimSpace(sub.FilterRule) != "" {
		sub.CanClearFilter = true
	}
	if strings.Contains(sub.LastRunSummary, duplicateOnlyHint) && hasResettableSubscriptionLogs(sub.ID, staleLogResetAge) {
		sub.CanResetStaleLogs = true
	}
	sub.HasRepairActions = subscriptionHasRepairActions(sub)
	populateSubscriptionLifecycle(sub)
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
	return sub.CanUseBaseRSS || sub.CanClearFilter || sub.CanResetStaleLogs || sub.CanRetryMissing || sub.CanRetryStale || sub.CanRetryUpgrade || sub.CanRefreshLibrary
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

func populateSubscriptionLibraryState(sub *model.Subscription) {
	if sub == nil || db.DB == nil {
		return
	}

	animeIDs := make([]uint, 0, 2)
	var localAnimes []model.LocalAnime
	query := db.DB.Model(&model.LocalAnime{})
	switch {
	case sub.MetadataID != nil && *sub.MetadataID != 0:
		query = query.Where("metadata_id = ?", *sub.MetadataID)
	case sub.Metadata != nil && sub.Metadata.BangumiID != 0:
		query = query.Joins("JOIN anime_metadata ON anime_metadata.id = local_animes.metadata_id").
			Where("anime_metadata.bangumi_id = ?", sub.Metadata.BangumiID)
	default:
		cleanTitle := parser.CleanTitle(sub.Title)
		query = query.Where("title = ? OR title = ?", sub.Title, cleanTitle)
	}
	if err := query.Find(&localAnimes).Error; err != nil || len(localAnimes) == 0 {
		return
	}

	hasSeriesInJellyfin := false
	for _, anime := range localAnimes {
		animeIDs = append(animeIDs, anime.ID)
		if strings.TrimSpace(anime.JellyfinSeriesID) != "" {
			hasSeriesInJellyfin = true
		}
	}
	if len(animeIDs) == 0 {
		return
	}
	jellyfinConfigured := strings.TrimSpace(configValue(model.ConfigKeyJellyfinUrl)) != "" &&
		strings.TrimSpace(configValue(model.ConfigKeyJellyfinApiKey)) != ""

	var totalEpisodes int64
	db.DB.Model(&model.LocalEpisode{}).Where("local_anime_id IN ?", animeIDs).Count(&totalEpisodes)
	if totalEpisodes > 0 {
		sub.LibraryStage = "已入库"
		sub.LibraryTone = "info"
		sub.LibraryHint = fmt.Sprintf("本地媒体库已识别 %d 集。", totalEpisodes)
	}

	var jellyfinEpisodeCount int64
	db.DB.Model(&model.LocalEpisode{}).
		Where("local_anime_id IN ? AND jellyfin_item_id <> ''", animeIDs).
		Count(&jellyfinEpisodeCount)
	if hasSeriesInJellyfin || jellyfinEpisodeCount > 0 {
		sub.LibraryStage = "可播放"
		sub.LibraryTone = subscriptionToneSuccess
		if totalEpisodes > 0 {
			sub.LibraryHint = fmt.Sprintf("本地已入库 %d 集，Jellyfin 已建立条目，可直接播放。", totalEpisodes)
		} else {
			sub.LibraryHint = "Jellyfin 已建立条目，可直接播放。"
		}
	} else if totalEpisodes > 0 && jellyfinConfigured {
		sub.LibraryStage = "待同步到媒体库"
		sub.LibraryTone = subscriptionToneWarning
		sub.LibraryHint = fmt.Sprintf("本地已经识别 %d 集，但 Jellyfin 还没有建立条目；建议触发一次库刷新。", totalEpisodes)
		sub.CanRefreshLibrary = true
	}

	var episodes []model.LocalEpisode
	if err := db.DB.Select("episode_num", "resolution").
		Where("local_anime_id IN ?", animeIDs).
		Find(&episodes).Error; err == nil {
		lowResEpisodes := collectUpgradeableEpisodes(episodes)
		if len(lowResEpisodes) > 0 {
			sub.CanRetryUpgrade = true
			appendStrategyHint(sub, formatUpgradeHint(lowResEpisodes))
			if sub.LibraryHint == "" {
				sub.LibraryHint = formatUpgradeHint(lowResEpisodes)
			} else {
				sub.LibraryHint = strings.TrimSpace(sub.LibraryHint + " " + formatUpgradeHint(lowResEpisodes))
			}
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
		ep := strings.TrimSpace(logEntry.Episode)
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
	logs, err := logStore.ListBySubscription(sub.ID, 12)
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

	return SubscriptionHistoryData{
		Subscription: sub,
		Runs:         runs,
		Logs:         logs,
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
