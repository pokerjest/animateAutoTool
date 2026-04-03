package service

import (
	"errors"
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
)

type fakeRSSParser struct {
	episodes []parser.Episode
	err      error
}

func (f fakeRSSParser) Name() string { return "fake" }
func (f fakeRSSParser) Parse(url string) ([]parser.Episode, error) {
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
	addErr error
	added  []string
}

func (f *fakeDownloader) Login(username, password string) error { return nil }
func (f *fakeDownloader) AddTorrent(url, savePath, category string, paused bool) error {
	if f.addErr != nil {
		return f.addErr
	}
	f.added = append(f.added, url)
	return nil
}
func (f *fakeDownloader) Ping() error { return nil }

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
