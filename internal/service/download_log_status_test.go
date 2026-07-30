package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

	targetDir := t.TempDir()
	targetFile := filepath.Join(targetDir, "01.mkv")
	if err := os.WriteFile(targetFile, []byte("video"), 0o600); err != nil {
		t.Fatalf("failed to create target file: %v", err)
	}

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
				ContentPath: targetFile,
			},
		},
	})
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if result.Updated != 1 || result.Completed != 1 {
		t.Fatalf("unexpected sync result: %#v", result)
	}
	if len(result.CompletedTargets) != 1 || result.CompletedTargets[0] != targetFile {
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
	if updated.TargetFile != targetFile {
		t.Fatalf("expected target file to be stored, got %q", updated.TargetFile)
	}
}

func TestSyncDownloadLogStatusesMatchesVersionedReleaseTitle(t *testing.T) {
	withServiceTestDB(t)

	logEntry := model.DownloadLog{
		SubscriptionID: 12,
		Title:          "[ANi] Chainsmoker Cat - 01 [1080P][AAC AVC][MP4]",
		Status:         downloadLogStatusDownloading,
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("failed to create log entry: %v", err)
	}

	result, err := SyncDownloadLogStatuses(fakeTorrentStatusSource{
		torrents: []downloader.TorrentInfo{{
			Hash:  "versioned-123",
			Name:  "[ANi] Chainsmoker Cat - 01 [1080P][V2][AAC AVC][MP4]",
			State: "uploading",
		}},
	})
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	if result.Updated != 1 || result.Completed != 1 {
		t.Fatalf("expected V2 title to be treated as completed, got %#v", result)
	}

	var updated model.DownloadLog
	if err := db.DB.First(&updated, logEntry.ID).Error; err != nil {
		t.Fatalf("failed to reload log entry: %v", err)
	}
	if updated.Status != downloadLogStatusCompleted {
		t.Fatalf("expected completed status after V2 match, got %q", updated.Status)
	}
}

