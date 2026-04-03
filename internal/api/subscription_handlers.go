package api

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
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

type SubscriptionsData struct {
	SkipLayout      bool
	Subscriptions   []model.Subscription
	SchedulerStatus scheduler.RunStatus
	TrendReport     SubscriptionTrendReport
}

type SubscriptionHistoryData struct {
	Subscription model.Subscription
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
		c.String(http.StatusBadRequest, "Invalid Data: %v", err)
		return
	}

	if err := createSubscriptionInternal(&sub); err != nil {
		if err.Error() == "exists" {
			c.String(http.StatusConflict, "Subscription with this RSS URL already exists")
		} else {
			c.String(http.StatusInternalServerError, err.Error())
		}
		return
	}

	// User requested "silky smooth" transition, wait 1s
	time.Sleep(1 * time.Second)

	c.Header("HX-Redirect", "/subscriptions")
	c.Status(http.StatusOK)
}

func createSubscriptionInternal(sub *model.Subscription) error {
	// Try to extract BangumiID from RSS URL
	if sub.RSSUrl != "" {
		if u, err := url.Parse(sub.RSSUrl); err == nil {
			q := u.Query()
			if bidStr := q.Get("bangumiId"); bidStr != "" {
				if bid, err := strconv.Atoi(bidStr); err == nil {
					if sub.Metadata == nil {
						sub.Metadata = &model.AnimeMetadata{}
					}
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
		qbCfg := qbutil.LoadConfig()
		if qbutil.ManagedBinaryMissing(qbCfg, config.BinDir()) {
			log.Printf("WARN: Skipping async subscription run for %s because qBittorrent is not installed and no external WebUI is configured", sub.Title)
			return
		}
		if qbutil.MissingExternalURL(qbCfg) {
			log.Printf("WARN: Skipping async subscription run for %s because external qBittorrent mode has no WebUI URL configured", sub.Title)
			return
		}

		qbt := downloader.NewQBittorrentClient(qbCfg.URL)
		if err := qbt.Login(qbCfg.Username, qbCfg.Password); err != nil {
			log.Printf("ERROR: Async QB Login failed: %v", err)
			return
		}
		mgr := service.NewSubscriptionManager(qbt)
		mgr.ProcessSubscription(sub)
	}()

	return nil
}

func CreateBatchSubscriptionHandler(c *gin.Context) {
	var subs []model.Subscription
	if err := c.ShouldBindJSON(&subs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON format"})
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
		"message": "Batch process completed",
		"added":   added,
		"failed":  failed,
	})
}

// BatchPreviewHandler 并发地为批量添加提供预览
func BatchPreviewHandler(c *gin.Context) {
	var subs []model.Subscription
	if err := c.ShouldBindJSON(&subs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Data"})
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
		c.String(http.StatusNotFound, "Not Found")
		return
	}

	sub.IsActive = !sub.IsActive
	if err := db.DB.Save(&sub).Error; err != nil {
		c.String(http.StatusInternalServerError, "Failed to update subscription: "+err.Error())
		return
	}

	time.Sleep(300 * time.Millisecond)

	cardSub, err := loadSubscriptionCard(sub.ID)
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to reload subscription card: "+err.Error())
		return
	}

	c.HTML(http.StatusOK, "subscription_card.html", cardSub)
}

func DeleteSubscriptionHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.String(http.StatusBadRequest, "无效的 ID")
		return
	}

	log.Printf("DEBUG: Deleting subscription ID: %d", id)

	// Start transaction for cascading delete
	tx := db.DB.Begin()

	// 1. Delete associated logs (Hard Delete)
	if err := tx.Unscoped().Where("subscription_id = ?", id).Delete(&model.DownloadLog{}).Error; err != nil {
		tx.Rollback()
		log.Printf("ERROR: Delete logs failed for subID %d: %v", id, err)
		c.String(http.StatusInternalServerError, "删除关联日志失败: "+err.Error())
		return
	}

	// 2. Delete subscription (Hard Delete)
	if err := tx.Unscoped().Delete(&model.Subscription{}, id).Error; err != nil {
		tx.Rollback()
		log.Printf("ERROR: Delete sub failed for ID %d: %v", id, err)
		c.String(http.StatusInternalServerError, "删除订阅失败: "+err.Error())
		return
	}

	if err := tx.Commit().Error; err != nil {
		log.Printf("ERROR: Commit failed: %v", err)
		c.String(http.StatusInternalServerError, "事务提交失败")
		return
	}

	time.Sleep(200 * time.Millisecond) // Slight delay for UI feel
	c.Status(http.StatusOK)
}

