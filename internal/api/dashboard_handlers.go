package api

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
	"github.com/pokerjest/animateAutoTool/internal/scheduler"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

type DashboardData struct {
	SkipLayout        bool
	ActiveSubs        int64
	TodayDownloads    int64
	QBConnected       bool
	QBVersion         string
	BangumiLogin      bool
	TMDBConnected     bool
	JellyfinConnected bool
}

type TaskOverviewCard struct {
	Title        string
	StatusLabel  string
	StatusTone   string
	Summary      string
	Detail       string
	StartedAt    *time.Time
	FinishedAt   *time.Time
	ProgressText string
	Error        string
	DisplayError string
	ActionLabel  string
	ActionPath   string
}

type TaskOverviewData struct {
	Scheduler TaskOverviewCard
	Scanner   TaskOverviewCard
	Metadata  TaskOverviewCard
	Downloads TaskOverviewCard
}

const (
	taskToneAmber       = "amber"
	taskToneEmerald     = "emerald"
	taskStatusCompleted = "最近已完成"
	taskStatusIdle      = "待命"
)

func DashboardHandler(c *gin.Context) {
	start := time.Now()
	log.Printf("DEBUG: DashboardHandler Started at %v", start)
	defer func() {
		log.Printf("DEBUG: DashboardHandler Finished in %v", time.Since(start))
	}()

	skip := IsHTMX(c)

	var activeSubs int64
	db.DB.Model(&model.Subscription{}).Where("is_active = ?", true).Count(&activeSubs)

	var totalDownloads int64
	db.DB.Model(&model.DownloadLog{}).Count(&totalDownloads)

	var qbConnected bool
	var qbVersion string

	var bangumiLogin bool
	if configValue(model.ConfigKeyBangumiAccessToken) != "" {
		bangumiLogin = true
	}

	var tmdbConnected bool
	if configValue(model.ConfigKeyTMDBToken) != "" {
		tmdbConnected = true
	}

	var jellyfinConnected bool
	if configValue(model.ConfigKeyJellyfinUrl) != "" {
		jellyfinConnected = true
	}

	data := DashboardData{
		SkipLayout:        skip,
		ActiveSubs:        activeSubs,
		TodayDownloads:    totalDownloads,
		QBConnected:       qbConnected,
		QBVersion:         qbVersion,
		BangumiLogin:      bangumiLogin,
		TMDBConnected:     tmdbConnected,
		JellyfinConnected: jellyfinConnected,
	}

	c.HTML(http.StatusOK, "index.html", data)
}

func DashboardBangumiDataHandler(c *gin.Context) {
	var watchingList []bangumi.UserCollectionItem

	if token := configValue(model.ConfigKeyBangumiAccessToken); token != "" {
		client := bangumi.NewClient("", "", "")
		user, err := client.GetCurrentUser(token)
		if err == nil {
			watching, err1 := client.GetUserCollection(token, user.Username, 3, 12, 0)
			if err1 != nil {
				log.Printf("Error fetching watching collection: %v", err1)
			} else {
				watchingList = watching
			}
		} else {
			log.Printf("Error fetching user profile: %v", err)
		}
	}

	c.HTML(http.StatusOK, "dashboard_bangumi.html", gin.H{
		"WatchingList": watchingList,
	})
}

func DashboardTaskOverviewHandler(c *gin.Context) {
	data := TaskOverviewData{
		Scheduler: buildSchedulerTaskCard(),
		Scanner:   buildScannerTaskCard(),
		Metadata:  buildMetadataTaskCard(),
		Downloads: buildDownloadSyncTaskCard(),
	}
	c.HTML(http.StatusOK, "dashboard_task_overview.html", data)
}

func DashboardQBStatusHandler(c *gin.Context) {
	start := time.Now()
	log.Printf("DEBUG: DashboardQBStatusHandler Started")
	defer func() {
		log.Printf("DEBUG: DashboardQBStatusHandler Finished in %v", time.Since(start))
	}()

	qbCfg := qbutil.LoadConfig()
	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, renderQBDashboardBadge(qbCfg))
}

