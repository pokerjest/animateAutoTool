package api

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
	"github.com/pokerjest/animateAutoTool/internal/scheduler"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"gorm.io/gorm"
)

const emptySubgroupFeedHint = "当前字幕组 RSS 为空"
const filteredAllHint = "都被过滤规则跳过"
const duplicateOnlyHint = "都已经在下载记录中"
const staleLogResetAge = 24 * time.Hour

func subscriptionNotFound(c *gin.Context) {
	c.String(http.StatusNotFound, "未找到对应的订阅")
}

func subscriptionBadRequest(c *gin.Context, message string) {
	c.String(http.StatusBadRequest, message)
}

func subscriptionSaveError(c *gin.Context, action string, err error) {
	if err == nil {
		c.String(http.StatusInternalServerError, action+"失败")
		return
	}
	c.String(http.StatusInternalServerError, action+"失败: "+err.Error())
}

func subscriptionReloadError(c *gin.Context, err error) {
	subscriptionSaveError(c, "刷新订阅卡片", err)
}

func subscriptionJSONBadRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, gin.H{"error": message})
}

func subscriptionJSONServerError(c *gin.Context, action string, err error) {
	message := action + "失败"
	if err != nil {
		message += ": " + err.Error()
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": message})
}

type SubscriptionsData struct {
	SkipLayout      bool
	Subscriptions   []model.Subscription
	SchedulerStatus scheduler.RunStatus
	TrendReport     SubscriptionTrendReport
}

type SubscriptionHistoryData struct {
	Subscription model.Subscription
	Runs         []model.SubscriptionRunLog
	Logs         []model.DownloadLog
}

type SubscriptionTrendReport struct {
	WindowLabel                string
	CheckedCount               int
	SuccessCount               int
	WarningCount               int
	ErrorCount                 int
	DownloadLogCount           int64
	CompletedCount             int64
	RecentNewDownloads         int
	ActiveIssueCount           int
	TopIssueSubscriptions      []SubscriptionTrendItem
	RecentWinningSubscriptions []SubscriptionTrendItem
}

type SubscriptionTrendItem struct {
	ID               uint
	Title            string
	Status           string
	StatusLabel      string
	LastRunSummary   string
	LastError        string
	LastNewDownloads int
	LastCheckLabel   string
}

var runSubscriptionCheck = func(sub *model.Subscription, source string) error {
	if sub == nil {
		return fmt.Errorf("subscription is nil")
	}

	qbCfg := qbutil.LoadConfig()
	if qbutil.ManagedBinaryMissing(qbCfg, config.BinDir()) {
		return fmt.Errorf("未检测到 qBittorrent。请先安装托管版本，或在设置中填写外部 qBittorrent WebUI 地址")
	}
	if qbutil.MissingExternalURL(qbCfg) {
		return fmt.Errorf("已启用外部 qBittorrent 模式，但 WebUI 地址还是空的")
	}

	qbt := downloader.NewQBittorrentClient(qbCfg.URL)
	if err := qbt.Login(qbCfg.Username, qbCfg.Password); err != nil {
		return err
	}

	mgr := service.NewSubscriptionManager(qbt)
	mgr.ProcessSubscriptionWithSource(sub, source)
	return nil
}

func SubscriptionsHandler(c *gin.Context) {
	skip := IsHTMX(c)
	var subs []model.Subscription
	if err := db.DB.Preload("Metadata").Find(&subs).Error; err != nil {
		log.Printf("Error fetching subscriptions: %v", err)
	}

	populateSubscriptionStats(subs)

	data := SubscriptionsData{
		SkipLayout:      skip,
		Subscriptions:   subs,
		SchedulerStatus: scheduler.GlobalRunStatus.Snapshot(),
		TrendReport:     loadSubscriptionTrendReport(7),
	}
	c.HTML(http.StatusOK, "subscriptions.html", data)
}

func CreateSubscriptionHandler(c *gin.Context) {
	var sub model.Subscription
	if err := c.ShouldBind(&sub); err != nil {
		c.String(http.StatusBadRequest, "提交的数据格式不正确: "+err.Error())
		return
	}

	if err := createSubscriptionInternal(&sub); err != nil {
		if err.Error() == "exists" {
			c.String(http.StatusConflict, "该 RSS 订阅已经存在")
		} else {
			subscriptionSaveError(c, "创建订阅", err)
		}
		return
	}

	c.Header("HX-Redirect", "/subscriptions")
	c.Status(http.StatusOK)
}

func createSubscriptionInternal(sub *model.Subscription) error {
	if sub.Metadata == nil {
		sub.Metadata = &model.AnimeMetadata{}
	}
	if sub.Metadata.Title == "" {
		sub.Metadata.Title = parser.CleanTitle(sub.Title)
	}

	// Try to extract BangumiID from RSS URL
	if sub.RSSUrl != "" {
		if u, err := url.Parse(sub.RSSUrl); err == nil {
			q := u.Query()
			if bidStr := q.Get("bangumiId"); bidStr != "" {
				if bid, err := strconv.Atoi(bidStr); err == nil {
					sub.Metadata.BangumiID = bid
				}
			}
		}
	}

	// Auto-fill FilterRule from SubtitleGroup if FilterRule is empty
	if sub.FilterRule == "" && sub.SubtitleGroup != "" {
		sub.FilterRule = sub.SubtitleGroup
	}

	// Enrich Metadata (Bangumi & TMDB)
	metaSvc := service.NewMetadataService()
	metaSvc.EnrichMetadata(sub.Metadata, sub.Title)

	sub.IsActive = true

	// Check if already exists (including soft-deleted)
	var existing model.Subscription
	if err := db.DB.Unscoped().Where("rss_url = ?", sub.RSSUrl).First(&existing).Error; err == nil {
		// Found existing
		if existing.DeletedAt.Valid {
			// Restore it
			existing.DeletedAt = gorm.DeletedAt{} // Restore
			existing.Title = sub.Title
			existing.FilterRule = sub.FilterRule
			existing.ExcludeRule = sub.ExcludeRule
			existing.IsActive = true
			if err := db.DB.Save(&existing).Error; err != nil {
				return fmt.Errorf("failed to restore: %v", err)
			}
			*sub = existing // Update caller's pointer
		} else {
			return fmt.Errorf("exists")
		}
	} else {
		// New creation
		if err := db.DB.Create(sub).Error; err != nil {
			return fmt.Errorf("failed to create: %v", err)
		}
	}

	// Trigger run asynchronously
	go func() {
		log.Printf("DEBUG: Async ProcessSubscription started for %s", sub.Title)
		if err := runSubscriptionCheck(sub, "create"); err != nil {
			log.Printf("WARN: Skipping async subscription run for %s: %v", sub.Title, err)
		}
	}()

	return nil
}

func CreateBatchSubscriptionHandler(c *gin.Context) {
	var subs []model.Subscription
	if err := c.ShouldBindJSON(&subs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "批量导入的数据格式不正确"})
		return
	}

	added := 0
	failed := 0
	for i := range subs {
		if err := createSubscriptionInternal(&subs[i]); err != nil {
			log.Printf("Batch add failed for %s: %v", subs[i].Title, err)
			failed++
		} else {
			added++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "批量处理完成",
		"added":   added,
		"failed":  failed,
	})
}

