package service

import (
	"log"
	"regexp"
	"strconv"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"gorm.io/gorm"
)

type SubscriptionManager struct {
	RSSParser  parser.RSSParser
	Downloader downloader.Downloader
	DB         *gorm.DB
}

func NewSubscriptionManager(down downloader.Downloader) *SubscriptionManager {
	return &SubscriptionManager{
		RSSParser:  parser.NewMikanParser(),
		Downloader: down,
		DB:         db.DB,
	}
}

// CheckUpdate 对所有活跃订阅执行一次检查
func (m *SubscriptionManager) CheckUpdate() {
	var subs []model.Subscription
	if err := m.DB.Where("is_active = ?", true).Find(&subs).Error; err != nil {
		log.Printf("Error fetching subscriptions: %v", err)
		return
	}

	for _, sub := range subs {
		m.ProcessSubscription(&sub)
	}
}

func (m *SubscriptionManager) ProcessSubscription(sub *model.Subscription) {
	log.Printf("DEBUG: Processing subscription %s (URL: %s)", sub.Title, sub.RSSUrl)

	episodes, err := m.RSSParser.Parse(sub.RSSUrl)
	if err != nil {
		log.Printf("Failed to parse RSS for %s: %v", sub.Title, err)
		return
	}

	log.Printf("DEBUG: Fetched %d episodes from RSS", len(episodes))

	// 编译正则
	var filterRe, excludeRe *regexp.Regexp
	if sub.FilterRule != "" {
		filterRe, _ = regexp.Compile(sub.FilterRule)
	}
	if sub.ExcludeRule != "" {
		excludeRe, _ = regexp.Compile(sub.ExcludeRule)
	}

	for _, ep := range episodes {
		// 1. 规则过滤
		if filterRe != nil && !filterRe.MatchString(ep.Title) {
			log.Printf("DEBUG: Filter skipped: %s (Rule: %s)", ep.Title, sub.FilterRule)
			continue
		}
		if excludeRe != nil && excludeRe.MatchString(ep.Title) {
			log.Printf("DEBUG: Exclude skipped: %s (Rule: %s)", ep.Title, sub.ExcludeRule)
			continue
		}

		// 2. 去重
		var count int64
		// 查重逻辑：同一个订阅下，TargetFile或者Source URL不能重复
		m.DB.Model(&model.DownloadLog{}).Where("subscription_id = ? AND title = ?", sub.ID, ep.Title).Count(&count)
		if count > 0 {
			log.Printf("DEBUG: Duplicate check skipped: %s (Already exists in logs)", ep.Title)
			continue // 已存在
		}

		// 3. 添加下载
		// 默认保存路径：BaseDir / Title / Season
		// 需要从配置读取 BaseDir，这里暂时假设 sub.SavePath 是完整的相对路径
		savePath := sub.SavePath
		if savePath == "" {
			savePath = "downloads/" + sub.Title
		}

		log.Printf("DEBUG: Adding torrent to QB: %s -> %s", ep.Title, savePath)
		err := m.Downloader.AddTorrent(ep.TorrentURL, savePath, "Anime", false)
		if err != nil {
			log.Printf("Failed to add torrent for %s - %s: %v", sub.Title, ep.Title, err)
			continue
		}

		log.Printf("Added torrent: %s [%s]", sub.Title, ep.Title)

		// 4. 记录日志
		logEntry := model.DownloadLog{
			SubscriptionID: sub.ID,
			Title:          ep.Title,
			Magnet:         ep.TorrentURL,
			Episode:        ep.EpisodeNum,
			SeasonVal:      ep.Season,
			// InfoHash:       ep.InfoHash, // Undefined in parser.Episode
			Status: "downloading",
		}
		if err := m.DB.Create(&logEntry).Error; err != nil {
			log.Printf("Failed to create log for %s: %v", ep.Title, err)
		} else {
			// Update LastEp
			if val, err := strconv.Atoi(ep.EpisodeNum); err == nil {
				if val > sub.LastEp {
					sub.LastEp = val
					m.DB.Model(sub).Update("last_ep", val)
				}
			} else {
				// Try float roughly
				if f, err := strconv.ParseFloat(ep.EpisodeNum, 64); err == nil {
					val = int(f)
					if val > sub.LastEp {
						sub.LastEp = val
						m.DB.Model(sub).Update("last_ep", val)
					}
				}
			}
		}
	}
}
