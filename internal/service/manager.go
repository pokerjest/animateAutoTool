package service

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/store"
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

func RetrySubscriptionsByID(ctx context.Context, down downloader.Downloader, ids []uint, source string) error {
	if db.DB == nil || len(ids) == 0 {
		return nil
	}
	unique := make(map[uint]struct{}, len(ids))
	filtered := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := unique[id]; ok {
			continue
		}
		unique[id] = struct{}{}
		filtered = append(filtered, id)
	}
	if len(filtered) == 0 {
		return nil
	}

	var subs []model.Subscription
	if err := db.DB.Where("id IN ? AND is_active = ?", filtered, true).Find(&subs).Error; err != nil {
		return err
	}
	mgr := NewSubscriptionManager(down)
	for i := range subs {
		mgr.ProcessSubscriptionWithSourceContext(ctx, &subs[i], source)
	}
	return nil
}

func RetryStaleSubscriptions(ctx context.Context, down downloader.Downloader, minCheckInterval time.Duration, source string) (int, error) {
	if db.DB == nil || down == nil {
		return 0, nil
	}
	if minCheckInterval <= 0 {
		minCheckInterval = 2 * time.Hour
	}

	now := time.Now()
	var subs []model.Subscription
	if err := db.DB.
		Where("is_active = ? AND stale_after_hours > 0 AND last_success_at IS NOT NULL", true).
		Find(&subs).Error; err != nil {
		return 0, err
	}

	mgr := NewSubscriptionManager(down)
	retried := 0
	for i := range subs {
		sub := &subs[i]
		if sub.LastSuccessAt == nil {
			continue
		}
		staleFor := time.Duration(sub.StaleAfterHours) * time.Hour
		if staleFor <= 0 {
			continue
		}
		if now.Sub(*sub.LastSuccessAt) < staleFor {
			continue
		}
		if sub.LastCheckAt != nil && now.Sub(*sub.LastCheckAt) < minCheckInterval {
			continue
		}
		mgr.ProcessSubscriptionWithSourceContext(ctx, sub, source)
		retried++
	}
	return retried, nil
}

// CheckUpdate 对所有活跃订阅执行一次检查
func (m *SubscriptionManager) CheckUpdate() {
	m.CheckUpdateContext(context.Background())
}

func (m *SubscriptionManager) CheckUpdateContext(ctx context.Context) {
	var subs []model.Subscription
	if err := m.DB.Where("is_active = ?", true).Find(&subs).Error; err != nil {
		log.Printf("Error fetching subscriptions: %v", err)
		return
	}

	for _, sub := range subs {
		m.ProcessSubscriptionWithSourceContext(ctx, &sub, "auto")
	}
}

func (m *SubscriptionManager) ProcessSubscription(sub *model.Subscription) {
	m.ProcessSubscriptionWithSourceContext(context.Background(), sub, "manual")
}

func (m *SubscriptionManager) ProcessSubscriptionWithSource(sub *model.Subscription, source string) {
	m.ProcessSubscriptionWithSourceContext(context.Background(), sub, source)
}

