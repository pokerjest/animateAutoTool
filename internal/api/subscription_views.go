package api

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/service"
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
	if sub == nil || db.DB == nil {
		return
	}

	var count int64
	db.DB.Model(&model.DownloadLog{}).Where("subscription_id = ?", sub.ID).Count(&count)
	sub.DownloadedCount = count
	populateSubscriptionActionHints(sub)
}

func loadSubscriptionCard(id uint) (model.Subscription, error) {
	var sub model.Subscription
	if err := db.DB.Preload("Metadata").First(&sub, id).Error; err != nil {
		return model.Subscription{}, err
	}

	populateSubscriptionStat(&sub)
	return sub, nil
}

func populateSubscriptionActionHints(sub *model.Subscription) {
	if sub == nil {
		return
	}

	sub.CanUseBaseRSS = false
	sub.BaseRSSURL = ""
	sub.CanClearFilter = false
	sub.CanResetStaleLogs = false
	sub.HasRepairActions = false
	sub.LastErrorDisplay = humanizeOperationError(sub.LastError)
	if sub.LastRunStatus != service.SubscriptionRunStatusIdle {
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
	sub.HasRepairActions = sub.CanUseBaseRSS || sub.CanClearFilter || sub.CanResetStaleLogs
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
	if subscriptionID == 0 || db.DB == nil {
		return false
	}

	var count int64
	cutoff := time.Now().Add(-maxAge)
	db.DB.Model(&model.DownloadLog{}).
		Where("subscription_id = ? AND status IN ? AND created_at < ?", subscriptionID, []string{"downloading", "failed"}, cutoff).
		Count(&count)
	return count > 0
}

func resetStaleLogsAndRecheck(sub *model.Subscription, maxAge time.Duration) error {
	if sub == nil {
		return fmt.Errorf("subscription is nil")
	}

	cutoff := time.Now().Add(-maxAge)
	if err := db.DB.Model(&model.DownloadLog{}).
		Where("subscription_id = ? AND status IN ? AND created_at < ?", sub.ID, []string{"downloading", "failed"}, cutoff).
		Update("status", "archived").Error; err != nil {
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

	if err := db.DB.Save(sub).Error; err != nil {
		return err
	}

	if err := runSubscriptionCheck(sub, "manual"); err != nil {
		log.Printf("Subscription repair auto recheck skipped for %s: %v", sub.Title, err)
		sub.LastRunStatus = service.SubscriptionRunStatusIdle
		sub.LastRunSummary = repairAutoRecheckFailureSummary(action)
		sub.LastError = err.Error()
		if saveErr := db.DB.Save(sub).Error; saveErr != nil {
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

	var logs []model.DownloadLog
	if err := db.DB.Where("subscription_id = ?", sub.ID).
		Order("created_at DESC").
		Limit(12).
		Find(&logs).Error; err != nil {
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

	p := parser.NewMikanParser()
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

	p := parser.NewMikanParser()
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

	p := parser.NewMikanParser()
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

	p := parser.NewMikanParser()
	dashboard, err := p.GetDashboard(year, season)
	if err != nil {
		log.Printf("GetMikanDashboard error: %v", err)
		subscriptionJSONServerError(c, "获取 Mikan 仪表盘", err)
		return
	}

	c.JSON(http.StatusOK, dashboard)
}

func RefreshSubscriptionsHandler(c *gin.Context) {
	var subs []model.Subscription
	if err := db.DB.Preload("Metadata").Find(&subs).Error; err != nil {
		subscriptionJSONServerError(c, "读取订阅列表", err)
		return
	}

	updatedCount := 0
	metaSvc := service.NewMetadataService()

	for i := range subs {
		metaSvc.EnrichMetadata(subs[i].Metadata, subs[i].Title)
		if err := db.DB.Save(&subs[i]).Error; err == nil {
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
