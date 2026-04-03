package scheduler

import (
	"log"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

type Manager struct {
	ticker *time.Ticker
	quit   chan struct{}
}

func NewManager() *Manager {
	// 每15分钟检查一次
	return &Manager{
		ticker: time.NewTicker(15 * time.Minute),
		quit:   make(chan struct{}),
	}
}

func (m *Manager) Start() {
	log.Println("Scheduler started...")
	go func() {
		for {
			select {
			case <-m.ticker.C:
				m.CheckUpdates()
			case <-m.quit:
				m.ticker.Stop()
				return
			}
		}
	}()
	// 立即执行一次
	go m.CheckUpdates()
}

func (m *Manager) Stop() {
	close(m.quit)
	log.Println("Scheduler stopped.")
}

func (m *Manager) CheckUpdates() {
	log.Println("Scheduler: Checking updates...")
	var subs []model.Subscription
	// 只查 Active 的
	if err := db.DB.Where("is_active = ?", true).Find(&subs).Error; err != nil {
		log.Printf("Scheduler Error: Failed to fetch subscriptions: %v", err)
		status := GlobalRunStatus.Skip("auto", "获取订阅列表失败")
		publishSchedulerStatus(status)
		return
	}

	GlobalRunStatus.Begin("auto", len(subs))
	publishSchedulerStatus(GlobalRunStatus.Snapshot())

	qbCfg := qbutil.LoadConfig()
	if qbutil.ManagedBinaryMissing(qbCfg, config.BinDir()) {
		log.Printf("Scheduler: Skipping update check because qBittorrent is not installed and no external WebUI is configured.")
		status := GlobalRunStatus.Skip("auto", "未检测到可用的 qBittorrent 配置")
		publishSchedulerStatus(status)
		return
	}
	if qbutil.MissingExternalURL(qbCfg) {
		log.Printf("Scheduler: Skipping update check because external qBittorrent mode has no WebUI URL configured.")
		status := GlobalRunStatus.Skip("auto", "外部 qBittorrent 模式缺少 WebUI 地址")
		publishSchedulerStatus(status)
		return
	}

	// Initialize Service Manager
	qbt := downloader.NewQBittorrentClient(qbCfg.URL)
	if err := qbt.Login(qbCfg.Username, qbCfg.Password); err != nil {
		log.Printf("Scheduler Warning: QB unavailable: %v", err)
		status := GlobalRunStatus.Skip("auto", "qBittorrent 登录失败")
		status.LastError = err.Error()
		publishSchedulerStatus(status)
		return // Can't do anything without QB
	}

	mgr := service.NewSubscriptionManager(qbt)
	successCount := 0
	warningCount := 0
	errorCount := 0
	lastErr := ""

	for _, sub := range subs {
		log.Printf("Scheduler: Checking sub %s (%s)", sub.Title, sub.RSSUrl)
		mgr.ProcessSubscriptionWithSource(&sub, "auto")
		switch sub.LastRunStatus {
		case "success", "idle":
			successCount++
		case "warning":
			warningCount++
			if lastErr == "" {
				lastErr = sub.LastError
			}
		case "error":
			errorCount++
			if lastErr == "" {
				lastErr = sub.LastError
			}
		default:
			successCount++
		}
	}

	status := GlobalRunStatus.Finish(successCount, warningCount, errorCount, len(subs), "auto", lastErr)
	publishSchedulerStatus(status)
}

func publishSchedulerStatus(status RunStatus) {
	event.GlobalBus.Publish(event.EventSchedulerRun, map[string]interface{}{
		"is_running":          status.IsRunning,
		"last_run_source":     status.LastRunSource,
		"total_subscriptions": status.TotalSubscriptions,
		"success_count":       status.SuccessCount,
		"warning_count":       status.WarningCount,
		"error_count":         status.ErrorCount,
		"skipped_count":       status.SkippedCount,
		"last_summary":        status.LastSummary,
		"last_error":          status.LastError,
		"last_started_at":     formatSchedulerTime(status.LastStartedAt),
		"last_finished_at":    formatSchedulerTime(status.LastFinishedAt),
	})
}

func formatSchedulerTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
