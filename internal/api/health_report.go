package api

import (
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

type HealthReport struct {
	GeneratedAt              time.Time       `json:"generated_at"`
	Configs                  map[string]bool `json:"configs"`
	SubscriptionTotal        int64           `json:"subscription_total"`
	SubscriptionActive       int64           `json:"subscription_active"`
	AutoDisabledOnDone       int64           `json:"auto_disabled_on_done"`
	DownloadCompleted        int64           `json:"download_completed"`
	DownloadDownloading      int64           `json:"download_downloading"`
	DownloadFailed           int64           `json:"download_failed"`
	DownloadArchived         int64           `json:"download_archived"`
	LocalAnimeCount          int64           `json:"local_anime_count"`
	LocalEpisodeCount        int64           `json:"local_episode_count"`
	OpenLibraryIssues        int64           `json:"open_library_issues"`
	JellyfinSeriesCount      int64           `json:"jellyfin_series_count"`
	JellyfinEpisodeCount     int64           `json:"jellyfin_episode_count"`
	SubscriptionsPlayable    int64           `json:"subscriptions_playable"`
	SubscriptionsPendingSync int64           `json:"subscriptions_pending_sync"`
	StaleSubscriptions72H    int64           `json:"stale_subscriptions_72h"`
	HealthTone               string          `json:"health_tone"`
	Summary                  string          `json:"summary"`
	Recommendations          []string        `json:"recommendations"`
}

func buildHealthReport() HealthReport {
	report := HealthReport{
		GeneratedAt: time.Now(),
		Configs: map[string]bool{
			"qB URL":           healthHasConfig(configValue(model.ConfigKeyQBUrl)),
			"TMDB Token":       healthHasConfig(configValue(model.ConfigKeyTMDBToken)),
			"AniList Token":    healthHasConfig(configValue(model.ConfigKeyAniListToken)),
			"Bangumi Token":    healthHasConfig(configValue(model.ConfigKeyBangumiAccessToken)),
			"Jellyfin URL":     healthHasConfig(configValue(model.ConfigKeyJellyfinUrl)),
			"Jellyfin API Key": healthHasConfig(configValue(model.ConfigKeyJellyfinApiKey)),
			"R2 Bucket":        healthHasConfig(configValue(model.ConfigKeyR2Bucket)),
		},
		HealthTone: "emerald",
	}
	subStore := subscriptionStore()
	logStore := downloadLogStore()
	laStore := localAnimeStore()
	if subStore == nil || logStore == nil || laStore == nil {
		report.HealthTone = "rose"
		report.Summary = "数据库尚未初始化"
		report.Recommendations = []string{"请先启动服务并完成数据库初始化。"}
		return report
	}

	report.SubscriptionTotal, _ = subStore.Count()
	report.SubscriptionActive, _ = subStore.CountActive()
	report.AutoDisabledOnDone, _ = subStore.CountAutoDisabledOnDone()
	report.DownloadCompleted, _ = logStore.CountByStatus("completed")
	report.DownloadDownloading, _ = logStore.CountByStatus("downloading")
	report.DownloadFailed, _ = logStore.CountByStatus("failed")
	report.DownloadArchived, _ = logStore.CountByStatus("archived")
	report.LocalAnimeCount, _ = laStore.CountAnimes()
	report.LocalEpisodeCount, _ = laStore.CountEpisodes()
	db.DB.Model(&model.LibraryIssue{}).Where("status = ?", "open").Count(&report.OpenLibraryIssues)
	report.JellyfinSeriesCount, _ = laStore.CountAnimesWithJellyfin()
	report.JellyfinEpisodeCount, _ = laStore.CountEpisodesWithJellyfin()
	report.StaleSubscriptions72H, _ = subStore.CountStaleSince(time.Now().Add(-72 * time.Hour))
	db.DB.Raw(`
		SELECT COUNT(DISTINCT subscriptions.id)
		FROM subscriptions
		JOIN local_animes ON (local_animes.metadata_id = subscriptions.metadata_id OR local_animes.title = subscriptions.title)
		WHERE local_animes.jellyfin_series_id <> ''
	`).Scan(&report.SubscriptionsPlayable)
	db.DB.Raw(`
		SELECT COUNT(DISTINCT subscriptions.id)
		FROM subscriptions
		JOIN local_animes ON (local_animes.metadata_id = subscriptions.metadata_id OR local_animes.title = subscriptions.title)
		WHERE (local_animes.jellyfin_series_id = '' OR local_animes.jellyfin_series_id IS NULL)
	`).Scan(&report.SubscriptionsPendingSync)

	report.Recommendations = buildRecommendations(report)
	report.Summary = buildHealthSummary(report)
	report.HealthTone = determineHealthTone(report)
	return report
}

func buildRecommendations(report HealthReport) []string {
	recommendations := make([]string, 0, 5)
	if report.DownloadDownloading > 0 || report.DownloadFailed > 0 {
		recommendations = append(recommendations, "打开首页任务总览，执行一次下载状态修复。")
	}
	if report.StaleSubscriptions72H > 0 {
		recommendations = append(recommendations, "在订阅页优先处理“长期无进展”或“疑似缺集”的条目。")
	}
	if report.SubscriptionsPendingSync > 0 {
		recommendations = append(recommendations, "有订阅已经入库但还没变成可播放，建议触发一次 Jellyfin 库刷新。")
	}
	if report.OpenLibraryIssues > 0 {
		recommendations = append(recommendations, "本地媒体库仍有打开的诊断问题，建议进入本地番剧页查看修复建议。")
	}
	if !report.Configs["Jellyfin URL"] || !report.Configs["Jellyfin API Key"] {
		recommendations = append(recommendations, "如果需要即下即看，请补全 Jellyfin URL 和 API Key。")
	}
	return recommendations
}

func buildHealthSummary(report HealthReport) string {
	switch {
	case report.DownloadDownloading > 0 || report.DownloadFailed > 0:
		return "下载链路仍有阻塞或失败记录，建议先做一次修复。"
	case report.StaleSubscriptions72H > 0:
		return "存在长时间无进展的订阅，系统需要补一次检查。"
	case report.SubscriptionsPendingSync > 0:
		return "订阅主链已经入库，但还有部分番剧没进入可播放状态。"
	default:
		return "主链整体健康：下载、扫描和媒体库闭环都比较稳定。"
	}
}

func determineHealthTone(report HealthReport) string {
	switch {
	case report.DownloadDownloading > 0 || report.DownloadFailed > 0:
		return "rose"
	case report.StaleSubscriptions72H > 0 || report.SubscriptionsPendingSync > 0 || report.OpenLibraryIssues > 0:
		return "amber"
	default:
		return "emerald"
	}
}

func healthHasConfig(value string) bool {
	return strings.TrimSpace(value) != ""
}
