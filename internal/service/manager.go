package service

import (
	"log"
	"regexp"

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
		m.processSubscription(&sub)
	}
}

func (m *SubscriptionManager) processSubscription(sub *model.Subscription) {
	episodes, err := m.RSSParser.Parse(sub.RSSUrl)
	if err != nil {
		log.Printf("Failed to parse RSS for %s: %v", sub.Title, err)
		return
	}

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
			continue
		}
		if excludeRe != nil && excludeRe.MatchString(ep.Title) {
			continue
		}

		// 2. 去重: 检查是否已下载 (通过 InfoHash 或 唯一 Title)
		// RSS 中可能没有 InfoHash，只能靠 Title 或 Unique Link。
		// Mikan 的 Enclosure URL 是唯一的。
		// 这里简单用 Title + SubID 查重，或者用 URL 查重。
		// 为了更严谨，我们可以用 URL 作为去重键。

		// 修正：如果使用 Title 去重，可能会有 v2 (修正版) 问题，v2 标题通常不同。可以接受。

		var count int64
		// 查重逻辑：同一个订阅下，TargetFile或者Source URL不能重复
		// 由于RSS并没有InfoHash，我们暂时无法用InfoHash查重，除非下载开始后更新。
		// 这里用 Title 是否已存在于该订阅的历史中来判断
		m.DB.Model(&model.DownloadLog{}).Where("subscription_id = ? AND title = ?", sub.ID, ep.Title).Count(&count)
		if count > 0 {
			continue // 已存在
		}

		// 3. 添加下载
		// 默认保存路径：BaseDir / Title / Season
		// 需要从配置读取 BaseDir，这里暂时假设 sub.SavePath 是完整的相对路径
		savePath := sub.SavePath
		if savePath == "" {
			savePath = "downloads/" + sub.Title
		}

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
			Magnet:         ep.TorrentURL, // 或 Magnet
			Episode:        ep.EpisodeNum,
			SeasonVal:      ep.Season,
			Status:         "downloading",
		}
		m.DB.Create(&logEntry)
	}
}
