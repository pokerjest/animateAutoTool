package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"gorm.io/gorm"
)

type doctorReport struct {
	DatabasePath    string            `json:"database_path"`
	DataDir         string            `json:"data_dir"`
	LogsDir         string            `json:"logs_dir"`
	Configs         map[string]bool   `json:"configs"`
	Subscriptions   doctorCounts      `json:"subscriptions"`
	DownloadLogs    doctorLogCounts   `json:"download_logs"`
	LocalLibrary    doctorLibraryInfo `json:"local_library"`
	StaleCount72H   int64             `json:"stale_subscription_count_72h"`
	Recommendations []string          `json:"recommendations"`
}

type doctorCounts struct {
	Total              int64 `json:"total"`
	Active             int64 `json:"active"`
	AutoDisabledOnDone int64 `json:"auto_disabled_on_done"`
}

type doctorLogCounts struct {
	Completed   int64 `json:"completed"`
	Downloading int64 `json:"downloading"`
	Failed      int64 `json:"failed"`
	Archived    int64 `json:"archived"`
}

type doctorLibraryInfo struct {
	AnimeCount               int64 `json:"anime_count"`
	EpisodeCount             int64 `json:"episode_count"`
	OpenIssues               int64 `json:"open_issues"`
	JellyfinSeriesCount      int64 `json:"jellyfin_series_count"`
	JellyfinEpisodeCount     int64 `json:"jellyfin_episode_count"`
	SubscriptionsPlayable    int64 `json:"subscriptions_playable"`
	SubscriptionsPendingSync int64 `json:"subscriptions_pending_sync"`
}

func main() {
	jsonMode := flag.Bool("json", false, "以 JSON 输出诊断结果")
	flag.Parse()

	if err := run(*jsonMode); err != nil {
		log.Printf("doctor failed: %v", err)
		os.Exit(1)
	}
}

