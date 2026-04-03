package service

import (
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

type fakeTorrentStatusSource struct {
	torrents []downloader.TorrentInfo
	err      error
}

func (f fakeTorrentStatusSource) ListTorrents() ([]downloader.TorrentInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.torrents, nil
}

func TestSyncDownloadLogStatusesMarksCompletedAndStoresMetadata(t *testing.T) {
	withServiceTestDB(t)

	logEntry := model.DownloadLog{
		SubscriptionID: 1,
		Title:          "[Group] Sync Show - 01",
		Status:         downloadLogStatusDownloading,
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("failed to create log entry: %v", err)
	}

	result, err := SyncDownloadLogStatuses(fakeTorrentStatusSource{
		torrents: []downloader.TorrentInfo{
			{
				Hash:        "ABC123",
				Name:        "[Group] Sync Show - 01",
				State:       "uploading",
				ContentPath: "/downloads/sync-show/01.mkv",
			},
		},
	})
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if result.Updated != 1 || result.Completed != 1 {
		t.Fatalf("unexpected sync result: %#v", result)
	}
	if len(result.CompletedTargets) != 1 || result.CompletedTargets[0] != "/downloads/sync-show/01.mkv" {
		t.Fatalf("expected completed target to be returned, got %#v", result.CompletedTargets)
	}

	var updated model.DownloadLog
	if err := db.DB.First(&updated, logEntry.ID).Error; err != nil {
		t.Fatalf("failed to reload log entry: %v", err)
	}

	if updated.Status != downloadLogStatusCompleted {
		t.Fatalf("expected completed status, got %q", updated.Status)
	}
	if updated.InfoHash != "ABC123" {
		t.Fatalf("expected info hash to be stored, got %q", updated.InfoHash)
	}
	if updated.TargetFile != "/downloads/sync-show/01.mkv" {
		t.Fatalf("expected target file to be stored, got %q", updated.TargetFile)
	}
}

func TestSyncDownloadLogStatusesMarksFailedByInfoHash(t *testing.T) {
	withServiceTestDB(t)

	logEntry := model.DownloadLog{
		SubscriptionID: 2,
		Title:          "[Group] Broken Show - 03",
		Status:         downloadLogStatusDownloading,
		InfoHash:       "deadbeef",
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("failed to create log entry: %v", err)
	}

	result, err := SyncDownloadLogStatuses(fakeTorrentStatusSource{
		torrents: []downloader.TorrentInfo{
			{
				Hash:  "DEADBEEF",
				Name:  "Totally Different Name",
				State: "error",
			},
		},
	})
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if result.Updated != 1 || result.Failed != 1 {
		t.Fatalf("unexpected sync result: %#v", result)
	}
	if len(result.CompletedTargets) != 0 {
		t.Fatalf("did not expect completed targets for failed torrent, got %#v", result.CompletedTargets)
	}

	var updated model.DownloadLog
	if err := db.DB.First(&updated, logEntry.ID).Error; err != nil {
		t.Fatalf("failed to reload log entry: %v", err)
	}

	if updated.Status != downloadLogStatusFailed {
		t.Fatalf("expected failed status, got %q", updated.Status)
	}
}
