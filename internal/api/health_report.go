package api

import (
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

type HealthReport struct {
	GeneratedAt              time.Time
	Configs                  map[string]bool
	SubscriptionTotal        int64
	SubscriptionActive       int64
	AutoDisabledOnDone       int64
	DownloadCompleted        int64
	DownloadDownloading      int64
	DownloadFailed           int64
	DownloadArchived         int64
	LocalAnimeCount          int64
	LocalEpisodeCount        int64
	OpenLibraryIssues        int64
	JellyfinSeriesCount      int64
	JellyfinEpisodeCount     int64
	SubscriptionsPlayable    int64
	SubscriptionsPendingSync int64
	StaleSubscriptions72H    int64
	HealthTone               string
	Summary                  string
	Recommendations          []string
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
	if db.DB == nil {
		report.HealthTone = "rose"
		report.Summary = "数据库尚未初始化"
		report.Recommendations = []string{"请先启动服务并完成数据库初始化。"}
		return report
	}

	db.DB.Model(&model.Subscription{}).Count(&report.SubscriptionTotal)
	db.DB.Model(&model.Subscription{}).Where("is_active = ?", true).Count(&report.SubscriptionActive)
	db.DB.Model(&model.Subscription{}).
		Where("is_active = ? AND auto_disable_on_done = ? AND expected_episodes > 0 AND last_ep >= expected_episodes", false, true).
		Count(&report.AutoDisabledOnDone)
	db.DB.Model(&model.DownloadLog{}).Where("status = ?", "completed").Count(&report.DownloadCompleted)
	db.DB.Model(&model.DownloadLog{}).Where("status = ?", "downloading").Count(&report.DownloadDownloading)
	db.DB.Model(&model.DownloadLog{}).Where("status = ?", "failed").Count(&report.DownloadFailed)
	db.DB.Model(&model.DownloadLog{}).Where("status = ?", "archived").Count(&report.DownloadArchived)
	db.DB.Model(&model.LocalAnime{}).Count(&report.LocalAnimeCount)
	db.DB.Model(&model.LocalEpisode{}).Count(&report.LocalEpisodeCount)
	db.DB.Model(&model.LibraryIssue{}).Where("status = ?", "open").Count(&report.OpenLibraryIssues)
	db.DB.Model(&model.LocalAnime{}).Where("jellyfin_series_id <> ''").Count(&report.JellyfinSeriesCount)
	db.DB.Model(&model.LocalEpisode{}).Where("jellyfin_item_id <> ''").Count(&report.JellyfinEpisodeCount)
	db.DB.Model(&model.Subscription{}).
		Where("is_active = ? AND stale_after_hours > 0 AND last_success_at IS NOT NULL AND last_success_at < ?",
			true, time.Now().Add(-72*time.Hour)).
		Count(&report.StaleSubscriptions72H)
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

func configTruthCount(configs map[string]bool) int {
	count := 0
	for _, ok := range configs {
		if ok {
			count++
		}
	}
	return count
}

func healthConfigSummary(configs map[string]bool) string {
	available := make([]string, 0, len(configs))
	for name, ok := range configs {
		if ok {
			available = append(available, name)
		}
	}
	return strings.Join(available, "、")
}

func healthHasConfig(value string) bool {
	return strings.TrimSpace(value) != ""
}