func buildSchedulerTaskCard() TaskOverviewCard {
	status := scheduler.GlobalRunStatus.Snapshot()
	card := TaskOverviewCard{
		Title:      "订阅调度",
		StatusTone: "slate",
	}

	switch {
	case status.IsRunning:
		card.StatusLabel = "运行中"
		card.StatusTone = taskToneAmber
		card.Summary = fmt.Sprintf("正在检查 %d 个订阅", max(status.TotalSubscriptions, 1))
		card.ProgressText = formatSchedulerDetail(status)
	case status.LastFinishedAt != nil || status.LastStartedAt != nil:
		card.StatusLabel = taskStatusCompleted
		card.StatusTone = statusToneFromCounts(status.ErrorCount, status.WarningCount)
		card.Summary = taskCompletedSummary(status.LastSummary, "最近一轮调度已结束")
		card.Detail = formatSchedulerDetail(status)
		card.StartedAt = status.LastStartedAt
		card.FinishedAt = status.LastFinishedAt
		card.Error = status.LastError
		card.DisplayError = humanizeOperationError(status.LastError)
	default:
		card.StatusLabel = taskStatusIdle
		card.Summary = taskNeverRunSummary(card.Title)
		card.Detail = taskNeverRunDetail(card.Title)
	}

	if status.IsRunning {
		card.StartedAt = status.LastStartedAt
		card.Error = status.LastError
		card.DisplayError = humanizeOperationError(status.LastError)
	}

	return card
}

func buildScannerTaskCard() TaskOverviewCard {
	status := service.GlobalScanStatus.Snapshot()
	card := TaskOverviewCard{
		Title:      "本地扫描",
		StatusTone: "slate",
	}

	switch {
	case status.IsRunning:
		card.StatusLabel = "扫描中"
		card.StatusTone = taskToneAmber
		card.Summary = fallbackText(status.LastSummary, "正在扫描本地媒体目录")
		card.ProgressText = fmt.Sprintf("进度 %d/%d", status.ProcessedDirectories, max(status.TotalDirectories, 1))
		if status.CurrentDirectory != "" {
			card.Detail = status.CurrentDirectory
		}
		card.StartedAt = status.LastStartedAt
		card.Error = status.LastError
		card.DisplayError = humanizeOperationError(status.LastError)
	case status.LastFinishedAt != nil || status.LastStartedAt != nil:
		card.StatusLabel = taskStatusCompleted
		card.StatusTone = statusToneFromFailure(status.FailedDirectories)
		card.Summary = taskCompletedSummary(status.LastSummary, "最近一轮扫描已结束")
		card.Detail = fmt.Sprintf("新增 %d，更新 %d，失败 %d", status.AddedCount, status.UpdatedCount, status.FailedDirectories)
		card.StartedAt = status.LastStartedAt
		card.FinishedAt = status.LastFinishedAt
		card.ProgressText = status.LastDuration
		card.Error = status.LastError
		card.DisplayError = humanizeOperationError(status.LastError)
	default:
		card.StatusLabel = taskStatusIdle
		card.Summary = taskNeverRunSummary(card.Title)
		card.Detail = taskNeverRunDetail(card.Title)
	}

	return card
}

func buildMetadataTaskCard() TaskOverviewCard {
	status := service.GlobalRefreshStatus.Snapshot()
	card := TaskOverviewCard{
		Title:      "元数据刷新",
		StatusTone: "slate",
	}

	switch {
	case status.IsRunning:
		card.StatusLabel = "刷新中"
		card.StatusTone = taskToneAmber
		card.Summary = "正在刷新媒体库元数据"
		card.ProgressText = fmt.Sprintf("进度 %d/%d", status.Current, max(status.Total, 1))
		card.Detail = fallbackText(status.CurrentTitle, "正在准备本轮刷新")
	case status.LastResult != "":
		card.StatusLabel = taskStatusCompleted
		card.StatusTone = taskToneEmerald
		card.Summary = status.LastResult
		card.Detail = taskFollowupDetail(card.Title)
	default:
		card.StatusLabel = taskStatusIdle
		card.Summary = taskNeverRunSummary(card.Title)
		card.Detail = taskNeverRunDetail(card.Title)
	}

	return card
}

