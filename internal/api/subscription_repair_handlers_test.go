package api

import (
	"fmt"
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestDeriveBaseRSSURL(t *testing.T) {
	got, ok := deriveBaseRSSURL("https://mikanani.me/RSS/Bangumi?bangumiId=3941&subgroupid=583")
	if !ok {
		t.Fatal("expected subgroup rss to derive base url")
	}
	assert.Equal(t, "https://mikanani.me/RSS/Bangumi?bangumiId=3941", got)
}

func TestApplyBaseRSSFallbackClearsGroupScopedFilter(t *testing.T) {
	sub := &model.Subscription{
		RSSUrl:        "https://mikanani.me/RSS/Bangumi?bangumiId=3941&subgroupid=583",
		SubtitleGroup: "ANi",
		FilterRule:    "ANi",
		LastError:     "old",
	}

	applyBaseRSSFallback(sub, "https://mikanani.me/RSS/Bangumi?bangumiId=3941")

	assert.Equal(t, "https://mikanani.me/RSS/Bangumi?bangumiId=3941", sub.RSSUrl)
	assert.Equal(t, "", sub.SubtitleGroup)
	assert.Equal(t, "", sub.FilterRule)
	assert.Equal(t, "", sub.LastError)
	assert.Contains(t, sub.LastRunSummary, "已切回主 RSS")
}

func TestPopulateSubscriptionActionHintsForEmptySubgroupFeed(t *testing.T) {
	sub := &model.Subscription{
		RSSUrl:         "https://mikanani.me/RSS/Bangumi?bangumiId=3941&subgroupid=583",
		LastRunStatus:  service.SubscriptionRunStatusIdle,
		LastRunSummary: "当前字幕组 RSS 为空（ANi），但该番剧主 RSS 还有 17 集可用",
	}

	populateSubscriptionActionHints(sub)

	assert.True(t, sub.CanUseBaseRSS)
	assert.Equal(t, "https://mikanani.me/RSS/Bangumi?bangumiId=3941", sub.BaseRSSURL)
}

func TestPopulateSubscriptionActionHintsForFilteredFeed(t *testing.T) {
	sub := &model.Subscription{
		FilterRule:     "ANi",
		LastRunStatus:  service.SubscriptionRunStatusIdle,
		LastRunSummary: "检查到 4 集，但都被过滤规则跳过",
	}

	populateSubscriptionActionHints(sub)

	assert.True(t, sub.CanClearFilter)
}

func TestUseBaseRSSHandlerTriggersAutoRecheck(t *testing.T) {
	if err := db.DB.Exec("DELETE FROM subscriptions").Error; err != nil {
		t.Fatalf("failed to clear subscriptions: %v", err)
	}

	sub := model.Subscription{
		Title:          "Fallback Show",
		RSSUrl:         "https://mikanani.me/RSS/Bangumi?bangumiId=3941&subgroupid=583",
		SubtitleGroup:  "ANi",
		FilterRule:     "ANi",
		LastRunStatus:  service.SubscriptionRunStatusIdle,
		LastRunSummary: "当前字幕组 RSS 为空（ANi），但该番剧主 RSS 还有 17 集可用",
		IsActive:       true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	prev := runSubscriptionCheck
	called := false
	runSubscriptionCheck = func(sub *model.Subscription, source string) error {
		called = true
		if source != "manual" {
			t.Fatalf("expected manual source, got %q", source)
		}
		sub.LastRunStatus = service.SubscriptionRunStatusSuccess
		sub.LastRunSummary = "新增 1 集待下载"
		return nil
	}
	t.Cleanup(func() {
		runSubscriptionCheck = prev
	})

	err := useBaseRSSAndRecheck(&sub, "https://mikanani.me/RSS/Bangumi?bangumiId=3941")
	assert.NoError(t, err)
	assert.True(t, called)

	var updated model.Subscription
	if err := db.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("failed to reload subscription: %v", err)
	}

	assert.Equal(t, "https://mikanani.me/RSS/Bangumi?bangumiId=3941", updated.RSSUrl)
	assert.Equal(t, "", updated.SubtitleGroup)
	assert.Equal(t, "", updated.FilterRule)
}

func TestUseBaseRSSAndRecheckRecordsConsistentFailureSummary(t *testing.T) {
	if err := db.DB.Exec("DELETE FROM subscriptions").Error; err != nil {
		t.Fatalf("failed to clear subscriptions: %v", err)
	}

	sub := model.Subscription{
		Title:          "Fallback Show",
		RSSUrl:         "https://mikanani.me/RSS/Bangumi?bangumiId=3941&subgroupid=583",
		SubtitleGroup:  "ANi",
		FilterRule:     "ANi",
		LastRunStatus:  service.SubscriptionRunStatusIdle,
		LastRunSummary: "当前字幕组 RSS 为空（ANi），但该番剧主 RSS 还有 17 集可用",
		IsActive:       true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	prev := runSubscriptionCheck
	runSubscriptionCheck = func(sub *model.Subscription, source string) error {
		return fmt.Errorf("qb offline")
	}
	t.Cleanup(func() {
		runSubscriptionCheck = prev
	})

	err := useBaseRSSAndRecheck(&sub, "https://mikanani.me/RSS/Bangumi?bangumiId=3941")
	assert.NoError(t, err)

	var updated model.Subscription
	if err := db.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("failed to reload subscription: %v", err)
	}

	assert.Equal(t, service.SubscriptionRunStatusIdle, updated.LastRunStatus)
	assert.Equal(t, "已切回主 RSS，但自动重检未执行", updated.LastRunSummary)
	assert.Equal(t, "qb offline", updated.LastError)
}

func TestClearFilterAndRecheckClearsFilterAndTriggersRun(t *testing.T) {
	if err := db.DB.Exec("DELETE FROM subscriptions").Error; err != nil {
		t.Fatalf("failed to clear subscriptions: %v", err)
	}

	sub := model.Subscription{
		Title:          "Filter Show",
		RSSUrl:         "https://example.test/filter-show",
		FilterRule:     "ANi",
		LastRunStatus:  service.SubscriptionRunStatusIdle,
		LastRunSummary: "检查到 4 集，但都被过滤规则跳过",
		IsActive:       true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	prev := runSubscriptionCheck
	called := false
	runSubscriptionCheck = func(sub *model.Subscription, source string) error {
		called = true
		sub.LastRunStatus = service.SubscriptionRunStatusSuccess
		sub.LastRunSummary = "新增 4 集待下载"
		return nil
	}
	t.Cleanup(func() {
		runSubscriptionCheck = prev
	})

	err := clearFilterAndRecheck(&sub)
	assert.NoError(t, err)
	assert.True(t, called)

	var updated model.Subscription
	if err := db.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("failed to reload subscription: %v", err)
	}

	assert.Equal(t, "", updated.FilterRule)
}

func TestClearFilterAndRecheckRecordsConsistentFailureSummary(t *testing.T) {
	if err := db.DB.Exec("DELETE FROM subscriptions").Error; err != nil {
		t.Fatalf("failed to clear subscriptions: %v", err)
	}

	sub := model.Subscription{
		Title:          "Filter Show",
		RSSUrl:         "https://example.test/filter-show",
		FilterRule:     "ANi",
		LastRunStatus:  service.SubscriptionRunStatusIdle,
		LastRunSummary: "检查到 4 集，但都被过滤规则跳过",
		IsActive:       true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	prev := runSubscriptionCheck
	runSubscriptionCheck = func(sub *model.Subscription, source string) error {
		return fmt.Errorf("qb offline")
	}
	t.Cleanup(func() {
		runSubscriptionCheck = prev
	})

	err := clearFilterAndRecheck(&sub)
	assert.NoError(t, err)

	var updated model.Subscription
	if err := db.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("failed to reload subscription: %v", err)
	}

	assert.Equal(t, service.SubscriptionRunStatusIdle, updated.LastRunStatus)
	assert.Equal(t, "已清空过滤规则，但自动重检未执行", updated.LastRunSummary)
	assert.Equal(t, "qb offline", updated.LastError)
}

func TestPopulateSubscriptionActionHintsForStaleDuplicateLogs(t *testing.T) {
	if err := db.DB.Exec("DELETE FROM subscriptions").Error; err != nil {
		t.Fatalf("failed to clear subscriptions: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM download_logs").Error; err != nil {
		t.Fatalf("failed to clear download logs: %v", err)
	}

	sub := model.Subscription{
		Title:          "Blocked Show",
		RSSUrl:         "https://example.test/blocked",
		LastRunStatus:  service.SubscriptionRunStatusIdle,
		LastRunSummary: "检查到 2 集，但都已经在下载记录中",
		IsActive:       true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	logEntry := model.DownloadLog{
		SubscriptionID: sub.ID,
		Title:          "[Group] Blocked Show - 01",
		Status:         "downloading",
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("failed to create log: %v", err)
	}
	oldCreatedAt := time.Now().Add(-48 * time.Hour)
	if err := db.DB.Model(&model.DownloadLog{}).Where("id = ?", logEntry.ID).Update("created_at", oldCreatedAt).Error; err != nil {
		t.Fatalf("failed to age log: %v", err)
	}

	populateSubscriptionActionHints(&sub)

	assert.True(t, sub.CanResetStaleLogs)
	assert.Contains(t, sub.StrategyHint, "卡住超过 24 小时")
}

func TestPopulateSubscriptionActionHintsIncludesEpisodeProgressHint(t *testing.T) {
	sub := model.Subscription{
		Title:            "Progress Show",
		RSSUrl:           "https://example.test/progress",
		ExpectedEpisodes: 12,
		LastEp:           8,
		StaleAfterHours:  0,
	}

	populateSubscriptionActionHints(&sub)

	assert.Contains(t, sub.StrategyHint, "当前已追到 8 / 12 集")
}

func TestPopulateSubscriptionActionHintsIncludesMissingEpisodeHint(t *testing.T) {
	if err := db.DB.Exec("DELETE FROM subscriptions").Error; err != nil {
		t.Fatalf("failed to clear subscriptions: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM download_logs").Error; err != nil {
		t.Fatalf("failed to clear download logs: %v", err)
	}

	sub := model.Subscription{
		Title:            "Gap Show",
		RSSUrl:           "https://example.test/gap",
		ExpectedEpisodes: 4,
		LastEp:           4,
		IsActive:         true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}
	logs := []model.DownloadLog{
		{SubscriptionID: sub.ID, Title: "Gap Show - 01", Episode: "1", Status: "completed"},
		{SubscriptionID: sub.ID, Title: "Gap Show - 03", Episode: "3", Status: "completed"},
	}
	if err := db.DB.Create(&logs).Error; err != nil {
		t.Fatalf("failed to create logs: %v", err)
	}

	populateSubscriptionActionHints(&sub)

	assert.Contains(t, sub.StrategyHint, "疑似缺集")
	assert.Contains(t, sub.StrategyHint, "02")
	assert.Contains(t, sub.StrategyHint, "04")
	assert.True(t, sub.CanRetryMissing)
	assert.Equal(t, "疑似缺集", sub.LifecycleStage)
	assert.Equal(t, "warning", sub.LifecycleTone)
}

func TestPopulateSubscriptionActionHintsMarksStaleLifecycle(t *testing.T) {
	now := time.Now().Add(-240 * time.Hour)
	sub := model.Subscription{
		Title:           "Quiet Show",
		RSSUrl:          "https://example.test/quiet",
		StaleAfterHours: 24,
		LastSuccessAt:   &now,
		IsActive:        true,
	}

	populateSubscriptionActionHints(&sub)

	assert.True(t, sub.CanRetryStale)
	assert.Equal(t, "长期无进展", sub.LifecycleStage)
	assert.Equal(t, "warning", sub.LifecycleTone)
	assert.Contains(t, sub.StrategyHint, "超过 24 小时没有出现新进展")
}

func TestPopulateSubscriptionActionHintsIncludesLibraryAndUpgradeState(t *testing.T) {
	if err := db.DB.Exec("DELETE FROM subscriptions").Error; err != nil {
		t.Fatalf("failed to clear subscriptions: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM anime_metadata").Error; err != nil {
		t.Fatalf("failed to clear metadata: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM local_animes").Error; err != nil {
		t.Fatalf("failed to clear local anime: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM local_episodes").Error; err != nil {
		t.Fatalf("failed to clear local episodes: %v", err)
	}

	metadata := model.AnimeMetadata{Title: "Library Show", BangumiID: 5566}
	if err := db.DB.Create(&metadata).Error; err != nil {
		t.Fatalf("failed to create metadata: %v", err)
	}
	sub := model.Subscription{
		Title:      "Library Show",
		RSSUrl:     "https://example.test/library",
		IsActive:   true,
		MetadataID: &metadata.ID,
		Metadata:   &metadata,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}
	localAnime := model.LocalAnime{
		Title:            "Library Show",
		MetadataID:       &metadata.ID,
		JellyfinSeriesID: "jf-series-1",
	}
	if err := db.DB.Create(&localAnime).Error; err != nil {
		t.Fatalf("failed to create local anime: %v", err)
	}
	episodes := []model.LocalEpisode{
		{LocalAnimeID: localAnime.ID, EpisodeNum: 1, Resolution: "720p", JellyfinItemID: "ep1", Path: "/tmp/library-show-01.mkv"},
		{LocalAnimeID: localAnime.ID, EpisodeNum: 2, Resolution: "1080p", JellyfinItemID: "ep2", Path: "/tmp/library-show-02.mkv"},
	}
	if err := db.DB.Create(&episodes).Error; err != nil {
		t.Fatalf("failed to create local episodes: %v", err)
	}

	populateSubscriptionActionHints(&sub)

	assert.Equal(t, "可播放", sub.LibraryStage)
	assert.Equal(t, "success", sub.LibraryTone)
	assert.Contains(t, sub.LibraryHint, "Jellyfin 已建立条目")
	assert.True(t, sub.CanRetryUpgrade)
	assert.Contains(t, sub.StrategyHint, "较低分辨率")
	assert.False(t, sub.HasRepairActions, "optional quality upgrades must not be reported as subscription failures")
}

func TestCollectUpgradeableEpisodesIgnoresUnknownResolution(t *testing.T) {
	episodes := []model.LocalEpisode{
		{EpisodeNum: 1, Resolution: ""},
		{EpisodeNum: 2, Resolution: "unknown"},
		{EpisodeNum: 3, Resolution: "720p"},
		{EpisodeNum: 4, Resolution: "1080p"},
	}

	assert.Equal(t, []int{3}, collectUpgradeableEpisodes(episodes))
}

func TestPopulateSubscriptionActionHintsMarksLibraryPendingSyncWhenJellyfinConfigured(t *testing.T) {
	if err := db.DB.Exec("DELETE FROM subscriptions").Error; err != nil {
		t.Fatalf("failed to clear subscriptions: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM anime_metadata").Error; err != nil {
		t.Fatalf("failed to clear metadata: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM local_animes").Error; err != nil {
		t.Fatalf("failed to clear local anime: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM local_episodes").Error; err != nil {
		t.Fatalf("failed to clear local episodes: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM global_configs WHERE key IN (?, ?)", model.ConfigKeyJellyfinUrl, model.ConfigKeyJellyfinApiKey).Error; err != nil {
		t.Fatalf("failed to clear jellyfin config: %v", err)
	}
	if err := db.DB.Create(&model.GlobalConfig{Key: model.ConfigKeyJellyfinUrl, Value: "http://jf.local"}).Error; err != nil {
		t.Fatalf("failed to seed jellyfin url: %v", err)
	}
	if err := db.DB.Create(&model.GlobalConfig{Key: model.ConfigKeyJellyfinApiKey, Value: "token"}).Error; err != nil {
		t.Fatalf("failed to seed jellyfin key: %v", err)
	}

	metadata := model.AnimeMetadata{Title: "Pending Sync Show", BangumiID: 7788}
	if err := db.DB.Create(&metadata).Error; err != nil {
		t.Fatalf("failed to create metadata: %v", err)
	}
	sub := model.Subscription{
		Title:      "Pending Sync Show",
		RSSUrl:     "https://example.test/pending-sync",
		IsActive:   true,
		MetadataID: &metadata.ID,
		Metadata:   &metadata,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}
	localAnime := model.LocalAnime{
		Title:      "Pending Sync Show",
		MetadataID: &metadata.ID,
	}
	if err := db.DB.Create(&localAnime).Error; err != nil {
		t.Fatalf("failed to create local anime: %v", err)
	}
	episode := model.LocalEpisode{
		LocalAnimeID: localAnime.ID,
		EpisodeNum:   1,
		Resolution:   "1080p",
		Path:         "/tmp/pending-sync-01.mkv",
	}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatalf("failed to create local episode: %v", err)
	}

	populateSubscriptionActionHints(&sub)

	assert.Equal(t, "待同步到媒体库", sub.LibraryStage)
	assert.Equal(t, "warning", sub.LibraryTone)
	assert.Contains(t, sub.LibraryHint, "建议触发一次库刷新")
	assert.True(t, sub.CanRefreshLibrary)
	assert.False(t, sub.HasRepairActions, "a media-server refresh recommendation must not create a permanent issue")
}

func TestRenderSubscriptionCardTemplateIncludesLifecycleAndRepairActions(t *testing.T) {
	html, err := renderTemplateToString("subscription_card.html", model.Subscription{
		Model:            gorm.Model{ID: 42},
		Title:            "Repair Show",
		RSSUrl:           "https://example.test/rss",
		ExpectedEpisodes: 12,
		LifecycleStage:   "疑似缺集",
		LifecycleTone:    "warning",
		CanRetryMissing:  true,
		CanRetryStale:    true,
		HasRepairActions: true,
		StrategyHint:     "疑似缺集：02、04。 已经超过 24 小时没有出现新进展。",
	})
	if err != nil {
		t.Fatalf("failed to render template: %v", err)
	}

	assert.Contains(t, html, "疑似缺集")
	assert.Contains(t, html, "补缺集重检")
	assert.Contains(t, html, "重新检查")
}

func TestRenderSubscriptionCardTemplateIncludesLibraryStateAndUpgradeAction(t *testing.T) {
	html, err := renderTemplateToString("subscription_card.html", model.Subscription{
		Model:            gorm.Model{ID: 77},
		Title:            "Upgrade Show",
		RSSUrl:           "https://example.test/upgrade",
		LibraryStage:     "可播放",
		LibraryTone:      "success",
		LibraryHint:      "本地已入库 2 集，Jellyfin 已建立条目，可直接播放。",
		CanRetryUpgrade:  true,
		HasRepairActions: true,
	})
	if err != nil {
		t.Fatalf("failed to render template: %v", err)
	}

	assert.Contains(t, html, "可播放")
	assert.Contains(t, html, "洗版检查")
	assert.Contains(t, html, "Jellyfin 已建立条目")
}

func TestRenderSubscriptionCardTemplateIncludesPendingLibraryRefreshAction(t *testing.T) {
	html, err := renderTemplateToString("subscription_card.html", model.Subscription{
		Model:             gorm.Model{ID: 88},
		Title:             "Pending Library Show",
		RSSUrl:            "https://example.test/pending-library",
		LibraryStage:      "待同步到媒体库",
		LibraryTone:       "warning",
		LibraryHint:       "本地已经识别 3 集，但 Jellyfin 还没有建立条目；建议触发一次库刷新。",
		CanRefreshLibrary: true,
		HasRepairActions:  true,
	})
	if err != nil {
		t.Fatalf("failed to render template: %v", err)
	}

	assert.Contains(t, html, "待同步到媒体库")
	assert.Contains(t, html, "刷新媒体库")
	assert.Contains(t, html, "/api/subscriptions/88/refresh-library")
}

func TestResetStaleLogsAndRecheckArchivesOldBlockingLogs(t *testing.T) {
	if err := db.DB.Exec("DELETE FROM subscriptions").Error; err != nil {
		t.Fatalf("failed to clear subscriptions: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM download_logs").Error; err != nil {
		t.Fatalf("failed to clear download logs: %v", err)
	}

	sub := model.Subscription{
		Title:          "Blocked Show",
		RSSUrl:         "https://example.test/blocked",
		LastRunStatus:  service.SubscriptionRunStatusIdle,
		LastRunSummary: "检查到 2 集，但都已经在下载记录中",
		IsActive:       true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	logEntry := model.DownloadLog{
		SubscriptionID: sub.ID,
		Title:          "[Group] Blocked Show - 01",
		Status:         "downloading",
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("failed to create log: %v", err)
	}
	oldCreatedAt := time.Now().Add(-48 * time.Hour)
	if err := db.DB.Model(&model.DownloadLog{}).Where("id = ?", logEntry.ID).Update("created_at", oldCreatedAt).Error; err != nil {
		t.Fatalf("failed to age log: %v", err)
	}

	prev := runSubscriptionCheck
	called := false
	runSubscriptionCheck = func(sub *model.Subscription, source string) error {
		called = true
		sub.LastRunStatus = service.SubscriptionRunStatusSuccess
		sub.LastRunSummary = "新增 2 集待下载"
		return nil
	}
	t.Cleanup(func() {
		runSubscriptionCheck = prev
	})

	err := resetStaleLogsAndRecheck(&sub, 24*time.Hour)
	assert.NoError(t, err)
	assert.True(t, called)

	var updatedLog model.DownloadLog
	if err := db.DB.First(&updatedLog, logEntry.ID).Error; err != nil {
		t.Fatalf("failed to reload log: %v", err)
	}
	assert.Equal(t, "archived", updatedLog.Status)
}

func TestResetStaleLogsAndRecheckRecordsConsistentFailureSummary(t *testing.T) {
	if err := db.DB.Exec("DELETE FROM subscriptions").Error; err != nil {
		t.Fatalf("failed to clear subscriptions: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM download_logs").Error; err != nil {
		t.Fatalf("failed to clear download logs: %v", err)
	}

	sub := model.Subscription{
		Title:          "Blocked Show",
		RSSUrl:         "https://example.test/blocked",
		LastRunStatus:  service.SubscriptionRunStatusIdle,
		LastRunSummary: "检查到 2 集，但都已经在下载记录中",
		IsActive:       true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	logEntry := model.DownloadLog{
		SubscriptionID: sub.ID,
		Title:          "[Group] Blocked Show - 01",
		Status:         "downloading",
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("failed to create log: %v", err)
	}
	oldCreatedAt := time.Now().Add(-48 * time.Hour)
	if err := db.DB.Model(&model.DownloadLog{}).Where("id = ?", logEntry.ID).Update("created_at", oldCreatedAt).Error; err != nil {
		t.Fatalf("failed to age log: %v", err)
	}

	prev := runSubscriptionCheck
	runSubscriptionCheck = func(sub *model.Subscription, source string) error {
		return fmt.Errorf("qb offline")
	}
	t.Cleanup(func() {
		runSubscriptionCheck = prev
	})

	err := resetStaleLogsAndRecheck(&sub, 24*time.Hour)
	assert.NoError(t, err)

	var updated model.Subscription
	if err := db.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("failed to reload subscription: %v", err)
	}

	assert.Equal(t, service.SubscriptionRunStatusIdle, updated.LastRunStatus)
	assert.Equal(t, "已清理陈旧下载记录，但自动重检未执行", updated.LastRunSummary)
	assert.Equal(t, "qb offline", updated.LastError)
}
