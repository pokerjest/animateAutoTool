package scheduler

import (
	"log"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
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

	// Fetch QB Config once
	// We need to fetch it manually here as we are not in a handler context
	var qbUrl, qbUser, qbPass string

	// Helper fetch (inline for now as we can't easily import private handler helper)
	// Or we can just use GORM
	var c1 model.GlobalConfig
	if err := db.DB.First(&c1, "key = 'qb_url'").Error; err == nil {
		qbUrl = c1.Value
	} else {
		qbUrl = "http://localhost:8080"
	}
	var c2 model.GlobalConfig
	if err := db.DB.First(&c2, "key = 'qb_username'").Error; err == nil {
		qbUser = c2.Value
	}
	var c3 model.GlobalConfig
	if err := db.DB.First(&c3, "key = 'qb_password'").Error; err == nil {
		qbPass = c3.Value
	}

	// Initialize Service Manager
	qbt := downloader.NewQBittorrentClient(qbUrl)
	if err := qbt.Login(qbUser, qbPass); err != nil {
		log.Printf("Scheduler Error: QB Login failed: %v", err)
		return // Can't do anything without QB
	}

	mgr := service.NewSubscriptionManager(qbt)

	for _, sub := range subs {
		log.Printf("Scheduler: Checking sub %s (%s)", sub.Title, sub.RSSUrl)
		mgr.ProcessSubscription(&sub)
	}
}
