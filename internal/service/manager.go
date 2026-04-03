package service

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"gorm.io/gorm"
)

type SubscriptionManager struct {
	RSSParser  parser.RSSParser
	Downloader downloader.Downloader
	DB         *gorm.DB
}

const (
	SubscriptionRunStatusSuccess = "success"
	SubscriptionRunStatusWarning = "warning"
	SubscriptionRunStatusError   = "error"
	SubscriptionRunStatusIdle    = "idle"
)

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
		m.ProcessSubscriptionWithSource(&sub, "auto")
	}
}

func (m *SubscriptionManager) ProcessSubscription(sub *model.Subscription) {
	m.ProcessSubscriptionWithSource(sub, "manual")
}

func (m *SubscriptionManager) ProcessSubscriptionWithSource(sub *model.Subscription, source string) {
	log.Printf("DEBUG: Processing subscription %s (URL: %s)", sub.Title, sub.RSSUrl)
	checkedAt := time.Now()

	episodes, err := m.RSSParser.Parse(sub.RSSUrl)
	if err != nil {
		log.Printf("Failed to parse RSS for %s: %v", sub.Title, err)
		m.persistRunState(sub, subscriptionRunState{
			Source:    normalizeRunSource(source),
			CheckedAt: checkedAt,
			Status:    SubscriptionRunStatusError,
			Summary:   "RSS 解析失败",
			Error:     err.Error(),
		})
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

	addedCount := 0
	failedCount := 0
	filteredCount := 0
	duplicateCount := 0
	latestTitle := ""
	lastError := ""

	for _, ep := range episodes {
		// 1. 规则过滤
		if filterRe != nil && !filterRe.MatchString(ep.Title) {
			log.Printf("DEBUG: Filter skipped: %s (Rule: %s)", ep.Title, sub.FilterRule)
			filteredCount++
			continue
		}
		if excludeRe != nil && excludeRe.MatchString(ep.Title) {
			log.Printf("DEBUG: Exclude skipped: %s (Rule: %s)", ep.Title, sub.ExcludeRule)
			filteredCount++
			continue
		}

		// 2. 去重
		var count int64
		// 查重逻辑：同一个订阅下，TargetFile或者Source URL不能重复
		m.DB.Model(&model.DownloadLog{}).Where("subscription_id = ? AND title = ?", sub.ID, ep.Title).Count(&count)
		if count > 0 {
			log.Printf("DEBUG: Duplicate check skipped: %s (Already exists in logs)", ep.Title)
			duplicateCount++
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
			failedCount++
			if lastError == "" {
				lastError = fmt.Sprintf("%s: %v", ep.Title, err)
			}
			continue
		}

		log.Printf("Added torrent: %s [%s]", sub.Title, ep.Title)
		addedCount++
		latestTitle = ep.Title

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

	state := subscriptionRunState{
		Source:              normalizeRunSource(source),
		CheckedAt:           checkedAt,
		TotalEpisodes:       len(episodes),
		FilteredCount:       filteredCount,
		DuplicateCount:      duplicateCount,
		NewDownloads:        addedCount,
		FailedDownloads:     failedCount,
		LastDownloadedTitle: latestTitle,
		Error:               lastError,
	}

	switch {
	case addedCount > 0 && failedCount == 0:
		state.Status = SubscriptionRunStatusSuccess
		state.Summary = fmt.Sprintf("新增 %d 集待下载", addedCount)
	case addedCount > 0 && failedCount > 0:
		state.Status = SubscriptionRunStatusWarning
		state.Summary = fmt.Sprintf("新增 %d 集，另有 %d 集加入下载失败", addedCount, failedCount)
	case failedCount > 0:
		state.Status = SubscriptionRunStatusError
		state.Summary = fmt.Sprintf("本次检查有 %d 集加入下载失败", failedCount)
	default:
		state.Status = SubscriptionRunStatusIdle
		state.Summary = buildIdleRunSummary(len(episodes), filteredCount, duplicateCount)
	}

	m.persistRunState(sub, state)
}

type subscriptionRunState struct {
	Source              string
	CheckedAt           time.Time
	Status              string
	Summary             string
	Error               string
	TotalEpisodes       int
	FilteredCount       int
	DuplicateCount      int
	NewDownloads        int
	FailedDownloads     int
	LastDownloadedTitle string
}

func (m *SubscriptionManager) persistRunState(sub *model.Subscription, state subscriptionRunState) {
	updates := map[string]interface{}{
		"last_check_at":         state.CheckedAt,
		"last_run_status":       state.Status,
		"last_run_summary":      strings.TrimSpace(state.Summary),
		"last_error":            strings.TrimSpace(state.Error),
		"last_new_downloads":    state.NewDownloads,
		"last_downloaded_title": strings.TrimSpace(state.LastDownloadedTitle),
	}

	if state.Status == SubscriptionRunStatusSuccess || state.Status == SubscriptionRunStatusWarning || state.Status == SubscriptionRunStatusIdle {
		updates["last_success_at"] = state.CheckedAt
	}

	if sub != nil {
		sub.LastCheckAt = &state.CheckedAt
		sub.LastRunStatus = state.Status
		sub.LastRunSummary = updates["last_run_summary"].(string)
		sub.LastError = updates["last_error"].(string)
		sub.LastNewDownloads = state.NewDownloads
		sub.LastDownloadedTitle = updates["last_downloaded_title"].(string)
		if _, ok := updates["last_success_at"]; ok {
			sub.LastSuccessAt = &state.CheckedAt
		}
	}

	if sub == nil || sub.ID == 0 || m.DB == nil {
		return
	}

	if err := m.DB.Model(&model.Subscription{}).Where("id = ?", sub.ID).Updates(updates).Error; err != nil {
		log.Printf("Failed to persist subscription run state for %s: %v", sub.Title, err)
	}

	if err := m.appendRunLog(sub, state); err != nil {
		log.Printf("Failed to append subscription run log for %s: %v", sub.Title, err)
	}

	event.GlobalBus.Publish(event.EventSubscriptionRun, map[string]interface{}{
		"subscription_id":       sub.ID,
		"title":                 sub.Title,
		"status":                state.Status,
		"summary":               strings.TrimSpace(state.Summary),
		"last_error":            strings.TrimSpace(state.Error),
		"last_new_downloads":    state.NewDownloads,
		"last_downloaded_title": strings.TrimSpace(state.LastDownloadedTitle),
		"checked_at":            state.CheckedAt.Format(time.RFC3339),
	})
}

func (m *SubscriptionManager) appendRunLog(sub *model.Subscription, state subscriptionRunState) error {
	if sub == nil || sub.ID == 0 || m.DB == nil {
		return nil
	}

	return m.DB.Create(&model.SubscriptionRunLog{
		SubscriptionID:      sub.ID,
		CheckedAt:           state.CheckedAt,
		TriggerSource:       normalizeRunSource(state.Source),
		Status:              state.Status,
		Summary:             strings.TrimSpace(state.Summary),
		Error:               strings.TrimSpace(state.Error),
		TotalEpisodes:       state.TotalEpisodes,
		FilteredCount:       state.FilteredCount,
		DuplicateCount:      state.DuplicateCount,
		NewDownloads:        state.NewDownloads,
		FailedDownloads:     state.FailedDownloads,
		LastDownloadedTitle: strings.TrimSpace(state.LastDownloadedTitle),
	}).Error
}

func normalizeRunSource(source string) string {
	switch strings.TrimSpace(source) {
	case "auto", "create":
		return source
	default:
		return "manual"
	}
}

func buildIdleRunSummary(total, filtered, duplicate int) string {
	switch {
	case total == 0:
		return "RSS 当前没有可用剧集"
	case filtered > 0 && duplicate == 0:
		return fmt.Sprintf("检查到 %d 集，但都被过滤规则跳过", total)
	case duplicate > 0 && filtered == 0:
		return fmt.Sprintf("检查到 %d 集，但都已经在下载记录中", total)
	case filtered > 0 || duplicate > 0:
		return fmt.Sprintf("未发现新剧集（过滤 %d，已存在 %d）", filtered, duplicate)
	default:
		return "未发现可下载新剧集"
	}
}