func TestSyncDownloadLogStatusesQueuesCompletedTargetBeforeFileAppears(t *testing.T) {
	withServiceTestDB(t)

	targetFile := filepath.Join(t.TempDir(), "episode-not-yet-visible.mkv")
	logEntry := model.DownloadLog{
		SubscriptionID: 11,
		Title:          "[Group] Settling Show - 01",
		Status:         downloadLogStatusDownloading,
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("failed to create log entry: %v", err)
	}

	result, err := SyncDownloadLogStatuses(fakeTorrentStatusSource{
		torrents: []downloader.TorrentInfo{
			{
				Hash:        "settling-123",
				Name:        "[Group] Settling Show - 01",
				State:       "uploading",
				ContentPath: targetFile,
			},
		},
	})
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if len(result.CompletedTargets) != 1 || result.CompletedTargets[0] != targetFile {
		t.Fatalf("expected missing completed target to be queued, got %#v", result.CompletedTargets)
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

func TestSyncDownloadLogStatusesBackfillsCompletedTargetForExistingCompletedLog(t *testing.T) {
	withServiceTestDB(t)

	targetDir := t.TempDir()
	targetFile := filepath.Join(targetDir, "01.mkv")
	if err := os.WriteFile(targetFile, []byte("video"), 0o600); err != nil {
		t.Fatalf("failed to create target file: %v", err)
	}

	logEntry := model.DownloadLog{
		SubscriptionID: 3,
		Title:          "[Group] Backfill Show - 01",
		Status:         downloadLogStatusCompleted,
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("failed to create completed log entry: %v", err)
	}

	result, err := SyncDownloadLogStatuses(fakeTorrentStatusSource{
		torrents: []downloader.TorrentInfo{
			{
				Hash:        "backfill-123",
				Name:        "[Group] Backfill Show - 01",
				State:       "stalledUP",
				ContentPath: targetFile,
			},
		},
	})
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if result.Updated != 1 || result.Completed != 1 {
		t.Fatalf("unexpected sync result: %#v", result)
	}
	if len(result.CompletedTargets) != 1 || result.CompletedTargets[0] != targetFile {
		t.Fatalf("expected completed target to be queued once, got %#v", result.CompletedTargets)
	}

	var updated model.DownloadLog
	if err := db.DB.First(&updated, logEntry.ID).Error; err != nil {
		t.Fatalf("failed to reload log entry: %v", err)
	}
	if updated.InfoHash != "backfill-123" {
		t.Fatalf("expected info hash to be backfilled, got %q", updated.InfoHash)
	}
	if updated.TargetFile != targetFile {
		t.Fatalf("expected target file to be backfilled, got %q", updated.TargetFile)
	}
}

func TestSyncDownloadLogStatusesDedupesCompletedTargets(t *testing.T) {
	withServiceTestDB(t)

	targetDir := t.TempDir()
	targetFile := filepath.Join(targetDir, "01.mkv")
	if err := os.WriteFile(targetFile, []byte("video"), 0o600); err != nil {
		t.Fatalf("failed to create target file: %v", err)
	}

	for _, title := range []string{"[Group] Dup Show - 01", "[Backup] Dup Show - 01"} {
		entry := model.DownloadLog{
			SubscriptionID: 4,
			Title:          title,
			Status:         downloadLogStatusCompleted,
		}
		if err := db.DB.Create(&entry).Error; err != nil {
			t.Fatalf("failed to create log entry %q: %v", title, err)
		}
	}

	result, err := SyncDownloadLogStatuses(fakeTorrentStatusSource{
		torrents: []downloader.TorrentInfo{
			{
				Hash:        "dup-1",
				Name:        "[Group] Dup Show - 01",
				State:       "uploading",
				ContentPath: targetFile,
			},
			{
				Hash:        "dup-2",
				Name:        "[Backup] Dup Show - 01",
				State:       "stalledUP",
				ContentPath: targetFile,
			},
		},
	})
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if len(result.CompletedTargets) != 1 || result.CompletedTargets[0] != targetFile {
		t.Fatalf("expected one deduped completed target, got %#v", result.CompletedTargets)
	}
}

func TestRepairDownloadLogsFromLocalLibraryRepairsStaleDownloadingLog(t *testing.T) {
	withServiceTestDB(t)

	meta := model.AnimeMetadata{Title: "Repair Show"}
	if err := db.DB.Create(&meta).Error; err != nil {
		t.Fatalf("failed to create metadata: %v", err)
	}
	sub := model.Subscription{
		Title:      "Repair Show",
		RSSUrl:     "https://example.com/repair-show",
		MetadataID: &meta.ID,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}
	anime := model.LocalAnime{Title: "Repair Show", MetadataID: &meta.ID, Path: t.TempDir()}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("failed to create local anime: %v", err)
	}
	targetFile := filepath.Join(anime.Path, "Repair Show - S01E01.mkv")
	if err := os.WriteFile(targetFile, []byte("video"), 0o600); err != nil {
		t.Fatalf("failed to create repaired target file: %v", err)
	}
	episode := model.LocalEpisode{
		LocalAnimeID: anime.ID,
		EpisodeNum:   1,
		SeasonNum:    1,
		Path:         targetFile,
	}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatalf("failed to create local episode: %v", err)
	}
	logEntry := model.DownloadLog{
		SubscriptionID: sub.ID,
		Title:          "[Group] Repair Show - 01",
		Episode:        "01",
		Status:         downloadLogStatusDownloading,
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("failed to create stale download log: %v", err)
	}
	oldCreatedAt := time.Now().Add(-7 * time.Hour)
	if err := db.DB.Model(&model.DownloadLog{}).Where("id = ?", logEntry.ID).Update("created_at", oldCreatedAt).Error; err != nil {
		t.Fatalf("failed to age download log: %v", err)
	}

	result, err := RepairDownloadLogsFromLocalLibrary(6 * time.Hour)
	if err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	if result.Repaired != 1 || result.Matched != 1 {
		t.Fatalf("unexpected repair result: %#v", result)
	}

	var updated model.DownloadLog
	if err := db.DB.First(&updated, logEntry.ID).Error; err != nil {
		t.Fatalf("failed to reload download log: %v", err)
	}
	if updated.Status != downloadLogStatusCompleted {
		t.Fatalf("expected repaired log to be completed, got %q", updated.Status)
	}
	if updated.TargetFile != targetFile {
		t.Fatalf("expected repaired target file %q, got %q", targetFile, updated.TargetFile)
	}
}

