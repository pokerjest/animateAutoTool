package service

import (
	"os"
	"path/filepath"
	"strings"
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

func TestDownloadLogStatusTreatsFullProgressAsCompleted(t *testing.T) {
	for _, torrent := range []downloader.TorrentInfo{
		{State: "downloading", Progress: 1},
		{State: "downloading", Progress: 100},
		{State: "downloading", Size: 1024, Completed: 1024},
		{State: "stalledDL", Size: 1024, Completed: 2048},
	} {
		if got := DownloadLogStatusFromTorrent(torrent); got != downloadLogStatusCompleted {
			t.Fatalf("DownloadLogStatusFromTorrent(%+v) = %q, want completed", torrent, got)
		}
	}
}

func TestDownloadLogStatusKeepsExplicitErrorsFailedAtFullProgress(t *testing.T) {
	torrent := downloader.TorrentInfo{State: "error", Progress: 1, Size: 1024, Completed: 1024}
	if got := DownloadLogStatusFromTorrent(torrent); got != downloadLogStatusFailed {
		t.Fatalf("DownloadLogStatusFromTorrent(%+v) = %q, want failed", torrent, got)
	}
}

func TestSyncDownloadLogStatusesReconcilesFullProgressResourceAsCompleted(t *testing.T) {
	withServiceTestDB(t)

	subscription := model.Subscription{Title: "Progress Show", RSSUrl: "https://example.test/progress"}
	if err := db.DB.Create(&subscription).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	resource := model.SubscriptionResource{
		SubscriptionID: subscription.ID,
		CanonicalKey:   "s01:e01",
		Fingerprint:    resourceFingerprintForLog(model.DownloadLog{InfoHash: "progress-full"}),
		Title:          "[Group] Progress Show - 01",
		Episode:        "01",
		SeasonVal:      "S01",
		State:          SubscriptionResourceStateDownloading,
		Selected:       true,
	}
	if err := db.DB.Create(&resource).Error; err != nil {
		t.Fatalf("create resource: %v", err)
	}
	logEntry := model.DownloadLog{
		SubscriptionID: subscription.ID,
		ResourceID:     &resource.ID,
		Title:          resource.Title,
		Episode:        resource.Episode,
		SeasonVal:      resource.SeasonVal,
		Status:         downloadLogStatusDownloading,
		InfoHash:       "progress-full",
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("create download log: %v", err)
	}

	result, err := SyncDownloadLogStatuses(fakeTorrentStatusSource{torrents: []downloader.TorrentInfo{{
		Hash:      "progress-full",
		Name:      resource.Title,
		State:     "downloading",
		Progress:  1,
		Size:      1024,
		Completed: 1024,
	}}})
	if err != nil {
		t.Fatalf("sync statuses: %v", err)
	}
	if result.Completed != 1 {
		t.Fatalf("expected completed sync result, got %+v", result)
	}

	var updatedLog model.DownloadLog
	if err := db.DB.First(&updatedLog, logEntry.ID).Error; err != nil {
		t.Fatalf("reload download log: %v", err)
	}
	if updatedLog.Status != downloadLogStatusCompleted {
		t.Fatalf("download log status = %q, want completed", updatedLog.Status)
	}
	var updatedResource model.SubscriptionResource
	if err := db.DB.First(&updatedResource, resource.ID).Error; err != nil {
		t.Fatalf("reload resource: %v", err)
	}
	if updatedResource.State != SubscriptionResourceStateCompleted {
		t.Fatalf("resource state = %q, want completed", updatedResource.State)
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

func TestSyncDownloadLogStatusesMatchesRSSTitleToQBMaterializedFilename(t *testing.T) {
	withServiceTestDB(t)

	subscription := model.Subscription{
		Title:    "才女的侍从 在满是高岭之花的贵族学校暗中照顾（毫无生活自理能力的）学院第一大小姐",
		RSSUrl:   "https://example.test/ani-rss",
		SavePath: `E:\Bangumi\才女的侍从 在满是高岭之花的贵族学校暗中照顾（毫无生活自理能力的）学院第一大小姐`,
	}
	if err := db.DB.Create(&subscription).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	logEntry := model.DownloadLog{
		SubscriptionID: subscription.ID,
		Title:          "[ANi]  才女的侍从 在满是高岭之花的贵族学校暗中照顾（毫无生活自理能力的）学院第一大小姐 - 04 [1080P][Baha][WEB-DL][AAC AVC][CHT][MP4]",
		Episode:        "04",
		SeasonVal:      "S01",
		Status:         downloadLogStatusDownloading,
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("create download log: %v", err)
	}

	target := `E:\Bangumi\才女的侍从 在满是高岭之花的贵族学校暗中照顾（毫无生活自理能力的）学院第一大小姐\Season 01\[ANi]  才女的侍从 在满是高岭之花的贵族学校暗中照顾（毫无生活自理能力的）学院第一大小姐 - 04 [1080P][Baha][WEB-DL][AAC AVC][CHT].mp4`
	result, err := SyncDownloadLogStatuses(fakeTorrentStatusSource{torrents: []downloader.TorrentInfo{{
		Hash:        "materialized-04",
		Name:        strings.TrimSuffix(filepath.Base(target), ".mp4") + ".mp4",
		State:       "downloading",
		Progress:    1,
		SavePath:    `E:\Bangumi\才女的侍从 在满是高岭之花的贵族学校暗中照顾（毫无生活自理能力的）学院第一大小姐\Season 01`,
		ContentPath: target,
	}}})
	if err != nil {
		t.Fatalf("sync statuses: %v", err)
	}
	if result.Completed != 1 || len(result.CompletedTargets) != 1 || result.CompletedTargets[0] != target {
		t.Fatalf("expected completed materialized torrent to be queued for scanning, got %#v", result)
	}

	var updated model.DownloadLog
	if err := db.DB.First(&updated, logEntry.ID).Error; err != nil {
		t.Fatalf("reload download log: %v", err)
	}
	if updated.Status != downloadLogStatusCompleted || updated.InfoHash != "materialized-04" || updated.TargetFile != target {
		t.Fatalf("unexpected synchronized log: %+v", updated)
	}
}

func TestSyncDownloadLogStatusesDoesNotMatchSameEpisodeFromAnotherSeries(t *testing.T) {
	withServiceTestDB(t)

	subscription := model.Subscription{
		Title:    "Target Show",
		RSSUrl:   "https://example.test/target-show",
		SavePath: `/downloads/Target Show`,
	}
	if err := db.DB.Create(&subscription).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	logEntry := model.DownloadLog{
		SubscriptionID: subscription.ID,
		Title:          "[Group] Target Show - 01 [MP4]",
		Episode:        "01",
		SeasonVal:      "S01",
		Status:         downloadLogStatusDownloading,
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("create download log: %v", err)
	}

	result, err := SyncDownloadLogStatuses(fakeTorrentStatusSource{torrents: []downloader.TorrentInfo{{
		Hash:        "wrong-show",
		Name:        "[Other] Other Show - 01 [1080P].mp4",
		State:       "uploading",
		Progress:    1,
		SavePath:    `/downloads/Other Show/Season 01`,
		ContentPath: `/downloads/Other Show/Season 01/Other Show - 01.mp4`,
	}}})
	if err != nil {
		t.Fatalf("sync statuses: %v", err)
	}
	if result.Completed != 0 || len(result.CompletedTargets) != 0 || result.Unmatched != 1 {
		t.Fatalf("same-episode torrent from another series was matched: %#v", result)
	}
}

func TestSyncDownloadLogStatusesUsesSeasonDirectoryWhenTaskNameOmitsSeason(t *testing.T) {
	withServiceTestDB(t)

	subscription := model.Subscription{
		Title:    "Seasoned Show",
		Season:   "S02",
		RSSUrl:   "https://example.test/seasoned-show",
		SavePath: `/downloads/Seasoned Show`,
	}
	if err := db.DB.Create(&subscription).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	logEntry := model.DownloadLog{
		SubscriptionID: subscription.ID,
		Title:          "[Group] Seasoned Show - 03 [MP4]",
		Episode:        "03",
		SeasonVal:      "S02",
		Status:         downloadLogStatusDownloading,
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("create download log: %v", err)
	}

	target := `/downloads/Seasoned Show/Season 02/Seasoned Show - 03.mkv`
	result, err := SyncDownloadLogStatuses(fakeTorrentStatusSource{torrents: []downloader.TorrentInfo{{
		Hash:        "season-directory",
		Name:        "Seasoned Show - 03.mkv",
		State:       "uploading",
		SavePath:    `/downloads/Seasoned Show/Season 02`,
		ContentPath: target,
		Progress:    1,
	}}})
	if err != nil {
		t.Fatalf("sync statuses: %v", err)
	}
	if result.Completed != 1 || len(result.CompletedTargets) != 1 || result.CompletedTargets[0] != target {
		t.Fatalf("expected season-directory match, got %#v", result)
	}
}

func TestSyncDownloadLogStatusesMatchesEpisodeInsideMultiEpisodeTorrent(t *testing.T) {
	withServiceTestDB(t)

	subscription := model.Subscription{
		Title:    "Multi Show",
		Season:   "S01",
		RSSUrl:   "https://example.test/multi-show",
		SavePath: `/downloads/Multi Show`,
	}
	if err := db.DB.Create(&subscription).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	logEntry := model.DownloadLog{
		SubscriptionID: subscription.ID,
		Title:          "[Group] Multi Show - 04",
		Episode:        "04",
		SeasonVal:      "S01",
		Status:         downloadLogStatusDownloading,
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("create download log: %v", err)
	}

	target := `/downloads/Multi Show/Season 01/Multi Show S01E03-E05.mkv`
	result, err := SyncDownloadLogStatuses(fakeTorrentStatusSource{torrents: []downloader.TorrentInfo{{
		Hash:        "multi-episode",
		Name:        "Multi Show S01E03-E05.mkv",
		State:       "uploading",
		SavePath:    `/downloads/Multi Show/Season 01`,
		ContentPath: target,
		Progress:    1,
	}}})
	if err != nil {
		t.Fatalf("sync statuses: %v", err)
	}
	if result.Completed != 1 || len(result.CompletedTargets) != 1 || result.CompletedTargets[0] != target {
		t.Fatalf("expected multi-episode match, got %#v", result)
	}
}

func TestSyncDownloadLogStatusesRefusesAmbiguousPathFallback(t *testing.T) {
	withServiceTestDB(t)

	subscription := model.Subscription{
		Title:    "Target Show",
		Season:   "S01",
		RSSUrl:   "https://example.test/target-show-ambiguous",
		SavePath: `/downloads/shared`,
	}
	if err := db.DB.Create(&subscription).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	logEntry := model.DownloadLog{
		SubscriptionID: subscription.ID,
		Title:          "[Group] Target Show - 01",
		Episode:        "01",
		SeasonVal:      "S01",
		Status:         downloadLogStatusDownloading,
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("create download log: %v", err)
	}

	result, err := SyncDownloadLogStatuses(fakeTorrentStatusSource{torrents: []downloader.TorrentInfo{
		{
			Hash:        "ambiguous-a",
			Name:        "a.mkv",
			State:       "downloading",
			SavePath:    `/downloads/shared/Season 01`,
			ContentPath: `/downloads/shared/Season 01/01.mkv`,
			Progress:    .35,
		},
		{
			Hash:        "ambiguous-b",
			Name:        "b.mkv",
			State:       "uploading",
			SavePath:    `/downloads/shared/Season 01`,
			ContentPath: `/downloads/shared/Season 01/Episode 01.mkv`,
			Progress:    1,
		},
	}})
	if err != nil {
		t.Fatalf("sync statuses: %v", err)
	}
	if result.Completed != 0 || result.Unmatched != 1 || len(result.CompletedTargets) != 0 {
		t.Fatalf("ambiguous candidates must not be guessed: %#v", result)
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
