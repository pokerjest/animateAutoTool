package service

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

type TorrentStatusSource interface {
	ListTorrents() ([]downloader.TorrentInfo, error)
}

type DownloadLogStatusSyncResult struct {
	Updated          int
	Completed        int
	Failed           int
	Active           int
	Unmatched        int
	CompletedTargets []string
}

type DownloadLogSyncSnapshot struct {
	LastCheckedAt      *time.Time
	LastSuccessAt      *time.Time
	LastError          string
	LastUpdated        int
	LastCompleted      int
	LastFailed         int
	LastActive         int
	LastUnmatched      int
	LastLibraryRepairs int
	LastRepairScanned  int
	LastArchived       int
}

type downloadLogSyncTracker struct {
	mu       sync.RWMutex
	snapshot DownloadLogSyncSnapshot
}

func (t *downloadLogSyncTracker) RecordSuccess(result DownloadLogStatusSyncResult) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	t.snapshot.LastCheckedAt = &now
	t.snapshot.LastSuccessAt = &now
	t.snapshot.LastError = ""
	t.snapshot.LastUpdated = result.Updated
	t.snapshot.LastCompleted = result.Completed
	t.snapshot.LastFailed = result.Failed
	t.snapshot.LastActive = result.Active
	t.snapshot.LastUnmatched = result.Unmatched
}

func (t *downloadLogSyncTracker) RecordLibraryRepair(repaired, scanned int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.snapshot.LastLibraryRepairs = repaired
	t.snapshot.LastRepairScanned = scanned
}

func (t *downloadLogSyncTracker) RecordArchived(count int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.snapshot.LastArchived = count
}

func (t *downloadLogSyncTracker) RecordFailure(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	t.snapshot.LastCheckedAt = &now
	if err != nil {
		t.snapshot.LastError = err.Error()
	} else {
		t.snapshot.LastError = "unknown error"
	}
}

func (t *downloadLogSyncTracker) Snapshot() DownloadLogSyncSnapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.snapshot
}

func (t *downloadLogSyncTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.snapshot = DownloadLogSyncSnapshot{}
}

var GlobalDownloadLogSyncStatus = &downloadLogSyncTracker{}

type DownloadLogRepairResult struct {
	Scanned  int
	Matched  int
	Repaired int
}

type DownloadLogArchiveResult struct {
	Scanned                 int
	Archived                int
	Protected               int
	AffectedSubscriptionIDs []uint
}

const (
	downloadLogStatusDownloading = "downloading"
	downloadLogStatusCompleted   = "completed"
	downloadLogStatusFailed      = "failed"
	downloadLogStatusArchived    = "archived"
)

func SyncDownloadLogStatuses(source TorrentStatusSource) (DownloadLogStatusSyncResult, error) {
	if source == nil {
		return DownloadLogStatusSyncResult{}, nil
	}

	torrents, err := source.ListTorrents()
	if err != nil {
		return DownloadLogStatusSyncResult{}, err
	}

	logStore := downloadLogStore()
	if logStore == nil {
		return DownloadLogStatusSyncResult{}, nil
	}

	logs, err := logStore.ListActiveOrIncompleteCompleted(downloadLogStatusDownloading, downloadLogStatusFailed, downloadLogStatusCompleted)
	if err != nil {
		return DownloadLogStatusSyncResult{}, err
	}

	byHash := make(map[string]downloader.TorrentInfo, len(torrents))
	byName := make(map[string]downloader.TorrentInfo, len(torrents))
	for _, torrent := range torrents {
		if torrent.Hash != "" {
			byHash[strings.ToLower(strings.TrimSpace(torrent.Hash))] = torrent
		}
		if torrent.Name != "" {
			byName[strings.TrimSpace(torrent.Name)] = torrent
		}
	}

	result := DownloadLogStatusSyncResult{}
	completedTargetSet := make(map[string]struct{})
	for _, logEntry := range logs {
		torrent, ok := matchTorrentForLog(logEntry, byHash, byName)
		if !ok {
			result.Unmatched++
			continue
		}

		nextStatus := mapTorrentStateToLogStatus(torrent.State)
		if nextStatus == "" {
			result.Unmatched++
			continue
		}

		updates := map[string]interface{}{}
		if nextStatus != logEntry.Status {
			updates["status"] = nextStatus
		}
		if logEntry.InfoHash == "" && torrent.Hash != "" {
			updates["info_hash"] = torrent.Hash
		}
		targetFile := deriveTargetFile(torrent)
		if targetFile != "" && logEntry.TargetFile != targetFile {
			updates["target_file"] = targetFile
		}

		switch nextStatus {
		case downloadLogStatusCompleted:
			result.Completed++
		case downloadLogStatusFailed:
			result.Failed++
		default:
			result.Active++
		}

		if len(updates) == 0 {
			continue
		}
		if err := logStore.UpdateByID(logEntry.ID, updates); err != nil {
			return result, err
		}
		result.Updated++
		if shouldQueueCompletedTarget(nextStatus, logEntry, targetFile) {
			if _, err := os.Stat(targetFile); err == nil {
				if _, seen := completedTargetSet[targetFile]; !seen {
					completedTargetSet[targetFile] = struct{}{}
					result.CompletedTargets = append(result.CompletedTargets, targetFile)
				}
			}
		}
	}

	return result, nil
}