func TestResolveLogTargetRejectsUnrelatedSeriesSharingMetadata(t *testing.T) {
	withServiceTestDB(t)

	meta := model.AnimeMetadata{Title: "转生成猫的大叔", TitleCN: "转生成猫的大叔"}
	if err := db.DB.Create(&meta).Error; err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	sub := model.Subscription{
		Title:      "遭到流放的转生重骑士凭借游戏知识大开无双",
		RSSUrl:     "https://example.test/heavy-knight",
		MetadataID: &meta.ID,
		Metadata:   &meta,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	anime := model.LocalAnime{
		Title:      "转生成猫的大叔",
		Path:       filepath.Join(t.TempDir(), "转生成猫的大叔 (2024) [tmdbid=248707]"),
		MetadataID: &meta.ID,
	}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("create local anime: %v", err)
	}
	targetFile := filepath.Join(anime.Path, "转生成猫的大叔 - S01E04.mp4")
	if err := os.MkdirAll(anime.Path, 0o700); err != nil {
		t.Fatalf("create anime path: %v", err)
	}
	if err := os.WriteFile(targetFile, []byte("cat"), 0o600); err != nil {
		t.Fatalf("create target: %v", err)
	}
	if err := db.DB.Create(&model.LocalEpisode{
		LocalAnimeID: anime.ID,
		EpisodeNum:   4,
		SeasonNum:    1,
		Path:         targetFile,
	}).Error; err != nil {
		t.Fatalf("create episode: %v", err)
	}

	if target, matched := resolveLogTargetFromLibrary(model.DownloadLog{
		SubscriptionID: sub.ID,
		Title:          "[ANi] 遭到流放的转生重骑士凭借游戏知识大开无双 - 04",
		Episode:        "04",
	}, sub); matched || target != "" {
		t.Fatalf("unrelated series sharing metadata must not match, got target=%q matched=%v", target, matched)
	}
}

func TestResolveLogTargetAllowsLocalizedAliasesWithVerifiedMetadata(t *testing.T) {
	withServiceTestDB(t)

	meta := model.AnimeMetadata{
		Title:   "间谍过家家",
		TitleCN: "间谍过家家",
		TitleJP: "SPY x FAMILY",
	}
	if err := db.DB.Create(&meta).Error; err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	sub := model.Subscription{
		Title:      "间谍过家家",
		RSSUrl:     "https://example.test/spy-family",
		MetadataID: &meta.ID,
		Metadata:   &meta,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	anime := model.LocalAnime{
		Title:      "SPY x FAMILY",
		Path:       filepath.Join(t.TempDir(), "SPY x FAMILY"),
		MetadataID: &meta.ID,
	}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("create local anime: %v", err)
	}
	targetFile := filepath.Join(anime.Path, "SPY x FAMILY - S01E01.mkv")
	if err := os.MkdirAll(anime.Path, 0o700); err != nil {
		t.Fatalf("create anime path: %v", err)
	}
	if err := os.WriteFile(targetFile, []byte("spy"), 0o600); err != nil {
		t.Fatalf("create target: %v", err)
	}
	if err := db.DB.Create(&model.LocalEpisode{
		LocalAnimeID: anime.ID,
		EpisodeNum:   1,
		SeasonNum:    1,
		Path:         targetFile,
	}).Error; err != nil {
		t.Fatalf("create episode: %v", err)
	}

	target, matched := resolveLogTargetFromLibrary(model.DownloadLog{
		SubscriptionID: sub.ID,
		Title:          "[Group] 间谍过家家 - 01",
		Episode:        "01",
	}, sub)
	if !matched || target != targetFile {
		t.Fatalf("verified localized aliases should match, got target=%q matched=%v", target, matched)
	}
}

