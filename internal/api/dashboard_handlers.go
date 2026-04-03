package api

import (
	"fmt"
	"log"
	"net/http"
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
}

type TaskOverviewData struct {
	Scheduler TaskOverviewCard
	Scanner   TaskOverviewCard
	Metadata  TaskOverviewCard
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
	var tokenConfig model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyBangumiAccessToken).First(&tokenConfig).Error; err == nil && tokenConfig.Value != "" {
		bangumiLogin = true
	}

	var tmdbConnected bool
	var tmdbConfig model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyTMDBToken).First(&tmdbConfig).Error; err == nil && tmdbConfig.Value != "" {
		tmdbConnected = true
	}

	var jellyfinConnected bool
	var jellyfinConfig model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyJellyfinUrl).First(&jellyfinConfig).Error; err == nil && jellyfinConfig.Value != "" {
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

	var tokenConfig model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyBangumiAccessToken).First(&tokenConfig).Error; err == nil && tokenConfig.Value != "" {
		client := bangumi.NewClient("", "", "")
		user, err := client.GetCurrentUser(tokenConfig.Value)
		if err == nil {
			watching, err1 := client.GetUserCollection(tokenConfig.Value, user.Username, 3, 12, 0)
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
	if qbutil.ManagedBinaryMissing(qbCfg, config.BinDir()) {
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, `<div id="qb-status-dashboard" title="Managed qBittorrent binary not found and no external WebUI is configured" class="text-amber-600 font-bold flex items-center gap-1.5 bg-amber-50 px-2 py-0.5 rounded-full text-xs"><span class="w-1.5 h-1.5 rounded-full bg-amber-500"></span> Missing</div>`)
		return
	}
	if qbutil.MissingExternalURL(qbCfg) {
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, `<div id="qb-status-dashboard" title="External qBittorrent mode is enabled, but the WebUI URL is empty" class="text-amber-600 font-bold flex items-center gap-1.5 bg-amber-50 px-2 py-0.5 rounded-full text-xs"><span class="w-1.5 h-1.5 rounded-full bg-amber-500"></span> Config</div>`)
		return
	}

	var qbConnected bool
	var qbVersion string
	if qbCfg.URL != "" {
		qbt := downloader.NewQBittorrentClient(qbCfg.URL)
		if err := qbt.Login(qbCfg.Username, qbCfg.Password); err == nil {
			if ver, err := qbt.GetVersion(); err == nil {
				qbConnected = true
				qbVersion = ver
			}
		}
	}

	html := ""
	if qbConnected {
		html = fmt.Sprintf(`<span class="text-emerald-600 font-bold flex items-center gap-1.5 bg-emerald-50 px-2 py-0.5 rounded-full text-xs" title="%s"><span class="w-1.5 h-1.5 rounded-full bg-emerald-500"></span> Connected (%s)</span>`, qbVersion, qbVersion)
	} else {
		html = `<span class="text-red-500 font-bold flex items-center gap-1.5 bg-red-50 px-2 py-0.5 rounded-full text-xs"><span class="w-1.5 h-1.5 rounded-full bg-red-500"></span> Offline</span>`
	}
	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, html)
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
		card.Summary = fallbackText(status.LastSummary, "最近一轮调度已结束")
		card.Detail = formatSchedulerDetail(status)
		card.StartedAt = status.LastStartedAt
		card.FinishedAt = status.LastFinishedAt
		card.Error = status.LastError
	default:
		card.StatusLabel = taskStatusIdle
		card.Summary = "还没有运行过订阅调度"
		card.Detail = "启动后会自动轮询，也可以在订阅页手动立即运行。"
	}

	if status.IsRunning {
		card.StartedAt = status.LastStartedAt
		card.Error = status.LastError
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
	case status.LastFinishedAt != nil || status.LastStartedAt != nil:
		card.StatusLabel = taskStatusCompleted
		card.StatusTone = statusToneFromFailure(status.FailedDirectories)
		card.Summary = fallbackText(status.LastSummary, "最近一轮扫描已结束")
		card.Detail = fmt.Sprintf("新增 %d，更新 %d，失败 %d", status.AddedCount, status.UpdatedCount, status.FailedDirectories)
		card.StartedAt = status.LastStartedAt
		card.FinishedAt = status.LastFinishedAt
		card.ProgressText = status.LastDuration
		card.Error = status.LastError
	default:
		card.StatusLabel = taskStatusIdle
		card.Summary = "还没有运行过本地库扫描"
		card.Detail = "从本地番剧页触发扫描后，这里会展示最近一轮摘要。"
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
		card.Detail = "可在媒体库页再次触发全量或增量刷新。"
	default:
		card.StatusLabel = taskStatusIdle
		card.Summary = "还没有运行过元数据全库刷新"
		card.Detail = "媒体库页的刷新按钮会在这里显示进度和结果。"
	}

	return card
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