// BatchPreviewHandler 并发地为批量添加提供预览
func BatchPreviewHandler(c *gin.Context) {
	var subs []model.Subscription
	if err := c.ShouldBindJSON(&subs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "批量预览的数据格式不正确"})
		return
	}

	type PreviewResult struct {
		Title    string           `json:"title"`
		RSSUrl   string           `json:"rss_url"`
		Episodes []parser.Episode `json:"episodes,omitempty"`
		Error    string           `json:"error,omitempty"`
	}

	results := make([]PreviewResult, len(subs))
	var wg sync.WaitGroup

	for i, sub := range subs {
		wg.Add(1)
		go func(i int, s model.Subscription) {
			defer wg.Done()
			res := PreviewResult{Title: s.Title, RSSUrl: s.RSSUrl}

			if s.RSSUrl == "" {
				res.Error = "缺少 RSS 链接"
			} else {
				p := parser.NewMikanParser()
				eps, err := p.Parse(s.RSSUrl)
				if err != nil {
					res.Error = err.Error()
				} else {
					// 只取前 5 集预览
					if len(eps) > 5 {
						eps = eps[:5]
					}
					res.Episodes = eps
				}
			}
			results[i] = res
		}(i, sub)
	}

	wg.Wait()
	c.JSON(http.StatusOK, results)
}

func ToggleSubscriptionHandler(c *gin.Context) {
	id := c.Param("id")
	var sub model.Subscription
	if err := db.DB.First(&sub, id).Error; err != nil {
		subscriptionNotFound(c)
		return
	}

	sub.IsActive = !sub.IsActive
	if err := db.DB.Save(&sub).Error; err != nil {
		subscriptionSaveError(c, "更新订阅状态", err)
		return
	}

	cardSub, err := loadSubscriptionCard(sub.ID)
	if err != nil {
		subscriptionReloadError(c, err)
		return
	}

	c.HTML(http.StatusOK, "subscription_card.html", cardSub)
}

func DeleteSubscriptionHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		subscriptionBadRequest(c, "订阅 ID 无效")
		return
	}

	log.Printf("DEBUG: Deleting subscription ID: %d", id)

	// Start transaction for cascading delete
	tx := db.DB.Begin()

	// 1. Delete associated logs (Hard Delete)
	if err := tx.Unscoped().Where("subscription_id = ?", id).Delete(&model.DownloadLog{}).Error; err != nil {
		tx.Rollback()
		log.Printf("ERROR: Delete logs failed for subID %d: %v", id, err)
		subscriptionSaveError(c, "删除关联日志", err)
		return
	}

	// 2. Delete subscription (Hard Delete)
	if err := tx.Unscoped().Delete(&model.Subscription{}, id).Error; err != nil {
		tx.Rollback()
		log.Printf("ERROR: Delete sub failed for ID %d: %v", id, err)
		subscriptionSaveError(c, "删除订阅", err)
		return
	}

	if err := tx.Commit().Error; err != nil {
		log.Printf("ERROR: Commit failed: %v", err)
		subscriptionSaveError(c, "提交删除操作", err)
		return
	}
	c.Status(http.StatusOK)
}

func RunSubscriptionHandler(c *gin.Context) {
	id := c.Param("id")
	var sub model.Subscription
	if err := db.DB.First(&sub, id).Error; err != nil {
		subscriptionNotFound(c)
		return
	}

	if err := runSubscriptionCheck(&sub, "manual"); err != nil {
		log.Printf("RunSubscription: QB Login failed: %v", err)
		subscriptionSaveError(c, "立即检查订阅", fmt.Errorf("QBittorrent 连接失败: %w", err))
		return
	}

	cardSub, err := loadSubscriptionCard(sub.ID)
	if err != nil {
		subscriptionReloadError(c, err)
		return
	}

	c.HTML(http.StatusOK, "subscription_card.html", cardSub)
}

func GetSubscriptionCardHandler(c *gin.Context) {
	idStr := c.Param("id")
	idUint64, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		subscriptionBadRequest(c, "订阅 ID 无效")
		return
	}

	sub, err := loadSubscriptionCard(uint(idUint64))
	if err != nil {
		subscriptionNotFound(c)
		return
	}

	c.HTML(http.StatusOK, "subscription_card.html", sub)
}

func GetSchedulerStatusHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "scheduler_status.html", scheduler.GlobalRunStatus.Snapshot())
}

func GetSubscriptionTrendsHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "subscription_trends.html", loadSubscriptionTrendReport(7))
}

func GetSubscriptionHistoryHandler(c *gin.Context) {
	idStr := c.Param("id")
	idUint64, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		subscriptionBadRequest(c, "订阅 ID 无效")
		return
	}

	data, err := loadSubscriptionHistory(uint(idUint64))
	if err != nil {
		subscriptionNotFound(c)
		return
	}

	c.HTML(http.StatusOK, "subscription_history.html", data)
}

func UpdateSubscriptionHandler(c *gin.Context) {
	id := c.Param("id")
	var sub model.Subscription
	if err := db.DB.First(&sub, id).Error; err != nil {
		subscriptionNotFound(c)
		return
	}

	var input struct {
		Title       string `form:"Title" binding:"required"`
		RSSUrl      string `form:"RSSUrl" binding:"required"`
		FilterRule  string `form:"FilterRule"`
		ExcludeRule string `form:"ExcludeRule"`
	}

	if err := c.ShouldBind(&input); err != nil {
		subscriptionBadRequest(c, "提交的数据格式不正确")
		return
	}

	// Check if Title changed, reset metadata ID search if so
	if sub.Title != input.Title {
		sub.Title = input.Title
		// Re-clean title just in case
		// Reset ID and try to find again immediately
		if sub.Metadata != nil {
			sub.Metadata.BangumiID = 0
			bgmClient := bangumi.NewClient("", "", "")
			if res, err := bgmClient.SearchSubject(sub.Title); err == nil && res != nil {
				sub.Metadata.BangumiID = res.ID
			}
		}
	}

	sub.RSSUrl = input.RSSUrl
	sub.FilterRule = input.FilterRule
	sub.ExcludeRule = input.ExcludeRule

	if err := db.DB.Save(&sub).Error; err != nil {
		subscriptionSaveError(c, "保存订阅", err)
		return
	}

	populateSubscriptionStat(&sub)

	cardSub, err := loadSubscriptionCard(sub.ID)
	if err != nil {
		subscriptionReloadError(c, err)
		return
	}

	c.HTML(http.StatusOK, "subscription_card.html", cardSub)
}