func TestRepairDownloadLogsFromLocalLibrarySkipsRecentDownloadingLog(t *testing.T) {
	withServiceTestDB(t)

	sub := model.Subscription{
		Title:  "Fresh Show",
		RSSUrl: "https://example.com/fresh-show",
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}
	logEntry := model.DownloadLog{
		SubscriptionID: sub.ID,
		Title:          "[Group] Fresh Show - 01",
		Episode:        "01",
		Status:         downloadLogStatusDownloading,
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("failed to create fresh download log: %v", err)
	}

	result, err := RepairDownloadLogsFromLocalLibrary(6 * time.Hour)
	if err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	if result.Scanned != 1 || result.Repaired != 0 {
		t.Fatalf("expected recent downloading log without local match to remain untouched, got %#v", result)
	}
}

func TestArchiveStaleDownloadLogsArchivesOldUnmatchedLog(t *testing.T) {
	withServiceTestDB(t)

	sub := model.Subscription{
		Title:  "Archive Show",
		RSSUrl: "https://example.com/archive-show",
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}
	entry := model.DownloadLog{
		SubscriptionID: sub.ID,
		Title:          "[Group] Archive Show - 01",
		Episode:        "01",
		Status:         downloadLogStatusDownloading,
	}
	if err := db.DB.Create(&entry).Error; err != nil {
		t.Fatalf("failed to create stale log: %v", err)
	}
	oldCreatedAt := time.Now().Add(-40 * 24 * time.Hour)
	if err := db.DB.Model(&model.DownloadLog{}).Where("id = ?", entry.ID).Update("created_at", oldCreatedAt).Error; err != nil {
		t.Fatalf("failed to age log: %v", err)
	}

	result, err := ArchiveStaleDownloadLogs(fakeTorrentStatusSource{}, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("archive failed: %v", err)
	}
	if result.Archived != 1 {
		t.Fatalf("expected 1 archived log, got %#v", result)
	}

	var updated model.DownloadLog
	if err := db.DB.First(&updated, entry.ID).Error; err != nil {
		t.Fatalf("failed to reload log: %v", err)
	}
	if updated.Status != downloadLogStatusArchived {
		t.Fatalf("expected archived status, got %q", updated.Status)
	}
	if len(result.AffectedSubscriptionIDs) != 1 || result.AffectedSubscriptionIDs[0] != sub.ID {
		t.Fatalf("expected affected subscription ids to include %d, got %#v", sub.ID, result.AffectedSubscriptionIDs)
	}
}

func TestArchiveStaleDownloadLogsProtectsMatchedTorrent(t *testing.T) {
	withServiceTestDB(t)

	sub := model.Subscription{
		Title:  "Protected Show",
		RSSUrl: "https://example.com/protected-show",
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}
	entry := model.DownloadLog{
		SubscriptionID: sub.ID,
		Title:          "[Group] Protected Show - 01",
		Episode:        "01",
		Status:         downloadLogStatusDownloading,
	}
	if err := db.DB.Create(&entry).Error; err != nil {
		t.Fatalf("failed to create stale log: %v", err)
	}
	oldCreatedAt := time.Now().Add(-40 * 24 * time.Hour)
	if err := db.DB.Model(&model.DownloadLog{}).Where("id = ?", entry.ID).Update("created_at", oldCreatedAt).Error; err != nil {
		t.Fatalf("failed to age log: %v", err)
	}

	result, err := ArchiveStaleDownloadLogs(fakeTorrentStatusSource{
		torrents: []downloader.TorrentInfo{{
			Name:  "[Group] Protected Show - 01",
			State: "downloading",
		}},
	}, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("archive failed: %v", err)
	}
	if result.Protected != 1 || result.Archived != 0 {
		t.Fatalf("expected matched torrent to be protected, got %#v", result)
	}
}
