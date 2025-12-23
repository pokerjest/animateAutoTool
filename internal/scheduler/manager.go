package scheduler

import (
	"log"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/pkg/rss"
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

	for _, sub := range subs {
		log.Printf("Scheduler: Checking sub %s (%s)", sub.Title, sub.RSSUrl)

		// 1. Parse RSS
		items, err := rss.ParseMikan(sub.RSSUrl)
		if err != nil {
			log.Printf("Scheduler: Failed to parse RSS for %s: %v", sub.Title, err)
			continue
		}

		for _, item := range items {
			// 2. Filter (Simple Check)
			// TODO: Implement regex filter from sub.FilterRule

			// 3. Check Deduplication (Magnet Link)
			var count int64
			db.DB.Model(&model.DownloadLog{}).Where("magnet = ?", item.Link).Count(&count)
			if count > 0 {
				// Skip existing
				continue
			}

			// 4. New Item Found
			log.Printf("Scheduler: New Item Found: %s", item.Title)

			// 5. Add to qBittorrent (Mock for now)
			// downloader.Add(item.Link, sub.SavePath...)

			// 6. Record Log
			newLog := model.DownloadLog{
				SubscriptionID: sub.ID,
				Title:          item.Title,
				Magnet:         item.Link,
				Status:         "downloading", // Initial status
			}
			if err := db.DB.Create(&newLog).Error; err != nil {
				log.Printf("Scheduler: Failed to create log: %v", err)
			} else {
				log.Printf("Scheduler: DownloadLog created for %s", item.Title)
			}
		}
	}
}