func (m *SubscriptionManager) ProcessSubscriptionWithSourceContext(ctx context.Context, sub *model.Subscription, source string) {
	log.Printf("DEBUG: Processing subscription %s (URL: %s)", sub.Title, sub.RSSUrl)
	checkedAt := time.Now()

	episodes, activeRSS, fallbackUsed, err := m.parseRSSWithFallback(ctx, sub)
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

	rules := buildSubscriptionRuleSet(sub)

	addedCount := 0
	failedCount := 0
	filteredCount := 0
	duplicateCount := 0
	latestTitle := ""
	lastError := ""

	for _, ep := range episodes {
		// 1. 规则过滤
		if !rules.allows(ep) {
			log.Printf("DEBUG: Rule skipped: %s (Filter: %s Exclude: %s SubGroup: %s)", ep.Title, sub.FilterRule, sub.ExcludeRule, ep.SubGroup)
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
		savePath := m.resolveSavePath(sub)

		log.Printf("DEBUG: Adding torrent to QB: %s -> %s", ep.Title, savePath)
		err := m.addTorrent(ctx, ep.TorrentURL, savePath, "Anime", false)
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
		state.Summary = strings.TrimSpace(m.buildIdleRunSummary(sub, len(episodes), filteredCount, duplicateCount))
	}

	if fallbackUsed {
		fallbackNote := "已自动切换到备用 RSS 继续检查"
		if activeRSS != "" {
			fallbackNote = "主 RSS 暂时不可用，已使用备用 RSS"
		}
		if state.Summary == "" {
			state.Summary = fallbackNote
		} else {
			state.Summary = strings.TrimSpace(state.Summary + "；" + fallbackNote)
		}
	}

	if shouldAutoDisableSubscription(sub, state) {
		if err := m.DB.Model(&model.Subscription{}).Where("id = ?", sub.ID).Update("is_active", false).Error; err != nil {
			log.Printf("Failed to auto-disable subscription %s: %v", sub.Title, err)
		} else {
			sub.IsActive = false
			if state.Summary == "" {
				state.Summary = "已完成全部集数，订阅已自动停用"
			} else {
				state.Summary = strings.TrimSpace(state.Summary + "；已完成全部集数，订阅已自动停用")
			}
		}
	}

	m.persistRunState(sub, state)
}

func (m *SubscriptionManager) parseRSSWithFallback(ctx context.Context, sub *model.Subscription) ([]parser.Episode, string, bool, error) {
	if sub == nil {
		return nil, "", false, fmt.Errorf("subscription is nil")
	}

	primary := strings.TrimSpace(sub.RSSUrl)
	backup := strings.TrimSpace(sub.BackupRSSUrl)

	episodes, err := m.parseRSS(ctx, primary)
	if err == nil && len(episodes) > 0 {
		return episodes, primary, false, nil
	}

	if backup == "" || backup == primary {
		if err != nil {
			return nil, primary, false, err
		}
		return episodes, primary, false, nil
	}

	backupEpisodes, backupErr := m.parseRSS(ctx, backup)
	if backupErr != nil {
		if err != nil {
			return nil, primary, false, err
		}
		return backupEpisodes, backup, true, backupErr
	}
	return backupEpisodes, backup, true, nil
}

func shouldAutoDisableSubscription(sub *model.Subscription, state subscriptionRunState) bool {
	if sub == nil || !sub.AutoDisableOnDone || sub.ExpectedEpisodes <= 0 {
		return false
	}
	if state.NewDownloads > 0 || state.FailedDownloads > 0 {
		return false
	}
	return sub.LastEp >= sub.ExpectedEpisodes
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

func (m *SubscriptionManager) buildIdleRunSummary(sub *model.Subscription, total, filtered, duplicate int) string {
	subtitleGroup := ""
	if sub != nil {
		subtitleGroup = strings.TrimSpace(sub.SubtitleGroup)
	}

	switch {
	case total == 0:
		if diagnosed := m.diagnoseEmptySubscriptionFeed(sub); diagnosed != "" {
			return diagnosed
		}
		if subtitleGroup != "" {
			return fmt.Sprintf("RSS 当前没有可用剧集（字幕组 %s）", subtitleGroup)
		}
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

func (m *SubscriptionManager) diagnoseEmptySubscriptionFeed(sub *model.Subscription) string {
	return m.diagnoseEmptySubscriptionFeedContext(context.Background(), sub)
}

func (m *SubscriptionManager) diagnoseEmptySubscriptionFeedContext(ctx context.Context, sub *model.Subscription) string {
	if m == nil || m.RSSParser == nil || sub == nil {
		return ""
	}

	subtitleGroup := strings.TrimSpace(sub.SubtitleGroup)
	if subtitleGroup == "" || strings.TrimSpace(sub.RSSUrl) == "" {
		return ""
	}

	u, err := url.Parse(sub.RSSUrl)
	if err != nil {
		return ""
	}
	query := u.Query()
	if query.Get("subgroupid") == "" {
		return ""
	}
	query.Del("subgroupid")
	u.RawQuery = query.Encode()
	fallbackURL := u.String()
	if fallbackURL == "" || fallbackURL == strings.TrimSpace(sub.RSSUrl) {
		return ""
	}

	episodes, err := m.parseRSS(ctx, fallbackURL)
	if err != nil || len(episodes) == 0 {
		return ""
	}

	return fmt.Sprintf("当前字幕组 RSS 为空（%s），但该番剧主 RSS 还有 %d 集可用", subtitleGroup, len(episodes))
}

func (m *SubscriptionManager) resolveSavePath(sub *model.Subscription) string {
	if sub == nil {
		return "downloads"
	}

	if savePath := strings.TrimSpace(sub.SavePath); savePath != "" {
		return savePath
	}

	baseDir := strings.TrimSpace(m.loadGlobalConfigValue(model.ConfigKeyBaseDir))
	if baseDir != "" {
		return joinDownloadPath(baseDir, strings.TrimSpace(sub.Title))
	}

	return joinDownloadPath("downloads", strings.TrimSpace(sub.Title))
}

func (m *SubscriptionManager) loadGlobalConfigValue(key string) string {
	if m.DB == nil {
		return ""
	}
	return store.NewConfigStore(m.DB).GetDefault(key, "")
}

func (m *SubscriptionManager) parseRSS(ctx context.Context, feedURL string) ([]parser.Episode, error) {
	if ctxParser, ok := m.RSSParser.(parser.ContextRSSParser); ok {
		return ctxParser.ParseContext(ctx, feedURL)
	}
	return m.RSSParser.Parse(feedURL)
}

func (m *SubscriptionManager) addTorrent(ctx context.Context, torrentURL, savePath, category string, paused bool) error {
	if ctxDownloader, ok := m.Downloader.(downloader.ContextDownloader); ok {
		return ctxDownloader.AddTorrentContext(ctx, torrentURL, savePath, category, paused)
	}
	return m.Downloader.AddTorrent(torrentURL, savePath, category, paused)
}

func joinDownloadPath(base, child string) string {
	base = strings.TrimSpace(base)
	child = strings.TrimSpace(child)
	if base == "" {
		return child
	}
	if child == "" {
		return base
	}

	if strings.HasSuffix(base, "/") || strings.HasSuffix(base, `\`) {
		return base + child
	}

	sep := "/"
	lastForwardSlash := strings.LastIndex(base, "/")
	lastBackSlash := strings.LastIndex(base, `\`)
	if lastBackSlash > lastForwardSlash || looksLikeWindowsDrive(base) {
		sep = `\`
	}

	return base + sep + child
}

func looksLikeWindowsDrive(path string) bool {
	if len(path) < 2 {
		return false
	}
	drive := path[0]
	return path[1] == ':' && ((drive >= 'A' && drive <= 'Z') || (drive >= 'a' && drive <= 'z'))
}
