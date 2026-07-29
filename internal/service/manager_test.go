package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
)

type fakeRSSParser struct {
	episodes   []parser.Episode
	err        error
	episodesBy map[string][]parser.Episode
	errByURL   map[string]error
}

func (f fakeRSSParser) Name() string { return "fake" }
func (f fakeRSSParser) Parse(url string) ([]parser.Episode, error) {
	if f.errByURL != nil {
		if err, ok := f.errByURL[url]; ok {
			return nil, err
		}
	}
	if f.episodesBy != nil {
		if episodes, ok := f.episodesBy[url]; ok {
			return episodes, nil
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.episodes, nil
}
func (f fakeRSSParser) Search(keyword string) ([]parser.SearchResult, error) { return nil, nil }
func (f fakeRSSParser) GetSubgroups(bangumiID string) ([]parser.Subgroup, error) {
	return nil, nil
}
func (f fakeRSSParser) GetDashboard(year, season string) (*parser.MikanDashboard, error) {
	return nil, nil
}

type fakeDownloader struct {
	addErr    error
	added     []string
	savePaths []string
	torrents  []downloader.TorrentInfo
	listErr   error
}

func (f *fakeDownloader) Login(username, password string) error { return nil }
func (f *fakeDownloader) AddTorrent(url, savePath, category string, paused bool) error {
	if f.addErr != nil {
		return f.addErr
	}
	f.added = append(f.added, url)
	f.savePaths = append(f.savePaths, savePath)
	return nil
}
func (f *fakeDownloader) Ping() error { return nil }
func (f *fakeDownloader) ListTorrents() ([]downloader.TorrentInfo, error) {
	return append([]downloader.TorrentInfo(nil), f.torrents...), f.listErr
}

type fakeTorrentFetcher struct {
	fakeRSSParser
	filename string
	data     []byte
	fetched  []string
	fetchErr error
}

func (f *fakeTorrentFetcher) FetchTorrentContext(_ context.Context, rawURL string) (string, []byte, error) {
	f.fetched = append(f.fetched, rawURL)
	return f.filename, f.data, f.fetchErr
}

type fakeTorrentFileDownloader struct {
	fakeDownloader
	uploadedFilename string
	uploadedData     []byte
	uploadedSavePath string
	uploadedCategory string
	uploadedPaused   bool
	uploadErr        error
}

func (f *fakeTorrentFileDownloader) AddTorrentFileContext(_ context.Context, filename string, data []byte, savePath, category string, paused bool) error {
	f.uploadedFilename = filename
	f.uploadedData = append([]byte(nil), data...)
	f.uploadedSavePath = savePath
	f.uploadedCategory = category
	f.uploadedPaused = paused
	return f.uploadErr
}

func withServiceTestDB(t *testing.T) {
	t.Helper()

	db.InitDB(":memory:")
	t.Cleanup(func() {
		_ = db.CloseDB()
	})
}

func TestProcessSubscriptionPersistsSuccessState(t *testing.T) {
	withServiceTestDB(t)

	sub := model.Subscription{
		Title:    "Test Show",
		RSSUrl:   "https://example.test/rss",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	down := &fakeDownloader{}
	mgr := &SubscriptionManager{
		RSSParser: fakeRSSParser{
			episodes: []parser.Episode{
				{Title: "[Group] Test Show - 01", EpisodeNum: "01", TorrentURL: "magnet:?xt=urn:btih:test-1"},
			},
		},
		Downloader: down,
		DB:         db.DB,
	}

	mgr.ProcessSubscription(&sub)

	var updated model.Subscription
	if err := db.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("failed to reload subscription: %v", err)
	}

	if updated.LastRunStatus != SubscriptionRunStatusSuccess {
		t.Fatalf("expected success status, got %q", updated.LastRunStatus)
	}
	if updated.LastRunSummary != "新增 1 集待下载" {
		t.Fatalf("unexpected success summary: %q", updated.LastRunSummary)
	}
	if updated.LastNewDownloads != 1 {
		t.Fatalf("expected last_new_downloads=1, got %d", updated.LastNewDownloads)
	}
	if updated.LastDownloadedTitle == "" {
		t.Fatal("expected last downloaded title to be recorded")
	}
	if updated.LastCheckAt == nil || updated.LastSuccessAt == nil {
		t.Fatal("expected check timestamps to be recorded")
	}

	var runLogs []model.SubscriptionRunLog
	if err := db.DB.Where("subscription_id = ?", sub.ID).Find(&runLogs).Error; err != nil {
		t.Fatalf("failed to load run logs: %v", err)
	}
	if len(runLogs) != 1 {
		t.Fatalf("expected 1 run log, got %d", len(runLogs))
	}
	if runLogs[0].Status != SubscriptionRunStatusSuccess {
		t.Fatalf("expected success run log, got %q", runLogs[0].Status)
	}
	if runLogs[0].TriggerSource != "manual" {
		t.Fatalf("expected manual trigger source, got %q", runLogs[0].TriggerSource)
	}
}

func TestProcessSubscriptionRecoversExistingQBTaskAfterFailsResponse(t *testing.T) {
	withServiceTestDB(t)

	sub := model.Subscription{
		Title:    "Recovered Show",
		RSSUrl:   "https://example.test/recovered",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	down := &fakeDownloader{
		addErr: downloader.ErrTorrentRejected,
		torrents: []downloader.TorrentInfo{{
			Hash:        "existing-hash",
			Name:        "[ANi] Recovered Show - 01 [1080P]",
			State:       "uploading",
			ContentPath: "/downloads/Recovered Show/Recovered Show - 01.mkv",
			Progress:    1,
		}},
	}
	mgr := &SubscriptionManager{
		RSSParser: fakeRSSParser{episodes: []parser.Episode{{
			Title:      "[ANi] Recovered Show - 01 [1080P]",
			EpisodeNum: "01",
			TorrentURL: "magnet:?xt=urn:btih:existing-hash",
		}}},
		Downloader: down,
		DB:         db.DB,
	}

	mgr.ProcessSubscription(&sub)

	var logEntry model.DownloadLog
	if err := db.DB.Where("subscription_id = ?", sub.ID).First(&logEntry).Error; err != nil {
		t.Fatalf("expected recovered download log: %v", err)
	}
	if logEntry.InfoHash != "existing-hash" {
		t.Fatalf("expected existing hash, got %q", logEntry.InfoHash)
	}
	if logEntry.Status != downloadLogStatusCompleted {
		t.Fatalf("expected completed status, got %q", logEntry.Status)
	}
	if logEntry.TargetFile != "/downloads/Recovered Show/Recovered Show - 01.mkv" {
		t.Fatalf("unexpected recovered target: %q", logEntry.TargetFile)
	}

	var updated model.Subscription
	if err := db.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if updated.LastRunStatus != SubscriptionRunStatusSuccess {
		t.Fatalf("expected successful recovery, got %q", updated.LastRunStatus)
	}
	if updated.LastRunSummary != "已恢复 1 个 qB 现有任务的下载记录" {
		t.Fatalf("unexpected recovery summary: %q", updated.LastRunSummary)
	}
}

func TestAddTorrentFetchesHTTPFileAndUploadsIt(t *testing.T) {
	t.Parallel()

	fetcher := &fakeTorrentFetcher{
		filename: "episode.torrent",
		data:     []byte("d4:infode"),
	}
	down := &fakeTorrentFileDownloader{}
	mgr := &SubscriptionManager{RSSParser: fetcher, Downloader: down}

	err := mgr.addTorrent(context.Background(), "https://mikanani.me/Download/2026/episode.torrent", "/downloads/show", "Anime", true)
	if err != nil {
		t.Fatalf("add HTTP torrent: %v", err)
	}
	if len(fetcher.fetched) != 1 {
		t.Fatalf("expected one source fetch, got %d", len(fetcher.fetched))
	}
	if down.uploadedFilename != "episode.torrent" || string(down.uploadedData) != "d4:infode" {
		t.Fatalf("unexpected uploaded torrent: filename=%q data=%q", down.uploadedFilename, down.uploadedData)
	}
	if down.uploadedSavePath != "/downloads/show" || down.uploadedCategory != "Anime" || !down.uploadedPaused {
		t.Fatalf("unexpected upload options: path=%q category=%q paused=%v", down.uploadedSavePath, down.uploadedCategory, down.uploadedPaused)
	}
	if len(down.added) != 0 {
		t.Fatalf("HTTP torrent should not use downloader-side URL fetch: %v", down.added)
	}
}

func TestAddTorrentKeepsMagnetOnURLPath(t *testing.T) {
	t.Parallel()

	fetcher := &fakeTorrentFetcher{filename: "unused.torrent", data: []byte("d4:infode")}
	down := &fakeTorrentFileDownloader{}
	mgr := &SubscriptionManager{RSSParser: fetcher, Downloader: down}

	magnet := "magnet:?xt=urn:btih:test"
	if err := mgr.addTorrent(context.Background(), magnet, "/downloads/show", "Anime", false); err != nil {
		t.Fatalf("add magnet: %v", err)
	}
	if len(fetcher.fetched) != 0 || down.uploadedFilename != "" {
		t.Fatal("magnet should not be fetched or uploaded as a torrent file")
	}
	if len(down.added) != 1 || down.added[0] != magnet {
		t.Fatalf("expected magnet URL path, got %v", down.added)
	}
}

func TestConfiguredProxyURLRequiresServiceToggle(t *testing.T) {
	withServiceTestDB(t)
	requireConfig := func(key, value string) {
		t.Helper()
		if err := db.DB.Create(&model.GlobalConfig{Key: key, Value: value}).Error; err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}
	requireConfig(model.ConfigKeyProxyURL, "127.0.0.1:7890")

	if got := configuredProxyURL(model.ConfigKeyProxyMikan); got != "" {
		t.Fatalf("expected disabled Mikan proxy to stay empty, got %q", got)
	}
	requireConfig(model.ConfigKeyProxyMikan, model.ConfigValueTrue)
	if got := configuredProxyURL(model.ConfigKeyProxyMikan); got != "http://127.0.0.1:7890" {
		t.Fatalf("unexpected configured proxy: %q", got)
	}
}

func TestProcessSubscriptionPersistsIdleStateForDuplicates(t *testing.T) {
	withServiceTestDB(t)

	sub := model.Subscription{
		Title:    "Idle Show",
		RSSUrl:   "https://example.test/idle",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}
	if err := db.DB.Create(&model.DownloadLog{
		SubscriptionID: sub.ID,
		Title:          "[Group] Idle Show - 01",
		Status:         "downloading",
	}).Error; err != nil {
		t.Fatalf("failed to seed download log: %v", err)
	}

	mgr := &SubscriptionManager{
		RSSParser: fakeRSSParser{
			episodes: []parser.Episode{
				{Title: "[Group] Idle Show - 01", EpisodeNum: "01", TorrentURL: "magnet:?xt=urn:btih:idle-1"},
			},
		},
		Downloader: &fakeDownloader{},
		DB:         db.DB,
	}

	mgr.ProcessSubscription(&sub)

	var updated model.Subscription
	if err := db.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("failed to reload subscription: %v", err)
	}

	if updated.LastRunStatus != SubscriptionRunStatusIdle {
		t.Fatalf("expected idle status, got %q", updated.LastRunStatus)
	}
	if updated.LastRunSummary == "" {
		t.Fatal("expected idle summary to be recorded")
	}
	if updated.LastNewDownloads != 0 {
		t.Fatalf("expected no new downloads, got %d", updated.LastNewDownloads)
	}
}

func TestProcessSubscriptionDeduplicatesVersionedEpisodes(t *testing.T) {
	withServiceTestDB(t)

	sub := model.Subscription{
		Title:    "Versioned Show",
		RSSUrl:   "https://example.test/versioned",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	down := &fakeDownloader{}
	mgr := &SubscriptionManager{
		RSSParser: fakeRSSParser{
			episodes: []parser.Episode{
				{Title: "[ANi] Versioned Show - 01 [1080P][MP4]", EpisodeNum: "01", TorrentURL: "magnet:?xt=urn:btih:v1"},
				{Title: "[ANi] Versioned Show - 01 [1080P][V2][MP4]", EpisodeNum: "01", TorrentURL: "magnet:?xt=urn:btih:v2"},
				{Title: "[ANi] Versioned Show - 02 [1080P][MP4]", EpisodeNum: "02", TorrentURL: "magnet:?xt=urn:btih:v3"},
			},
		},
		Downloader: down,
		DB:         db.DB,
	}

	mgr.ProcessSubscription(&sub)

	if len(down.added) != 2 {
		t.Fatalf("expected two unique torrents, got %d (%v)", len(down.added), down.added)
	}
	var updated model.Subscription
	if err := db.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("failed to reload subscription: %v", err)
	}
	if updated.LastRunSummary != "新增 2 集待下载，跳过 1 个重复版本" {
		t.Fatalf("unexpected duplicate-aware summary: %q", updated.LastRunSummary)
	}
	var logs []model.DownloadLog
	if err := db.DB.Where("subscription_id = ?", sub.ID).Order("id ASC").Find(&logs).Error; err != nil {
		t.Fatalf("failed to load download logs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected two download logs, got %d", len(logs))
	}
	if logs[0].Title != "[ANi] Versioned Show - 01 [1080P][MP4]" {
		t.Fatalf("expected the first release to win the duplicate slot, got %q", logs[0].Title)
	}
}

func TestProcessSubscriptionAllowsDifferentSubgroupsWhenConfigured(t *testing.T) {
	withServiceTestDB(t)

	sub := model.Subscription{
		Title:              "Multi Group Show",
		RSSUrl:             "https://example.test/multi-group",
		IsActive:           true,
		AllowMultiSubgroup: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	down := &fakeDownloader{}
	mgr := &SubscriptionManager{
		RSSParser: fakeRSSParser{
			episodes: []parser.Episode{
				{Title: "[ANi] Multi Group Show - 01 [1080P]", EpisodeNum: "01", SubGroup: "ANi", TorrentURL: "magnet:?xt=urn:btih:ani"},
				{Title: "[Other] Multi Group Show - 01 [1080P][V2]", EpisodeNum: "01", SubGroup: "Other", TorrentURL: "magnet:?xt=urn:btih:other"},
				{Title: "[ANi] Multi Group Show - 01 [1080P][V2]", EpisodeNum: "01", SubGroup: "ANi", TorrentURL: "magnet:?xt=urn:btih:ani-v2"},
			},
		},
		Downloader: down,
		DB:         db.DB,
	}

	mgr.ProcessSubscription(&sub)

	if len(down.added) != 2 {
		t.Fatalf("expected one release per subgroup, got %d (%v)", len(down.added), down.added)
	}
}

func TestProcessSubscriptionPersistsIdleStateForEmptySubgroupRSS(t *testing.T) {
	withServiceTestDB(t)

	sub := model.Subscription{
		Title:         "Empty Group Show",
		RSSUrl:        "https://example.test/empty-group",
		SubtitleGroup: "ANi",
		IsActive:      true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	mgr := &SubscriptionManager{
		RSSParser:  fakeRSSParser{episodes: []parser.Episode{}},
		Downloader: &fakeDownloader{},
		DB:         db.DB,
	}

	mgr.ProcessSubscription(&sub)

	var updated model.Subscription
	if err := db.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("failed to reload subscription: %v", err)
	}

	if updated.LastRunStatus != SubscriptionRunStatusIdle {
		t.Fatalf("expected idle status, got %q", updated.LastRunStatus)
	}
	if updated.LastRunSummary != "RSS 当前没有可用剧集（字幕组 ANi）" {
		t.Fatalf("unexpected empty subgroup summary: %q", updated.LastRunSummary)
	}
}

func TestBuildIdleRunSummaryDistinguishesCurrentRSSFromHistory(t *testing.T) {
	mgr := &SubscriptionManager{}
	tests := []struct {
		name      string
		total     int
		filtered  int
		duplicate int
		want      string
	}{
		{
			name:      "all resources already tracked",
			total:     3,
			duplicate: 3,
			want:      "本次 RSS 返回 3 条资源，均已存在于历史下载记录",
		},
		{
			name:     "all resources filtered",
			total:    4,
			filtered: 4,
			want:     "本次 RSS 返回 4 条资源，均被过滤规则跳过",
		},
		{
			name:      "mixed existing and filtered resources",
			total:     5,
			filtered:  2,
			duplicate: 3,
			want:      "本次 RSS 返回 5 条资源（过滤 2，已存在 3），未发现新增",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mgr.buildIdleRunSummary(nil, tt.total, tt.filtered, tt.duplicate); got != tt.want {
				t.Fatalf("buildIdleRunSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProcessSubscriptionFallsBackToBackupRSS(t *testing.T) {
	withServiceTestDB(t)

	sub := model.Subscription{
		Title:        "Fallback Show",
		RSSUrl:       "https://example.test/primary",
		BackupRSSUrl: "https://example.test/backup",
		IsActive:     true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	down := &fakeDownloader{}
	mgr := &SubscriptionManager{
		RSSParser: fakeRSSParser{
			episodesBy: map[string][]parser.Episode{
				"https://example.test/primary": {},
				"https://example.test/backup": {
					{Title: "[Alt] Fallback Show - 01", EpisodeNum: "01", TorrentURL: "magnet:?xt=urn:btih:fallback-1"},
				},
			},
		},
		Downloader: down,
		DB:         db.DB,
	}

	mgr.ProcessSubscription(&sub)

	var updated model.Subscription
	if err := db.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("failed to reload subscription: %v", err)
	}
	if updated.LastRunStatus != SubscriptionRunStatusSuccess {
		t.Fatalf("expected success status, got %q", updated.LastRunStatus)
	}
	if !strings.Contains(updated.LastRunSummary, "备用 RSS") {
		t.Fatalf("expected fallback summary to mention backup rss, got %q", updated.LastRunSummary)
	}
	if len(down.added) != 1 {
		t.Fatalf("expected one torrent to be added from backup rss, got %d", len(down.added))
	}
}

func TestProcessSubscriptionAutoDisablesCompletedSeries(t *testing.T) {
	withServiceTestDB(t)

	sub := model.Subscription{
		Title:             "Complete Show",
		RSSUrl:            "https://example.test/complete",
		IsActive:          true,
		LastEp:            12,
		ExpectedEpisodes:  12,
		AutoDisableOnDone: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	mgr := &SubscriptionManager{
		RSSParser:  fakeRSSParser{episodes: []parser.Episode{}},
		Downloader: &fakeDownloader{},
		DB:         db.DB,
	}

	mgr.ProcessSubscription(&sub)

	var updated model.Subscription
	if err := db.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("failed to reload subscription: %v", err)
	}
	if updated.IsActive {
		t.Fatal("expected completed subscription to be auto-disabled")
	}
	if !strings.Contains(updated.LastRunSummary, "自动停用") {
		t.Fatalf("expected summary to mention auto-disable, got %q", updated.LastRunSummary)
	}
}

func TestProcessSubscriptionDiagnosesEmptySubgroupFeedWhenBaseRSSHasEpisodes(t *testing.T) {
	withServiceTestDB(t)

	sub := model.Subscription{
		Title:         "Diag Show",
		RSSUrl:        "https://example.test/rss?bangumiId=1&subgroupid=583",
		SubtitleGroup: "ANi",
		IsActive:      true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	mgr := &SubscriptionManager{
		RSSParser: fakeRSSParser{
			episodesBy: map[string][]parser.Episode{
				"https://example.test/rss?bangumiId=1&subgroupid=583": {},
				"https://example.test/rss?bangumiId=1": {
					{Title: "[Other] Diag Show - 01", EpisodeNum: "01"},
					{Title: "[Other] Diag Show - 02", EpisodeNum: "02"},
				},
			},
		},
		Downloader: &fakeDownloader{},
		DB:         db.DB,
	}

	mgr.ProcessSubscription(&sub)

	var updated model.Subscription
	if err := db.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("failed to reload subscription: %v", err)
	}

	want := "当前字幕组 RSS 为空（ANi），但该番剧主 RSS 还有 2 集可用"
	if updated.LastRunSummary != want {
		t.Fatalf("unexpected diagnostic summary: got %q want %q", updated.LastRunSummary, want)
	}
}

func TestProcessSubscriptionPersistsParseFailure(t *testing.T) {
	withServiceTestDB(t)

	sub := model.Subscription{
		Title:    "Broken Show",
		RSSUrl:   "https://example.test/broken",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	mgr := &SubscriptionManager{
		RSSParser:  fakeRSSParser{err: errors.New("rss unavailable")},
		Downloader: &fakeDownloader{},
		DB:         db.DB,
	}

	mgr.ProcessSubscription(&sub)

	var updated model.Subscription
	if err := db.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("failed to reload subscription: %v", err)
	}

	if updated.LastRunStatus != SubscriptionRunStatusError {
		t.Fatalf("expected error status, got %q", updated.LastRunStatus)
	}
	if updated.LastRunSummary != "RSS 解析失败" {
		t.Fatalf("unexpected parse error summary: %q", updated.LastRunSummary)
	}
	if updated.LastError != "rss unavailable" {
		t.Fatalf("expected parse error to be recorded, got %q", updated.LastError)
	}
	if updated.LastSuccessAt != nil {
		t.Fatal("expected parse failure to keep last success empty")
	}
}

func TestProcessSubscriptionPublishesSubscriptionRunEvent(t *testing.T) {
	withServiceTestDB(t)

	sub := model.Subscription{
		Title:    "Event Show",
		RSSUrl:   "https://example.test/events",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	prevBus := event.GlobalBus
	bus := event.NewInMemoryBus()
	event.GlobalBus = bus
	t.Cleanup(func() {
		event.GlobalBus = prevBus
	})

	received := make(chan event.Event, 1)
	subID := bus.Subscribe(event.EventSubscriptionRun, func(evt event.Event) {
		select {
		case received <- evt:
		default:
		}
	})
	t.Cleanup(func() {
		bus.Unsubscribe(event.EventSubscriptionRun, subID)
	})

	mgr := &SubscriptionManager{
		RSSParser: fakeRSSParser{
			episodes: []parser.Episode{
				{Title: "[Group] Event Show - 01", EpisodeNum: "01", TorrentURL: "magnet:?xt=urn:btih:event-1"},
			},
		},
		Downloader: &fakeDownloader{},
		DB:         db.DB,
	}

	mgr.ProcessSubscription(&sub)

	select {
	case evt := <-received:
		payload, ok := evt.Payload.(map[string]interface{})
		if !ok {
			t.Fatalf("expected map payload, got %T", evt.Payload)
		}
		if payload["status"] != SubscriptionRunStatusSuccess {
			t.Fatalf("expected success event status, got %#v", payload["status"])
		}
		if payload["subscription_id"] != sub.ID {
			t.Fatalf("expected subscription id %d, got %#v", sub.ID, payload["subscription_id"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected subscription run event to be published")
	}
}

func TestProcessSubscriptionWithSourcePersistsRunSource(t *testing.T) {
	withServiceTestDB(t)

	sub := model.Subscription{
		Title:    "Auto Show",
		RSSUrl:   "https://example.test/auto",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	mgr := &SubscriptionManager{
		RSSParser: fakeRSSParser{
			episodes: []parser.Episode{
				{Title: "[Group] Auto Show - 01", EpisodeNum: "01", TorrentURL: "magnet:?xt=urn:btih:auto-1"},
			},
		},
		Downloader: &fakeDownloader{},
		DB:         db.DB,
	}

	mgr.ProcessSubscriptionWithSource(&sub, "auto")

	var runLog model.SubscriptionRunLog
	if err := db.DB.Where("subscription_id = ?", sub.ID).First(&runLog).Error; err != nil {
		t.Fatalf("failed to load run log: %v", err)
	}
	if runLog.TriggerSource != "auto" {
		t.Fatalf("expected auto trigger source, got %q", runLog.TriggerSource)
	}
}

func TestProcessSubscriptionUsesGlobalBaseDirWhenSavePathEmpty(t *testing.T) {
	withServiceTestDB(t)

	if err := db.SaveGlobalConfig(model.ConfigKeyBaseDir, `E:\bangumi`); err != nil {
		t.Fatalf("failed to save base dir config: %v", err)
	}

	sub := model.Subscription{
		Title:    "Path Show",
		RSSUrl:   "https://example.test/path-show",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	down := &fakeDownloader{}
	mgr := &SubscriptionManager{
		RSSParser: fakeRSSParser{
			episodes: []parser.Episode{
				{Title: "[Group] Path Show - 01", EpisodeNum: "01", TorrentURL: "magnet:?xt=urn:btih:path-1"},
			},
		},
		Downloader: down,
		DB:         db.DB,
	}

	mgr.ProcessSubscription(&sub)

	if len(down.savePaths) != 1 {
		t.Fatalf("expected one save path, got %d", len(down.savePaths))
	}
	if down.savePaths[0] != `E:\bangumi\Path Show\Season 01` {
		t.Fatalf("unexpected save path, got %q", down.savePaths[0])
	}
}

func TestProcessSubscriptionFallsBackToFilenameEpisodeForAutomaticRename(t *testing.T) {
	withServiceTestDB(t)

	sub := model.Subscription{Title: "Fallback Episode Season 2", RSSUrl: "https://example.test/fallback-episode", IsActive: true}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	down := &fakeDownloader{}
	mgr := &SubscriptionManager{
		RSSParser: fakeRSSParser{episodes: []parser.Episode{{
			Title:      "[Odd Group] Fallback Episode S02E07 [1080p]",
			TorrentURL: "magnet:?xt=urn:btih:fallback-episode",
		}}},
		Downloader: down,
		DB:         db.DB,
	}

	mgr.ProcessSubscription(&sub)

	var entry model.DownloadLog
	if err := db.DB.Where("subscription_id = ?", sub.ID).First(&entry).Error; err != nil {
		t.Fatalf("load download log: %v", err)
	}
	if entry.Episode != "7" || entry.SeasonVal != "S02" {
		t.Fatalf("unexpected parsed identity episode=%q season=%q", entry.Episode, entry.SeasonVal)
	}
	if len(down.savePaths) != 1 || down.savePaths[0] != "downloads/Fallback Episode/Season 02" {
		t.Fatalf("unexpected structured save path %#v", down.savePaths)
	}
}

func TestProcessSubscriptionPrefersSubscriptionSavePath(t *testing.T) {
	withServiceTestDB(t)

	if err := db.SaveGlobalConfig(model.ConfigKeyBaseDir, `E:\bangumi`); err != nil {
		t.Fatalf("failed to save base dir config: %v", err)
	}

	sub := model.Subscription{
		Title:    "Custom Path Show",
		RSSUrl:   "https://example.test/custom-path",
		SavePath: `D:\manual`,
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	down := &fakeDownloader{}
	mgr := &SubscriptionManager{
		RSSParser: fakeRSSParser{
			episodes: []parser.Episode{
				{Title: "[Group] Custom Path Show - 01", EpisodeNum: "01", TorrentURL: "magnet:?xt=urn:btih:custom-1"},
			},
		},
		Downloader: down,
		DB:         db.DB,
	}

	mgr.ProcessSubscription(&sub)

	if len(down.savePaths) != 1 {
		t.Fatalf("expected one save path, got %d", len(down.savePaths))
	}
	if down.savePaths[0] != `D:\manual\Season 01` {
		t.Fatalf("expected subscription save path to be preferred, got %q", down.savePaths[0])
	}
}