func run(jsonMode bool) error {
	if err := config.LoadConfigReadOnly(""); err != nil {
		return fmt.Errorf("加载只读配置失败: %w", err)
	}

	readDB, sqlDB, err := db.OpenReadOnly(config.AppConfig.Database.Path)
	if err != nil {
		return fmt.Errorf("打开只读数据库失败: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()
	if err := validateReadOnlyDatabase(readDB); err != nil {
		return fmt.Errorf("数据库不满足只读诊断要求: %w", err)
	}

	cfgStore := store.NewConfigStore(readDB)
	cfgMap, err := cfgStore.ListMap()
	if err != nil {
		return fmt.Errorf("读取全局配置失败: %w", err)
	}

	var (
		subscriptionCount    int64
		activeSubscription   int64
		downloadingLogs      int64
		failedLogs           int64
		archivedLogs         int64
		completedLogs        int64
		localAnimeCount      int64
		localEpisodeCount    int64
		openLibraryIssues    int64
		jellyfinSeriesCount  int64
		jellyfinEpisodeCount int64
		staleSubscriptions   int64
		autoDisabledFinished int64
		playableSubs         int64
		pendingSyncSubs      int64
	)

	queries := []struct {
		name  string
		query *gorm.DB
	}{
		{"订阅总数", readDB.Model(&model.Subscription{}).Count(&subscriptionCount)},
		{"激活订阅数", readDB.Model(&model.Subscription{}).Where("is_active = ?", true).Count(&activeSubscription)},
		{"已完结自动停用订阅数", readDB.Model(&model.Subscription{}).Where("is_active = ? AND auto_disable_on_done = ? AND expected_episodes > 0 AND last_ep >= expected_episodes", false, true).Count(&autoDisabledFinished)},
		{"下载中日志数", readDB.Model(&model.DownloadLog{}).Where("status = ?", "downloading").Count(&downloadingLogs)},
		{"失败日志数", readDB.Model(&model.DownloadLog{}).Where("status = ?", "failed").Count(&failedLogs)},
		{"归档日志数", readDB.Model(&model.DownloadLog{}).Where("status = ?", "archived").Count(&archivedLogs)},
		{"已完成日志数", readDB.Model(&model.DownloadLog{}).Where("status = ?", "completed").Count(&completedLogs)},
		{"本地番剧数", readDB.Model(&model.LocalAnime{}).Count(&localAnimeCount)},
		{"本地单集数", readDB.Model(&model.LocalEpisode{}).Count(&localEpisodeCount)},
		{"打开的媒体问题数", readDB.Model(&model.LibraryIssue{}).Where("status = ?", "open").Count(&openLibraryIssues)},
		{"已关联 Jellyfin 番剧数", readDB.Model(&model.LocalAnime{}).Where("jellyfin_series_id <> ''").Count(&jellyfinSeriesCount)},
		{"已关联 Jellyfin 单集数", readDB.Model(&model.LocalEpisode{}).Where("jellyfin_item_id <> ''").Count(&jellyfinEpisodeCount)},
		{"72 小时无进展订阅数", readDB.Model(&model.Subscription{}).Where("is_active = ? AND stale_after_hours > 0 AND last_success_at IS NOT NULL AND last_success_at < ?", true, time.Now().Add(-72*time.Hour)).Count(&staleSubscriptions)},
	}
	for _, item := range queries {
		if err := item.query.Error; err != nil {
			return fmt.Errorf("查询%s失败: %w", item.name, err)
		}
	}

	playableQuery := readDB.Raw(`
		SELECT COUNT(DISTINCT subscriptions.id)
		FROM subscriptions
		JOIN local_animes ON (local_animes.metadata_id = subscriptions.metadata_id OR local_animes.title = subscriptions.title)
		WHERE local_animes.jellyfin_series_id <> ''
	`).Scan(&playableSubs)
	if err := playableQuery.Error; err != nil {
		return fmt.Errorf("查询可播放订阅数失败: %w", err)
	}
	pendingQuery := readDB.Raw(`
		SELECT COUNT(DISTINCT subscriptions.id)
		FROM subscriptions
		JOIN local_animes ON (local_animes.metadata_id = subscriptions.metadata_id OR local_animes.title = subscriptions.title)
		WHERE (local_animes.jellyfin_series_id = '' OR local_animes.jellyfin_series_id IS NULL)
	`).Scan(&pendingSyncSubs)
	if err := pendingQuery.Error; err != nil {
		return fmt.Errorf("查询待同步订阅数失败: %w", err)
	}

	recommendations := buildRecommendations(cfgMap, downloadingLogs, failedLogs, staleSubscriptions, pendingSyncSubs)
	report := doctorReport{
		DatabasePath: config.AppConfig.Database.Path,
		DataDir:      config.AppPaths.DataDir,
		LogsDir:      config.AppPaths.LogsDir,
		Configs: map[string]bool{
			"qb_url":           hasConfig(cfgMap[model.ConfigKeyQBUrl]),
			"tmdb_token":       hasConfig(cfgMap[model.ConfigKeyTMDBToken]),
			"anilist_token":    hasConfig(cfgMap[model.ConfigKeyAniListToken]),
			"bangumi_token":    hasConfig(cfgMap[model.ConfigKeyBangumiAccessToken]),
			"jellyfin_url":     hasConfig(cfgMap[model.ConfigKeyJellyfinUrl]),
			"jellyfin_api_key": hasConfig(cfgMap[model.ConfigKeyJellyfinApiKey]),
			"r2_bucket":        hasConfig(cfgMap[model.ConfigKeyR2Bucket]),
		},
		Subscriptions: doctorCounts{
			Total:              subscriptionCount,
			Active:             activeSubscription,
			AutoDisabledOnDone: autoDisabledFinished,
		},
		DownloadLogs: doctorLogCounts{
			Completed:   completedLogs,
			Downloading: downloadingLogs,
			Failed:      failedLogs,
			Archived:    archivedLogs,
		},
		LocalLibrary: doctorLibraryInfo{
			AnimeCount:               localAnimeCount,
			EpisodeCount:             localEpisodeCount,
			OpenIssues:               openLibraryIssues,
			JellyfinSeriesCount:      jellyfinSeriesCount,
			JellyfinEpisodeCount:     jellyfinEpisodeCount,
			SubscriptionsPlayable:    playableSubs,
			SubscriptionsPendingSync: pendingSyncSubs,
		},
		StaleCount72H:   staleSubscriptions,
		Recommendations: recommendations,
	}

	if jsonMode {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return fmt.Errorf("输出 JSON 失败: %w", err)
		}
		return nil
	}

	fmt.Println("== AnimateAutoTool Doctor ==")
	fmt.Printf("数据库: %s\n", report.DatabasePath)
	fmt.Printf("数据目录: %s\n", report.DataDir)
	fmt.Printf("日志目录: %s\n", report.LogsDir)
	fmt.Println()

	printConfigCheck("qB URL", cfgMap[model.ConfigKeyQBUrl])
	printConfigCheck("TMDB Token", cfgMap[model.ConfigKeyTMDBToken])
	printConfigCheck("AniList Token", cfgMap[model.ConfigKeyAniListToken])
	printConfigCheck("Bangumi Token", cfgMap[model.ConfigKeyBangumiAccessToken])
	printConfigCheck("Jellyfin URL", cfgMap[model.ConfigKeyJellyfinUrl])
	printConfigCheck("Jellyfin API Key", cfgMap[model.ConfigKeyJellyfinApiKey])
	printConfigCheck("R2 Bucket", cfgMap[model.ConfigKeyR2Bucket])
	fmt.Println()

	fmt.Printf("订阅: %d 总数 / %d 激活 / %d 已完结自动停用\n", report.Subscriptions.Total, report.Subscriptions.Active, report.Subscriptions.AutoDisabledOnDone)
	fmt.Printf("下载日志: completed=%d downloading=%d failed=%d archived=%d\n", report.DownloadLogs.Completed, report.DownloadLogs.Downloading, report.DownloadLogs.Failed, report.DownloadLogs.Archived)
	fmt.Printf("本地媒体库: 番剧 %d / 单集 %d / 打开诊断 %d\n", report.LocalLibrary.AnimeCount, report.LocalLibrary.EpisodeCount, report.LocalLibrary.OpenIssues)
	fmt.Printf("媒体闭环: 可播放订阅 %d / 待同步订阅 %d\n", report.LocalLibrary.SubscriptionsPlayable, report.LocalLibrary.SubscriptionsPendingSync)
	fmt.Printf("长时间无进展订阅(72h+): %d\n", report.StaleCount72H)
	fmt.Println()

	if len(report.Recommendations) > 0 {
		fmt.Println("建议:")
		for _, recommendation := range report.Recommendations {
			fmt.Println("- " + recommendation)
		}
	} else {
		fmt.Println("状态良好：当前没有明显的下载阻塞或长期停滞订阅。")
	}
	return nil
}
func validateReadOnlyDatabase(readDB *gorm.DB) error {
	if readDB == nil {
		return gorm.ErrInvalidDB
	}
	if err := db.CheckIntegrity(readDB); err != nil {
		return fmt.Errorf("完整性检查失败: %w", err)
	}
	version := strings.TrimSpace(db.CurrentSchemaVersion(readDB))
	expectedVersion := strings.TrimSpace(db.LatestSchemaVersion())
	if version == "" {
		return fmt.Errorf("未找到有效 schema 版本")
	}
	if expectedVersion == "" || version != expectedVersion {
		return fmt.Errorf("schema 版本过旧或不受支持：当前 %q，要求 %q", version, expectedVersion)
	}
	for _, table := range []any{
		&db.SchemaMigration{},
		&model.GlobalConfig{},
		&model.Subscription{},
		&model.DownloadLog{},
		&model.LocalAnime{},
		&model.LocalEpisode{},
		&model.LibraryIssue{},
	} {
		if !readDB.Migrator().HasTable(table) {
			return fmt.Errorf("缺少诊断所需的数据表 %T", table)
		}
	}
	return nil
}

func printConfigCheck(label, value string) {
	status := "未配置"
	if hasConfig(value) {
		status = "已配置"
	}
	fmt.Printf("%-18s %s\n", label+":", status)
}

func hasConfig(value string) bool {
	return strings.TrimSpace(value) != ""
}

func buildRecommendations(cfgMap map[string]string, downloadingLogs, failedLogs, staleSubscriptions, pendingSyncSubs int64) []string {
	recommendations := make([]string, 0, 4)
	if downloadingLogs > 0 || failedLogs > 0 {
		recommendations = append(recommendations, "打开首页任务总览，执行一次下载状态修复。")
	}
	if staleSubscriptions > 0 {
		recommendations = append(recommendations, "在订阅页优先处理“长期无进展”或“疑似缺集”的条目。")
	}
	if pendingSyncSubs > 0 {
		recommendations = append(recommendations, "有订阅已经入库但还没变成可播放，建议触发一次 Jellyfin 库刷新。")
	}
	if !hasConfig(cfgMap[model.ConfigKeyJellyfinUrl]) || !hasConfig(cfgMap[model.ConfigKeyJellyfinApiKey]) {
		recommendations = append(recommendations, "如果需要即下即看，请补全 Jellyfin URL 和 API Key。")
	}
	return recommendations
}