func matchTorrentForLog(logEntry model.DownloadLog, byHash map[string]downloader.TorrentInfo, byName map[string]downloader.TorrentInfo) (downloader.TorrentInfo, bool) {
	if hash := strings.ToLower(strings.TrimSpace(logEntry.InfoHash)); hash != "" {
		if torrent, ok := byHash[hash]; ok {
			return torrent, true
		}
	}

	title := strings.TrimSpace(logEntry.Title)
	if title == "" {
		return downloader.TorrentInfo{}, false
	}

	torrent, ok := byName[title]
	return torrent, ok
}

func mapTorrentStateToLogStatus(state string) string {
	switch strings.TrimSpace(state) {
	case "error", "missingFiles", "unknown":
		return downloadLogStatusFailed
	case "uploading", "stalledUP", "queuedUP", "pausedUP", "checkingUP", "forcedUP", "allocating", "moving":
		return downloadLogStatusCompleted
	case "downloading", "metaDL", "stalledDL", "queuedDL", "pausedDL", "forcedDL", "checkingDL", "checkingResumeData":
		return downloadLogStatusDownloading
	default:
		return ""
	}
}

func deriveTargetFile(torrent downloader.TorrentInfo) string {
	if strings.TrimSpace(torrent.ContentPath) != "" {
		return filepath.Clean(strings.TrimSpace(torrent.ContentPath))
	}
	if strings.TrimSpace(torrent.SavePath) == "" || strings.TrimSpace(torrent.Name) == "" {
		return ""
	}
	return filepath.Clean(filepath.Join(strings.TrimSpace(torrent.SavePath), strings.TrimSpace(torrent.Name)))
}

func shouldQueueCompletedTarget(nextStatus string, logEntry model.DownloadLog, targetFile string) bool {
	if nextStatus != downloadLogStatusCompleted || targetFile == "" {
		return false
	}

	if logEntry.Status != downloadLogStatusCompleted {
		return true
	}

	return strings.TrimSpace(logEntry.TargetFile) == ""
}

func SyncDownloadLogStatusesWithQBClient(client *downloader.QBittorrentClient) (DownloadLogStatusSyncResult, error) {
	if client == nil {
		return DownloadLogStatusSyncResult{}, nil
	}

	result, err := SyncDownloadLogStatuses(client)
	if err != nil {
		GlobalDownloadLogSyncStatus.RecordFailure(err)
		log.Printf("Worker: qB download log sync failed: %v", err)
		return result, err
	}
	GlobalDownloadLogSyncStatus.RecordSuccess(result)

	if result.Updated > 0 {
		log.Printf("Worker: qB download log sync updated %d records (completed=%d failed=%d active=%d unmatched=%d)",
			result.Updated, result.Completed, result.Failed, result.Active, result.Unmatched)
	}
	return result, nil
}

func RepairDownloadLogsFromLocalLibrary(_ time.Duration) (DownloadLogRepairResult, error) {
	logStore := downloadLogStore()
	if logStore == nil {
		return DownloadLogRepairResult{}, nil
	}

	logs, err := logStore.ListByStatuses([]string{downloadLogStatusDownloading, downloadLogStatusFailed, downloadLogStatusCompleted})
	if err != nil {
		return DownloadLogRepairResult{}, err
	}

	subs, err := loadAllSubscriptions()
	if err != nil {
		return DownloadLogRepairResult{}, err
	}
	subscriptions := make(map[uint]model.Subscription, len(subs))
	for _, sub := range subs {
		subscriptions[sub.ID] = sub
	}

	result := DownloadLogRepairResult{}
	for _, logEntry := range logs {
		if !shouldAttemptLibraryRepair(logEntry) {
			continue
		}
		result.Scanned++

		sub, ok := subscriptions[logEntry.SubscriptionID]
		if !ok {
			continue
		}

		targetFile, matched := resolveLogTargetFromLibrary(logEntry, sub)
		if !matched || targetFile == "" {
			continue
		}
		result.Matched++

		updates := map[string]interface{}{}
		if strings.TrimSpace(logEntry.TargetFile) != targetFile {
			updates["target_file"] = targetFile
		}
		if logEntry.Status != downloadLogStatusCompleted {
			updates["status"] = downloadLogStatusCompleted
		}
		if len(updates) == 0 {
			continue
		}

		if err := logStore.UpdateByID(logEntry.ID, updates); err != nil {
			return result, err
		}
		result.Repaired++
	}

	GlobalDownloadLogSyncStatus.RecordLibraryRepair(result.Repaired, result.Scanned)
	return result, nil
}

func shouldAttemptLibraryRepair(logEntry model.DownloadLog) bool {
	target := strings.TrimSpace(logEntry.TargetFile)
	if target != "" && fileExists(target) && logEntry.Status == downloadLogStatusCompleted {
		return false
	}

	switch logEntry.Status {
	case downloadLogStatusCompleted:
		return target == "" || !fileExists(target)
	case downloadLogStatusFailed:
		return true
	case downloadLogStatusDownloading:
		return true
	default:
		return false
	}
}