func UseBaseRSSHandler(c *gin.Context) {
	id := c.Param("id")
	var sub model.Subscription
	if err := db.DB.First(&sub, id).Error; err != nil {
		subscriptionNotFound(c)
		return
	}

	baseRSS, ok := deriveBaseRSSURL(sub.RSSUrl)
	if !ok {
		subscriptionBadRequest(c, "当前订阅已经是主 RSS")
		return
	}

	if err := useBaseRSSAndRecheck(&sub, baseRSS); err != nil {
		subscriptionSaveError(c, "执行主 RSS 修复", err)
		return
	}

	populateSubscriptionStat(&sub)
	cardSub, err := loadSubscriptionCard(sub.ID)
	if err != nil {
		subscriptionReloadError(c, err)
		return
	}

	triggerAppToast(c, repairSuccessToast(repairActionUseBaseRSS), "success")
	c.HTML(http.StatusOK, "subscription_card.html", cardSub)
}

func ClearSubscriptionFilterHandler(c *gin.Context) {
	id := c.Param("id")
	var sub model.Subscription
	if err := db.DB.First(&sub, id).Error; err != nil {
		subscriptionNotFound(c)
		return
	}

	if err := clearFilterAndRecheck(&sub); err != nil {
		subscriptionSaveError(c, "清空过滤规则", err)
		return
	}

	populateSubscriptionStat(&sub)
	cardSub, err := loadSubscriptionCard(sub.ID)
	if err != nil {
		subscriptionReloadError(c, err)
		return
	}

	triggerAppToast(c, repairSuccessToast(repairActionClearFilter), "success")
	c.HTML(http.StatusOK, "subscription_card.html", cardSub)
}

func ResetSubscriptionLogsHandler(c *gin.Context) {
	id := c.Param("id")
	var sub model.Subscription
	if err := db.DB.First(&sub, id).Error; err != nil {
		subscriptionNotFound(c)
		return
	}

	if err := resetStaleLogsAndRecheck(&sub, staleLogResetAge); err != nil {
		subscriptionSaveError(c, "清理阻塞下载记录", err)
		return
	}

	populateSubscriptionStat(&sub)
	cardSub, err := loadSubscriptionCard(sub.ID)
	if err != nil {
		subscriptionReloadError(c, err)
		return
	}

	triggerAppToast(c, repairSuccessToast(repairActionResetStaleLog), "success")
	c.HTML(http.StatusOK, "subscription_card.html", cardSub)
}

func RefreshSubscriptionMetadataHandler(c *gin.Context) {
	id := c.Param("id")
	var sub model.Subscription
	if err := db.DB.First(&sub, id).Error; err != nil {
		subscriptionNotFound(c)
		return
	}

	// Enrich Metadata using shared service
	metaSvc := service.NewMetadataService()
	metaSvc.EnrichMetadata(sub.Metadata, sub.Title)

	if err := db.DB.Save(&sub).Error; err != nil {
		subscriptionSaveError(c, "刷新订阅元数据", err)
		return
	}

	cardSub, err := loadSubscriptionCard(sub.ID)
	if err != nil {
		subscriptionReloadError(c, err)
		return
	}

	c.HTML(http.StatusOK, "subscription_card.html", cardSub)
}

// SwitchSubscriptionSourceHandler 切换订阅的数据源并全局同步
func SwitchSubscriptionSourceHandler(c *gin.Context) {
	id := c.Param("id")
	source := c.Query("source")

	var sub model.Subscription
	if err := db.DB.Preload("Metadata").First(&sub, id).Error; err != nil {
		subscriptionNotFound(c)
		return
	}

	if sub.Metadata == nil {
		subscriptionBadRequest(c, "当前订阅还没有关联元数据")
		return
	}

	m := sub.Metadata
	switch source {
	case SourceTMDB:
		if m.TMDBID != 0 {
			m.Title = m.TMDBTitle
			m.Image = m.TMDBImage
			m.Summary = m.TMDBSummary
		}
	case SourceBangumi:
		if m.BangumiID != 0 {
			m.Title = m.BangumiTitle
			m.Image = m.BangumiImage
			m.Summary = m.BangumiSummary
		}
	case SourceAniList:
		if m.AniListID != 0 {
			m.Title = m.AniListTitle
			m.Image = m.AniListImage
			m.Summary = m.AniListSummary
		}
	}

	if err := db.DB.Save(m).Error; err != nil {
		subscriptionSaveError(c, "切换数据源", err)
		return
	}

	// Trigger global sync (updates the subscription itself + any local folders)
	metaSvc := service.NewMetadataService()
	metaSvc.SyncMetadataToModels(m)

	cardSub, err := loadSubscriptionCard(sub.ID)
	if err != nil {
		subscriptionReloadError(c, err)
		return
	}

	c.HTML(http.StatusOK, "subscription_card.html", cardSub)
}

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
		// Be nice to the API
		time.Sleep(200 * time.Millisecond)
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("刷新完成，更新了 %d 个订阅的元数据", updatedCount),
		"updated": updatedCount,
		"total":   len(subs),
	})
}