func RunSubscriptionHandler(c *gin.Context) {
	id := c.Param("id")
	var sub model.Subscription
	if err := db.DB.First(&sub, id).Error; err != nil {
		c.String(http.StatusNotFound, "Not Found")
		return
	}

	// Fetch QB Config
	qbCfg := qbutil.LoadConfig()
	if qbutil.ManagedBinaryMissing(qbCfg, config.BinDir()) {
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, `<script>alert("未检测到 qBittorrent。请先安装托管版本，或在设置中填写外部 qBittorrent WebUI 地址。")</script>`)
		return
	}
	if qbutil.MissingExternalURL(qbCfg) {
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, `<script>alert("External qBittorrent mode is enabled, but the WebUI URL is still empty.")</script>`)
		return
	}

	// Init Downloader
	qbt := downloader.NewQBittorrentClient(qbCfg.URL)
	if err := qbt.Login(qbCfg.Username, qbCfg.Password); err != nil {
		log.Printf("RunSubscription: QB Login failed: %v", err)
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, `<script>alert("QBittorrent 连接失败: `+err.Error()+`")</script>`)
		return
	}

	// Init Manager and Run
	mgr := service.NewSubscriptionManager(qbt)
	// Let's do Sync for now as it shouldn't be too long for RSS parse.
	mgr.ProcessSubscription(&sub)

	time.Sleep(1 * time.Second) // Smooth transition
	cardSub, err := loadSubscriptionCard(sub.ID)
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to reload subscription card: "+err.Error())
		return
	}

	c.HTML(http.StatusOK, "subscription_card.html", cardSub)
}

func GetSubscriptionCardHandler(c *gin.Context) {
	idStr := c.Param("id")
	idUint64, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.String(http.StatusBadRequest, "Invalid subscription ID")
		return
	}

	sub, err := loadSubscriptionCard(uint(idUint64))
	if err != nil {
		c.String(http.StatusNotFound, "Not Found")
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
		c.String(http.StatusBadRequest, "Invalid subscription ID")
		return
	}

	data, err := loadSubscriptionHistory(uint(idUint64))
	if err != nil {
		c.String(http.StatusNotFound, "Not Found")
		return
	}

	c.HTML(http.StatusOK, "subscription_history.html", data)
}

func UpdateSubscriptionHandler(c *gin.Context) {
	id := c.Param("id")
	var sub model.Subscription
	if err := db.DB.First(&sub, id).Error; err != nil {
		c.String(http.StatusNotFound, "Not Found")
		return
	}

	var input struct {
		Title       string `form:"Title" binding:"required"`
		RSSUrl      string `form:"RSSUrl" binding:"required"`
		FilterRule  string `form:"FilterRule"`
		ExcludeRule string `form:"ExcludeRule"`
	}

	if err := c.ShouldBind(&input); err != nil {
		c.String(http.StatusBadRequest, "Invalid Data")
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
		c.String(http.StatusInternalServerError, "Error saving: "+err.Error())
		return
	}

	populateSubscriptionStat(&sub)

	// Sleep for smooth UI feel
	time.Sleep(500 * time.Millisecond)

	cardSub, err := loadSubscriptionCard(sub.ID)
	if err != nil {
		c.String(http.StatusInternalServerError, "Error loading updated card: "+err.Error())
		return
	}

	c.HTML(http.StatusOK, "subscription_card.html", cardSub)
}

func RefreshSubscriptionMetadataHandler(c *gin.Context) {
	id := c.Param("id")
	var sub model.Subscription
	if err := db.DB.First(&sub, id).Error; err != nil {
		c.String(http.StatusNotFound, "Not Found")
		return
	}

	// Enrich Metadata using shared service
	metaSvc := service.NewMetadataService()
	metaSvc.EnrichMetadata(sub.Metadata, sub.Title)

	if err := db.DB.Save(&sub).Error; err != nil {
		c.String(http.StatusInternalServerError, "Failed to save: "+err.Error())
		return
	}

	// Sleep for smooth UI feel
	time.Sleep(500 * time.Millisecond)

	cardSub, err := loadSubscriptionCard(sub.ID)
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to reload card: "+err.Error())
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
		c.String(http.StatusNotFound, "Not Found")
		return
	}

	if sub.Metadata == nil {
		c.String(http.StatusBadRequest, "No metadata associated")
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
		c.String(http.StatusInternalServerError, "Failed to save metadata")
		return
	}

	// Trigger global sync (updates the subscription itself + any local folders)
	metaSvc := service.NewMetadataService()
	metaSvc.SyncMetadataToModels(m)

	cardSub, err := loadSubscriptionCard(sub.ID)
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to reload card: "+err.Error())
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
}

func loadSubscriptionCard(id uint) (model.Subscription, error) {
	var sub model.Subscription
	if err := db.DB.Preload("Metadata").First(&sub, id).Error; err != nil {
		return model.Subscription{}, err
	}

	populateSubscriptionStat(&sub)
	return sub, nil
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

	return SubscriptionHistoryData{
		Subscription: sub,
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	p := parser.NewMikanParser()
	subgroups, err := p.GetSubgroups(bangumiID)
	if err != nil {
		log.Printf("GetSubgroups error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, dashboard)
}

func RefreshSubscriptionsHandler(c *gin.Context) {
	var subs []model.Subscription
	if err := db.DB.Preload("Metadata").Find(&subs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch subscriptions"})
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
