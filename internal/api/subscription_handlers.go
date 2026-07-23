package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
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
	WindowLabel                string                  `json:"window_label"`
	CheckedCount               int                     `json:"checked_count"`
	SuccessCount               int                     `json:"success_count"`
	WarningCount               int                     `json:"warning_count"`
	ErrorCount                 int                     `json:"error_count"`
	DownloadLogCount           int64                   `json:"download_log_count"`
	CompletedCount             int64                   `json:"completed_count"`
	RecentNewDownloads         int                     `json:"recent_new_downloads"`
	ActiveIssueCount           int                     `json:"active_issue_count"`
	TopIssueSubscriptions      []SubscriptionTrendItem `json:"top_issue_subscriptions"`
	RecentWinningSubscriptions []SubscriptionTrendItem `json:"recent_winning_subscriptions"`
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

type RSSValidationResponse struct {
	PrimaryCount    int      `json:"primary_count"`
	BackupCount     int      `json:"backup_count,omitempty"`
	MatchingCount   int      `json:"matching_count"`
	Warnings        []string `json:"warnings,omitempty"`
	PreviewTitles   []string `json:"preview_titles,omitempty"`
	UsingBackupHint string   `json:"using_backup_hint,omitempty"`
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

var enrichSubscriptionMetadata = func(metadata *model.AnimeMetadata, title string) {
	service.NewMetadataService().EnrichMetadata(metadata, title)
}

func SubscriptionsHandler(c *gin.Context) {
	skip := IsHTMX(c)
	subs, err := listSubscriptionsWithMetadata()
	if err != nil {
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
	normalizeMikanAssociation(sub)
	normalizeSubscriptionStrategy(sub)

	if sub.Metadata == nil {
		sub.Metadata = &model.AnimeMetadata{}
	}
	if sub.Metadata.Title == "" {
		sub.Metadata.Title = parser.CleanTitle(sub.Title)
	}

	// Auto-fill FilterRule from SubtitleGroup if FilterRule is empty
	if sub.FilterRule == "" && sub.SubtitleGroup != "" && !sub.AllowMultiSubgroup {
		sub.FilterRule = sub.SubtitleGroup
	}

	// Enrich Metadata (Bangumi & TMDB)
	enrichSubscriptionMetadata(sub.Metadata, sub.Title)

	sub.IsActive = true

	// Check if already exists (including soft-deleted)
	var existing model.Subscription
	if err := db.DB.Unscoped().Where("rss_url = ?", sub.RSSUrl).First(&existing).Error; err == nil {
		// Found existing
		if existing.DeletedAt.Valid {
			// Restore it
			existing.DeletedAt = gorm.DeletedAt{} // Restore
			existing.Title = sub.Title
			existing.MikanID = sub.MikanID
			existing.Image = sub.Image
			existing.SubtitleGroup = sub.SubtitleGroup
			existing.Season = sub.Season
			existing.FilterRule = sub.FilterRule
			existing.ExcludeRule = sub.ExcludeRule
			existing.BackupRSSUrl = sub.BackupRSSUrl
			existing.ExpectedEpisodes = sub.ExpectedEpisodes
			existing.AutoDisableOnDone = sub.AutoDisableOnDone
			existing.AllowMultiSubgroup = sub.AllowMultiSubgroup
			existing.StaleAfterHours = sub.StaleAfterHours
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

func normalizeMikanAssociation(sub *model.Subscription) {
	if sub == nil {
		return
	}
	sub.MikanID = strings.TrimSpace(sub.MikanID)
	sub.SubtitleGroup = strings.TrimSpace(sub.SubtitleGroup)
	sub.Image = strings.TrimSpace(sub.Image)
	sub.Season = strings.TrimSpace(sub.Season)
	if sub.MikanID == "" {
		if mikanID, ok := parser.MikanIDFromRSSURL(sub.RSSUrl); ok {
			sub.MikanID = mikanID
		}
	}
}

func normalizeSubscriptionStrategy(sub *model.Subscription) {
	if sub == nil {
		return
	}
	if strings.TrimSpace(sub.BackupRSSUrl) == "" {
		if baseRSS, ok := deriveBaseRSSURL(sub.RSSUrl); ok {
			sub.BackupRSSUrl = baseRSS
		}
	}
	sub.BackupRSSUrl = strings.TrimSpace(sub.BackupRSSUrl)
	if sub.ExpectedEpisodes < 0 {
		sub.ExpectedEpisodes = 0
	}
	if sub.StaleAfterHours < 0 {
		sub.StaleAfterHours = 0
	}
	if sub.StaleAfterHours == 0 {
		sub.StaleAfterHours = 168
	}
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
	sub, err := subscriptionByID(id)
	if err != nil {
		subscriptionNotFound(c)
		return
	}

	sub.IsActive = !sub.IsActive
	if err := saveSubscription(sub); err != nil {
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

	auditCtx := buildAuditContext(c)
	var subTitle string
	var existing model.Subscription
	if err := db.DB.Unscoped().First(&existing, id).Error; err == nil {
		subTitle = existing.Title
	}

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
	service.RecordAudit(auditCtx, service.AuditEntry{
		Action:     service.AuditActionSubscriptionDelete,
		Outcome:    service.AuditOutcomeSuccess,
		TargetType: "subscription",
		TargetID:   idStr,
		Details:    map[string]string{"title": subTitle},
	})
	c.Status(http.StatusOK)
}

func RunSubscriptionHandler(c *gin.Context) {
	id := c.Param("id")
	sub, err := subscriptionByID(id)
	if err != nil {
		subscriptionNotFound(c)
		return
	}

	if err := runSubscriptionCheck(sub, "manual"); err != nil {
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
	sub, err := subscriptionByID(id)
	if err != nil {
		subscriptionNotFound(c)
		return
	}

	var input struct {
		Title              string `form:"Title" binding:"required"`
		RSSUrl             string `form:"RSSUrl" binding:"required"`
		FilterRule         string `form:"FilterRule"`
		ExcludeRule        string `form:"ExcludeRule"`
		BackupRSSUrl       string `form:"BackupRSSUrl"`
		ExpectedEpisodes   int    `form:"ExpectedEpisodes"`
		AllowMultiSubgroup bool   `form:"AllowMultiSubgroup"`
		AutoDisableOnDone  bool   `form:"AutoDisableOnDone"`
		StaleAfterHours    int    `form:"StaleAfterHours"`
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
	sub.BackupRSSUrl = input.BackupRSSUrl
	sub.ExpectedEpisodes = input.ExpectedEpisodes
	sub.AllowMultiSubgroup = input.AllowMultiSubgroup
	sub.AutoDisableOnDone = input.AutoDisableOnDone
	sub.StaleAfterHours = input.StaleAfterHours
	normalizeSubscriptionStrategy(sub)

	if err := saveSubscription(sub); err != nil {
		subscriptionSaveError(c, "保存订阅", err)
		return
	}

	populateSubscriptionStat(sub)

	cardSub, err := loadSubscriptionCard(sub.ID)
	if err != nil {
		subscriptionReloadError(c, err)
		return
	}

	c.HTML(http.StatusOK, "subscription_card.html", cardSub)
}

func ValidateSubscriptionRSSHandler(c *gin.Context) {
	sub := model.Subscription{
		Title:              strings.TrimSpace(c.Query("title")),
		RSSUrl:             strings.TrimSpace(c.Query("rss")),
		BackupRSSUrl:       strings.TrimSpace(c.Query("backup_rss")),
		FilterRule:         strings.TrimSpace(c.Query("filter")),
		ExcludeRule:        strings.TrimSpace(c.Query("exclude")),
		SubtitleGroup:      strings.TrimSpace(c.Query("subtitle_group")),
		AllowMultiSubgroup: c.Query("allow_multi_subgroup") == ValueTrue,
	}

	if sub.RSSUrl == "" {
		subscriptionJSONBadRequest(c, "请先输入 RSS 链接")
		return
	}

	parserClient := parser.NewMikanParser()
	primaryEpisodes, primaryErr := parserClient.ParseContext(context.Background(), sub.RSSUrl)
	response := RSSValidationResponse{}
	if primaryErr == nil {
		response.PrimaryCount = len(primaryEpisodes)
		response.PreviewTitles = previewEpisodeTitles(primaryEpisodes, 5)
	}

	rules := service.BuildSubscriptionRuleSetForValidation(&sub)
	if primaryErr == nil {
		for _, ep := range primaryEpisodes {
			if rules.Allows(ep) {
				response.MatchingCount++
			}
		}
	}

	if primaryErr != nil {
		response.Warnings = append(response.Warnings, "主 RSS 当前无法解析，请检查地址或稍后再试。")
	}
	if primaryErr == nil && len(primaryEpisodes) == 0 {
		response.Warnings = append(response.Warnings, "主 RSS 当前为空，可能是该字幕组暂时还没有可用资源。")
	}
	if primaryErr == nil && len(primaryEpisodes) > 0 && response.MatchingCount == 0 && (sub.FilterRule != "" || sub.ExcludeRule != "") {
		response.Warnings = append(response.Warnings, "当前过滤/排除规则会把本次 RSS 结果全部筛空。")
	}

	if sub.BackupRSSUrl != "" && sub.BackupRSSUrl != sub.RSSUrl {
		backupEpisodes, backupErr := parserClient.ParseContext(context.Background(), sub.BackupRSSUrl)
		if backupErr != nil {
			response.Warnings = append(response.Warnings, "备用 RSS 当前也不可用。")
		} else {
			response.BackupCount = len(backupEpisodes)
			if len(backupEpisodes) > 0 {
				response.UsingBackupHint = "如果主 RSS 后续失效，系统可以自动回退到备用 RSS。"
			}
		}
	}

	if sub.AllowMultiSubgroup {
		response.Warnings = append(response.Warnings, "已启用多字幕组共存：不会自动把过滤规则锁定到单一字幕组。")
	}

	if primaryErr != nil {
		c.JSON(http.StatusBadGateway, response)
		return
	}
	c.JSON(http.StatusOK, response)
}

func previewEpisodeTitles(episodes []parser.Episode, limit int) []string {
	if limit <= 0 || len(episodes) == 0 {
		return nil
	}
	if len(episodes) < limit {
		limit = len(episodes)
	}
	titles := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		if title := strings.TrimSpace(episodes[i].Title); title != "" {
			titles = append(titles, title)
		}
	}
	return titles
}

func UseBaseRSSHandler(c *gin.Context) {
	id := c.Param("id")
	sub, err := subscriptionByID(id)
	if err != nil {
		subscriptionNotFound(c)
		return
	}

	baseRSS, ok := deriveBaseRSSURL(sub.RSSUrl)
	if !ok {
		subscriptionBadRequest(c, "当前订阅已经是主 RSS")
		return
	}

	if err := useBaseRSSAndRecheck(sub, baseRSS); err != nil {
		subscriptionSaveError(c, "执行主 RSS 修复", err)
		return
	}

	populateSubscriptionStat(sub)
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
	sub, err := subscriptionByID(id)
	if err != nil {
		subscriptionNotFound(c)
		return
	}

	if err := clearFilterAndRecheck(sub); err != nil {
		subscriptionSaveError(c, "清空过滤规则", err)
		return
	}

	populateSubscriptionStat(sub)
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
	sub, err := subscriptionByID(id)
	if err != nil {
		subscriptionNotFound(c)
		return
	}

	if err := resetStaleLogsAndRecheck(sub, staleLogResetAge); err != nil {
		subscriptionSaveError(c, "清理阻塞下载记录", err)
		return
	}

	populateSubscriptionStat(sub)
	cardSub, err := loadSubscriptionCard(sub.ID)
	if err != nil {
		subscriptionReloadError(c, err)
		return
	}

	triggerAppToast(c, repairSuccessToast(repairActionResetStaleLog), "success")
	c.HTML(http.StatusOK, "subscription_card.html", cardSub)
}

func RetryMissingEpisodesHandler(c *gin.Context) {
	recheckSubscriptionRepairAction(c, repairActionRetryMissing, "执行缺集重检")
}

func RecheckStaleSubscriptionHandler(c *gin.Context) {
	recheckSubscriptionRepairAction(c, repairActionRetryStale, "重新检查长期无进展的订阅")
}

func RetryUpgradeSubscriptionHandler(c *gin.Context) {
	recheckSubscriptionRepairAction(c, repairActionRetryUpgrade, "执行洗版检查")
}

func RefreshSubscriptionLibraryHandler(c *gin.Context) {
	id := c.Param("id")
	sub, err := subscriptionByID(id)
	if err != nil {
		subscriptionNotFound(c)
		return
	}

	triggerJellyfinLibraryRefresh(context.Background())

	cardSub, err := loadSubscriptionCard(sub.ID)
	if err != nil {
		subscriptionReloadError(c, err)
		return
	}

	triggerAppToast(c, repairSuccessToast(repairActionRefreshLibrary), "success")
	c.HTML(http.StatusOK, "subscription_card.html", cardSub)
}

func recheckSubscriptionRepairAction(c *gin.Context, action repairAction, operation string) {
	id := c.Param("id")
	sub, err := subscriptionByID(id)
	if err != nil {
		subscriptionNotFound(c)
		return
	}

	sub.LastRunSummary = repairPendingSummary(action)
	sub.LastError = ""
	if err := saveSubscription(sub); err != nil {
		subscriptionSaveError(c, operation, err)
		return
	}

	if err := runSubscriptionCheck(sub, "manual"); err != nil {
		sub.LastRunSummary = repairAutoRecheckFailureSummary(action)
		sub.LastError = err.Error()
		_ = saveSubscription(sub)
		subscriptionSaveError(c, operation, err)
		return
	}

	cardSub, err := loadSubscriptionCard(sub.ID)
	if err != nil {
		subscriptionReloadError(c, err)
		return
	}

	triggerAppToast(c, repairSuccessToast(action), "success")
	c.HTML(http.StatusOK, "subscription_card.html", cardSub)
}

func RefreshSubscriptionMetadataHandler(c *gin.Context) {
	id := c.Param("id")
	sub, err := subscriptionByID(id)
	if err != nil {
		subscriptionNotFound(c)
		return
	}

	// Enrich Metadata using shared service
	metaSvc := service.NewMetadataService()
	metaSvc.EnrichMetadata(sub.Metadata, sub.Title)

	if err := saveSubscription(sub); err != nil {
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

	sub, err := subscriptionWithMetadataByID(id)
	if err != nil {
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
