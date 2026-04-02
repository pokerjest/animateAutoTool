package scheduler

import (
	"log"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
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
		return
	}

	qbCfg := qbutil.LoadConfig()
	if qbutil.ManagedBinaryMissing(qbCfg, config.BinDir()) {
		log.Printf("Scheduler: Skipping update check because qBittorrent is not installed and no external WebUI is configured.")
		return
	}
	if qbutil.MissingExternalURL(qbCfg) {
		log.Printf("Scheduler: Skipping update check because external qBittorrent mode has no WebUI URL configured.")
		return
	}

	// Initialize Service Manager
	qbt := downloader.NewQBittorrentClient(qbCfg.URL)
	if err := qbt.Login(qbCfg.Username, qbCfg.Password); err != nil {
		log.Printf("Scheduler Warning: QB unavailable: %v", err)
		return // Can't do anything without QB
	}

	mgr := service.NewSubscriptionManager(qbt)

	for _, sub := range subs {
		log.Printf("Scheduler: Checking sub %s (%s)", sub.Title, sub.RSSUrl)
		mgr.ProcessSubscription(&sub)
	}
}