func buildDownloadSyncTaskCard() TaskOverviewCard {
	status := service.GlobalDownloadLogSyncStatus.Snapshot()
	card := TaskOverviewCard{
		Title:      "下载状态同步",
		StatusTone: "slate",
	}

	var missingTargetCount int64
	var staleDownloadingCount int64
	if db.DB != nil {
		db.DB.Model(&model.DownloadLog{}).
			Where("status = ? AND (target_file = '' OR target_file IS NULL)", "completed").
			Count(&missingTargetCount)
		db.DB.Model(&model.DownloadLog{}).
			Where("status = ? AND created_at < ?", "downloading", time.Now().Add(-6*time.Hour)).
			Count(&staleDownloadingCount)
	}

	switch {
	case status.LastCheckedAt == nil:
		card.StatusLabel = taskStatusIdle
		card.Summary = taskNeverRunSummary(card.Title)
		card.Detail = taskNeverRunDetail(card.Title)
	case status.LastError != "":
		card.StatusLabel = "同步异常"
		card.StatusTone = "rose"
		card.Summary = "最近一次下载状态同步失败"
		card.Detail = fmt.Sprintf("待修复记录 %d，长时间下载中 %d", missingTargetCount, staleDownloadingCount)
		card.FinishedAt = status.LastCheckedAt
		card.Error = status.LastError
		card.DisplayError = humanizeOperationError(status.LastError)
		card.ActionLabel = repairActionCTA(repairActionSyncDownloads)
		card.ActionPath = "/api/download-logs/repair"
	case missingTargetCount > 0 || staleDownloadingCount > 0 || status.LastUnmatched > 0:
		card.StatusLabel = "需要关注"
		card.StatusTone = taskToneAmber
		card.Summary = "下载链路有待补偿的状态记录"
		card.Detail = fmt.Sprintf("待修复记录 %d，长时间下载中 %d，未匹配 torrents %d", missingTargetCount, staleDownloadingCount, status.LastUnmatched)
		card.FinishedAt = status.LastCheckedAt
		card.ProgressText = fmt.Sprintf("最近同步：qB 修复 %d 条，本地回补 %d 条，归档 %d 条，完成 %d，失败 %d", status.LastUpdated, status.LastLibraryRepairs, status.LastArchived, status.LastCompleted, status.LastFailed)
		card.ActionLabel = repairActionCTA(repairActionSyncDownloads)
		card.ActionPath = "/api/download-logs/repair"
	default:
		card.StatusLabel = taskStatusCompleted
		card.StatusTone = taskToneEmerald
		card.Summary = "下载日志与 qB 状态保持一致"
		card.Detail = taskFollowupDetail(card.Title)
		card.FinishedAt = status.LastCheckedAt
		card.ProgressText = fmt.Sprintf("最近同步：qB 修复 %d 条，本地回补 %d 条，归档 %d 条，活跃 %d，完成 %d", status.LastUpdated, status.LastLibraryRepairs, status.LastArchived, status.LastActive, status.LastCompleted)
	}

	return card
}

func RepairDownloadLogsHandler(c *gin.Context) {
	var lastErr error

	qbCfg := qbutil.LoadConfig()
	if !qbutil.ManagedBinaryMissing(qbCfg, config.BinDir()) && !qbutil.MissingExternalURL(qbCfg) && strings.TrimSpace(qbCfg.URL) != "" {
		client := downloader.NewQBittorrentClient(qbCfg.URL)
		if err := client.Login(qbCfg.Username, qbCfg.Password); err != nil {
			lastErr = err
		} else if _, err := service.SyncDownloadLogStatusesWithQBClient(client); err != nil {
			lastErr = err
		}
	}

	if _, err := service.RepairDownloadLogsFromLocalLibrary(6 * time.Hour); err != nil {
		lastErr = err
	}
	if archiveResult, err := service.ArchiveStaleDownloadLogs(clientOrNil(qbCfg, lastErr == nil), 30*24*time.Hour); err != nil {
		lastErr = err
	} else {
		service.GlobalDownloadLogSyncStatus.RecordArchived(archiveResult.Archived)
	}
	if lastErr != nil {
		service.GlobalDownloadLogSyncStatus.RecordFailure(lastErr)
		triggerAppToast(c, repairReviewToast(repairActionSyncDownloads), "error")
	} else {
		triggerAppToast(c, repairSuccessToast(repairActionSyncDownloads), "success")
	}

	DashboardTaskOverviewHandler(c)
}

func clientOrNil(cfg qbutil.Config, ready bool) service.TorrentStatusSource {
	if !ready || qbutil.ManagedBinaryMissing(cfg, config.BinDir()) || qbutil.MissingExternalURL(cfg) || strings.TrimSpace(cfg.URL) == "" {
		return nil
	}
	client := downloader.NewQBittorrentClient(cfg.URL)
	if err := client.Login(cfg.Username, cfg.Password); err != nil {
		return nil
	}
	return client
}

func formatSchedulerDetail(status scheduler.RunStatus) string {
	sourceLabel := map[string]string{
		"auto":   "自动调度",
		"manual": "手动运行",
		"create": "创建后首次检查",
	}[status.LastRunSource]
	if sourceLabel == "" {
		sourceLabel = "最近一轮"
	}

	return fmt.Sprintf("%s · 成功 %d / 警告 %d / 失败 %d / 跳过 %d", sourceLabel, status.SuccessCount, status.WarningCount, status.ErrorCount, status.SkippedCount)
}

func statusToneFromCounts(errorsCount, warnings int) string {
	switch {
	case errorsCount > 0:
		return "rose"
	case warnings > 0:
		return taskToneAmber
	default:
		return taskToneEmerald
	}
}

func statusToneFromFailure(failed int) string {
	if failed > 0 {
		return taskToneAmber
	}
	return taskToneEmerald
}

func fallbackText(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