func resolveLogTargetFromLibrary(logEntry model.DownloadLog, sub model.Subscription) (string, bool) {
	epNum, err := strconv.Atoi(strings.TrimSpace(logEntry.Episode))
	if err != nil || epNum <= 0 {
		return "", false
	}

	if sub.MetadataID != nil && *sub.MetadataID != 0 {
		if path, ok := findEpisodePathByMetadata(*sub.MetadataID, epNum); ok {
			return path, true
		}
	}

	return findEpisodePathByTitle(sub.Title, epNum)
}

func findEpisodePathByMetadata(metadataID uint, episodeNum int) (string, bool) {
	st := localAnimeStore()
	if st == nil {
		return "", false
	}
	rows, err := st.EpisodePathsByMetadata(metadataID, episodeNum)
	if err != nil {
		return "", false
	}
	for _, candidate := range rows {
		if fileExists(candidate.Path) {
			return filepath.Clean(candidate.Path), true
		}
	}
	return "", false
}

func findEpisodePathByTitle(title string, episodeNum int) (string, bool) {
	cleanTitle := normalizedRuleTitle(title)
	if cleanTitle == "" {
		return "", false
	}

	st := localAnimeStore()
	if st == nil {
		return "", false
	}
	rows, err := st.EpisodePathsByEpisodeNum(episodeNum)
	if err != nil {
		return "", false
	}
	for _, candidate := range rows {
		candidateTitle := normalizedRuleTitle(candidate.AnimeTitle)
		if candidateTitle == "" {
			continue
		}
		if candidateTitle != cleanTitle && !titlesLookRelated(candidate.AnimeTitle, title) {
			continue
		}
		if fileExists(candidate.Path) {
			return filepath.Clean(candidate.Path), true
		}
	}
	return "", false
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(filepath.Clean(path))
	return err == nil
}

func ArchiveStaleDownloadLogs(source TorrentStatusSource, maxAge time.Duration) (DownloadLogArchiveResult, error) {
	logStore := downloadLogStore()
	if logStore == nil {
		return DownloadLogArchiveResult{}, nil
	}

	byHash := map[string]downloader.TorrentInfo{}
	byName := map[string]downloader.TorrentInfo{}
	if source != nil {
		torrents, err := source.ListTorrents()
		if err != nil {
			return DownloadLogArchiveResult{}, err
		}
		for _, torrent := range torrents {
			if torrent.Hash != "" {
				byHash[strings.ToLower(strings.TrimSpace(torrent.Hash))] = torrent
			}
			if torrent.Name != "" {
				byName[strings.TrimSpace(torrent.Name)] = torrent
			}
		}
	}

	logs, err := logStore.ListByStatusesAsc([]string{downloadLogStatusDownloading, downloadLogStatusFailed})
	if err != nil {
		return DownloadLogArchiveResult{}, err
	}

	subs, err := loadAllSubscriptions()
	if err != nil {
		return DownloadLogArchiveResult{}, err
	}
	subscriptions := make(map[uint]model.Subscription, len(subs))
	for _, sub := range subs {
		subscriptions[sub.ID] = sub
	}

	cutoff := time.Now().Add(-maxAge)
	result := DownloadLogArchiveResult{}
	affected := make(map[uint]struct{})
	for _, logEntry := range logs {
		if logEntry.CreatedAt.After(cutoff) {
			continue
		}
		result.Scanned++

		if _, ok := matchTorrentForLog(logEntry, byHash, byName); ok {
			result.Protected++
			continue
		}

		sub, ok := subscriptions[logEntry.SubscriptionID]
		if ok {
			if targetFile, matched := resolveLogTargetFromLibrary(logEntry, sub); matched && targetFile != "" {
				result.Protected++
				continue
			}
		}

		if hasCompletedSibling(logEntry) {
			if err := logStore.MarkArchived(logEntry.ID, downloadLogStatusArchived); err != nil {
				return result, err
			}
			result.Archived++
			if logEntry.SubscriptionID != 0 {
				affected[logEntry.SubscriptionID] = struct{}{}
			}
			continue
		}

		if err := logStore.MarkArchived(logEntry.ID, downloadLogStatusArchived); err != nil {
			return result, err
		}
		result.Archived++
		if logEntry.SubscriptionID != 0 {
			affected[logEntry.SubscriptionID] = struct{}{}
		}
	}

	GlobalDownloadLogSyncStatus.RecordArchived(result.Archived)
	for id := range affected {
		result.AffectedSubscriptionIDs = append(result.AffectedSubscriptionIDs, id)
	}
	return result, nil
}

func hasCompletedSibling(logEntry model.DownloadLog) bool {
	s := downloadLogStore()
	if s == nil {
		return false
	}
	return s.HasCompletedSibling(logEntry.SubscriptionID, logEntry.Episode, downloadLogStatusCompleted)
}
