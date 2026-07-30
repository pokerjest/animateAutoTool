package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
)

func TestDiscoverSubscriptionResourcesKeepsV2AsExplicitCandidate(t *testing.T) {
	withServiceTestDB(t)
	sub := model.Subscription{
		Title:    "Candidate Show",
		RSSUrl:   "https://example.test/candidates",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	down := &fakeDownloader{}
	manager := &SubscriptionManager{
		RSSParser: fakeRSSParser{episodes: []parser.Episode{
			{Title: "[Group] Candidate Show - 01 [V2]", EpisodeNum: "01", TorrentURL: "magnet:?xt=urn:btih:candidate-v2"},
			{Title: "[Group] Candidate Show - 01", EpisodeNum: "01", TorrentURL: "magnet:?xt=urn:btih:candidate-v1"},
			{Title: "[Group] Candidate Show - 02", EpisodeNum: "02", TorrentURL: "magnet:?xt=urn:btih:candidate-2"},
		}},
		Downloader: down,
		DB:         db.DB,
	}

	result, err := manager.DiscoverSubscriptionResourcesContext(context.Background(), &sub)
	if err != nil {
		t.Fatalf("discover resources: %v", err)
	}
	if len(down.added) != 0 {
		t.Fatalf("discovery must not submit downloads, got %v", down.added)
	}
	if result.RSSCount != 3 || result.CanonicalCount != 2 {
		t.Fatalf("unexpected discovery summary: %+v", result)
	}

	var resources []model.SubscriptionResource
	if err := db.DB.Where("subscription_id = ?", sub.ID).Order("candidate_rank ASC").Find(&resources).Error; err != nil {
		t.Fatalf("load resources: %v", err)
	}
	if len(resources) != 3 {
		t.Fatalf("expected three candidate rows, got %d", len(resources))
	}
	var v1, v2 *model.SubscriptionResource
	for i := range resources {
		switch resources[i].VersionTag {
		case "V1":
			if resources[i].Episode == "1" || resources[i].Episode == "01" {
				v1 = &resources[i]
			}
		case "V2":
			v2 = &resources[i]
		}
	}
	if v1 == nil || !v1.Selected || v1.State != SubscriptionResourceStateSeen {
		t.Fatalf("expected V1 to remain selected, got %+v", v1)
	}
	if v2 == nil || v2.Selected || v2.State != SubscriptionResourceStateSuperseded {
		t.Fatalf("expected V2 to remain an explicit candidate, got %+v", v2)
	}
}

func TestProcessSubscriptionPreflightAvoidsDuplicateSubmit(t *testing.T) {
	withServiceTestDB(t)
	sub := model.Subscription{
		Title:    "Existing Task Show",
		RSSUrl:   "https://example.test/existing-task",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	down := &fakeDownloader{torrents: []downloader.TorrentInfo{{
		Name:     "[Group] Existing Task Show - 01",
		Hash:     "existing-task-hash",
		State:    "downloading",
		SavePath: "downloads/Existing Task Show/Season 01",
	}}}
	manager := &SubscriptionManager{
		RSSParser: fakeRSSParser{episodes: []parser.Episode{{
			Title:      "[Group] Existing Task Show - 01",
			EpisodeNum: "01",
			TorrentURL: "magnet:?xt=urn:btih:existing-task-hash",
		}}},
		Downloader: down,
		DB:         db.DB,
	}

	manager.ProcessSubscription(&sub)
	if len(down.added) != 0 {
		t.Fatalf("preflight should not resubmit an existing qB task, got %v", down.added)
	}
	var logEntry model.DownloadLog
	if err := db.DB.Where("subscription_id = ?", sub.ID).First(&logEntry).Error; err != nil {
		t.Fatalf("expected compatibility log to be rebuilt: %v", err)
	}
	if logEntry.InfoHash != "existing-task-hash" {
		t.Fatalf("expected existing qB hash, got %q", logEntry.InfoHash)
	}
}

func TestProcessSubscriptionFailureDoesNotAutoUpgradeToV2(t *testing.T) {
	withServiceTestDB(t)
	sub := model.Subscription{
		Title:    "Conservative Upgrade Show",
		RSSUrl:   "https://example.test/conservative-upgrade",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	down := &fakeDownloader{addErr: errors.New("Fails.")}
	manager := &SubscriptionManager{
		RSSParser: fakeRSSParser{episodes: []parser.Episode{
			{Title: "[Group] Conservative Upgrade Show - 01", EpisodeNum: "01", TorrentURL: "magnet:?xt=urn:btih:conservative-v1"},
			{Title: "[Group] Conservative Upgrade Show - 01 [V2]", EpisodeNum: "01", TorrentURL: "magnet:?xt=urn:btih:conservative-v2"},
		}},
		Downloader: down,
		DB:         db.DB,
	}

	manager.ProcessSubscription(&sub)
	if down.attempts != 1 {
		t.Fatalf("expected only V1 to be attempted, got %d attempts", down.attempts)
	}
	var resources []model.SubscriptionResource
	if err := db.DB.Where("subscription_id = ?", sub.ID).Find(&resources).Error; err != nil {
		t.Fatalf("load resources: %v", err)
	}
	var failed, candidate bool
	for _, resource := range resources {
		failed = failed || (resource.VersionTag == "V1" && resource.State == SubscriptionResourceStateFailed)
		candidate = candidate || (resource.VersionTag == "V2" && resource.State == SubscriptionResourceStateSuperseded)
	}
	if !failed || !candidate {
		t.Fatalf("expected failed V1 and retained V2 candidate, got %+v", resources)
	}
}

func TestConcurrentSubscriptionRunsSubmitCanonicalEpisodeOnce(t *testing.T) {
	withServiceTestDB(t)
	sub := model.Subscription{
		Title:    "Concurrent Show",
		RSSUrl:   "https://example.test/concurrent",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	down := &fakeDownloader{}
	manager := &SubscriptionManager{
		RSSParser: fakeRSSParser{episodes: []parser.Episode{{
			Title:      "[Group] Concurrent Show - 01",
			EpisodeNum: "01",
			TorrentURL: "magnet:?xt=urn:btih:concurrent-1",
		}}},
		Downloader: down,
		DB:         db.DB,
	}
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			manager.ProcessSubscription(&sub)
		}()
	}
	wg.Wait()
	if down.attempts != 1 {
		t.Fatalf("expected one serialized qB submission, got %d", down.attempts)
	}
}

func TestRefreshAndRepairBackfillsMissingWithoutArchiving(t *testing.T) {
	withServiceTestDB(t)
	sub := model.Subscription{
		Title:    "Refresh Ledger Show",
		RSSUrl:   "https://example.test/refresh-ledger",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	oldLog := model.DownloadLog{
		SubscriptionID: sub.ID,
		Title:          "Refresh Ledger Show - 09",
		Episode:        "09",
		Status:         downloadLogStatusFailed,
	}
	if err := db.DB.Create(&oldLog).Error; err != nil {
		t.Fatalf("create old log: %v", err)
	}
	source := &refreshTestDownloader{
		fakeDownloader: fakeDownloader{},
		episodes: []parser.Episode{{
			Title:      "[Group] Refresh Ledger Show - 01",
			EpisodeNum: "01",
			TorrentURL: "magnet:?xt=urn:btih:refresh-ledger-1",
		}},
	}
	originalFactory := newSubscriptionManagerForRefresh
	newSubscriptionManagerForRefresh = func(downloader.Downloader) *SubscriptionManager {
		return &SubscriptionManager{
			RSSParser:  fakeRSSParser{episodes: source.episodes},
			Downloader: source,
			DB:         db.DB,
		}
	}
	t.Cleanup(func() { newSubscriptionManagerForRefresh = originalFactory })

	result, err := RefreshAndRepairSubscriptions(context.Background(), source, nil)
	if err != nil {
		t.Fatalf("refresh and repair: %v", err)
	}
	if len(source.added) != 1 {
		t.Fatalf("refresh should submit the genuinely missing episode once, got %v", source.added)
	}
	if result.Discovered != 1 || result.CanonicalCount != 1 || result.AutoSubmitted != 1 {
		t.Fatalf("unexpected refresh result: %+v", result)
	}
	var got model.DownloadLog
	if err := db.DB.First(&got, oldLog.ID).Error; err != nil {
		t.Fatalf("reload old log: %v", err)
	}
	if got.Status == downloadLogStatusArchived {
		t.Fatalf("refresh must not archive old logs")
	}
}

func TestRefreshAndRepairSubmitsOnlyMissingCanonicalEpisodes(t *testing.T) {
	withServiceTestDB(t)
	sub := model.Subscription{
		Title:    "Missing Episode Show",
		RSSUrl:   "https://example.test/missing-episode",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	// Episode 01 is already confirmed in the legacy compatibility log. The
	// refresh must leave it alone and submit only the genuinely missing 02.
	if err := db.DB.Create(&model.DownloadLog{
		SubscriptionID: sub.ID,
		Title:          "[Group] Missing Episode Show - 01",
		Episode:        "01",
		SeasonVal:      "S01",
		Status:         downloadLogStatusCompleted,
		InfoHash:       "missing-episode-1",
		TargetFile:     "/library/Missing Episode Show/01.mkv",
	}).Error; err != nil {
		t.Fatalf("create completed log: %v", err)
	}

	source := &refreshTestDownloader{
		fakeDownloader: fakeDownloader{},
		episodes: []parser.Episode{
			{Title: "[Group] Missing Episode Show - 01", EpisodeNum: "01", TorrentURL: "magnet:?xt=urn:btih:missing-episode-1"},
			{Title: "[Group] Missing Episode Show - 01 [V2]", EpisodeNum: "01", TorrentURL: "magnet:?xt=urn:btih:missing-episode-1-v2"},
			{Title: "[Group] Missing Episode Show - 02", EpisodeNum: "02", TorrentURL: "magnet:?xt=urn:btih:missing-episode-2"},
		},
	}
	originalFactory := newSubscriptionManagerForRefresh
	newSubscriptionManagerForRefresh = func(downloader.Downloader) *SubscriptionManager {
		return &SubscriptionManager{
			RSSParser:  fakeRSSParser{episodes: source.episodes},
			Downloader: source,
			DB:         db.DB,
		}
	}
	t.Cleanup(func() { newSubscriptionManagerForRefresh = originalFactory })

	result, err := RefreshAndRepairSubscriptions(context.Background(), source, nil)
	if err != nil {
		t.Fatalf("refresh and repair: %v", err)
	}
	if len(source.added) != 1 || source.added[0] != "magnet:?xt=urn:btih:missing-episode-2" {
		t.Fatalf("expected only missing episode 02 to be submitted, got %v", source.added)
	}
	if result.AutoSubmitted != 1 {
		t.Fatalf("expected one automatic submission, got %+v", result)
	}

	var resources []model.SubscriptionResource
	if err := db.DB.Where("subscription_id = ?", sub.ID).Find(&resources).Error; err != nil {
		t.Fatalf("load resources: %v", err)
	}
	var completed, candidate, submitted bool
	for _, resource := range resources {
		switch {
		case resource.Episode == "01" && resource.State == SubscriptionResourceStateCompleted:
			completed = true
		case resource.VersionTag == "V2" && resource.State == SubscriptionResourceStateSuperseded && !resource.Selected:
			candidate = true
		case resource.Episode == "02" && resource.State == SubscriptionResourceStateDownloading:
			submitted = true
		}
	}
	if !completed || !candidate || !submitted {
		t.Fatalf("unexpected resource reconciliation result: %+v", resources)
	}
}

func TestRefreshQBFailureDoesNotMarkMissingCandidateFailed(t *testing.T) {
	withServiceTestDB(t)
	sub := model.Subscription{
		Title:    "Offline qB Show",
		RSSUrl:   "https://example.test/offline-qb",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	source := &refreshTestDownloader{
		fakeDownloader: fakeDownloader{listErr: errors.New("qB unavailable")},
		episodes: []parser.Episode{{
			Title:      "[Group] Offline qB Show - 01",
			EpisodeNum: "01",
			TorrentURL: "magnet:?xt=urn:btih:offline-qb-1",
		}},
	}
	originalFactory := newSubscriptionManagerForRefresh
	newSubscriptionManagerForRefresh = func(downloader.Downloader) *SubscriptionManager {
		return &SubscriptionManager{
			RSSParser:  fakeRSSParser{episodes: source.episodes},
			Downloader: source,
			DB:         db.DB,
		}
	}
	t.Cleanup(func() { newSubscriptionManagerForRefresh = originalFactory })

	result, err := RefreshAndRepairSubscriptions(context.Background(), source, nil)
	if err != nil {
		t.Fatalf("refresh should preserve discovery when qB is offline: %v", err)
	}
	if source.attempts != 0 {
		t.Fatalf("offline qB refresh must not submit, got %d attempts", source.attempts)
	}
	var resource model.SubscriptionResource
	if err := db.DB.Where("subscription_id = ?", sub.ID).First(&resource).Error; err != nil {
		t.Fatalf("load discovered resource: %v", err)
	}
	if resource.State == SubscriptionResourceStateFailed {
		t.Fatalf("qB outage must not turn a missing candidate into failed: %+v", resource)
	}
	if result.NeedsAttention == 0 {
		t.Fatalf("expected qB outage to be reported: %+v", result)
	}
}

type refreshTestDownloader struct {
	fakeDownloader
	episodes []parser.Episode
}
