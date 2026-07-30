package service

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
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
	Scanned     int
	Matched     int
	Repaired    int
	Invalidated int
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
	downloadLogStatusRenamed     = "renamed"
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
	byNormalizedName := make(map[string]downloader.TorrentInfo, len(torrents))
	byEpisode := make(map[string][]downloader.TorrentInfo)
	for _, torrent := range torrents {
		if torrent.Hash != "" {
			addPreferredTorrent(byHash, strings.ToLower(strings.TrimSpace(torrent.Hash)), torrent)
		}
		if torrent.Name != "" {
			addPreferredTorrent(byName, strings.TrimSpace(torrent.Name), torrent)
			if normalized := parser.NormalizeReleaseTitle(torrent.Name); normalized != "" {
				addPreferredTorrent(byNormalizedName, normalized, torrent)
			}
		}
		addTorrentEpisodeIdentities(byEpisode, torrent)
	}

	subscriptions := loadSubscriptionsForDownloadLogMatching()
	result := DownloadLogStatusSyncResult{}
	completedTargetSet := make(map[string]struct{})
	for _, logEntry := range logs {
		torrent, ok := matchTorrentForLogWithNormalized(logEntry, byHash, byName, byNormalizedName)
		if !ok {
			var matchedByFallback bool
			torrent, matchedByFallback = matchTorrentForLogByEpisode(logEntry, subscriptions[logEntry.SubscriptionID], byEpisode)
			ok = matchedByFallback
			if ok {
				log.Printf("Worker: matched download log by episode/title/path fallback (subscription=%d episode=%s target=%s)",
					logEntry.SubscriptionID, strings.TrimSpace(logEntry.Episode), strings.TrimSpace(torrentContentPath(torrent)))
			}
		}
		if !ok {
			result.Unmatched++
			continue
		}

		nextStatus := torrentLogStatus(torrent)
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
			// qBittorrent can report a completed torrent before the final file
			// move/flush is visible on disk. Queue the path anyway so the worker
			// can scan immediately and retry after the filesystem settles.
			if _, seen := completedTargetSet[targetFile]; !seen {
				completedTargetSet[targetFile] = struct{}{}
				result.CompletedTargets = append(result.CompletedTargets, targetFile)
			}
		}
	}

	// Keep the durable resource ledger in sync with the compatibility log
	// projection. A reconciliation failure is surfaced to the caller rather
	// than silently leaving the two sources of truth divergent.
	if _, err := ReconcileSubscriptionResourcesFromDownloadLogs(); err != nil {
		return result, err
	}

	return result, nil
}

func loadSubscriptionsForDownloadLogMatching() map[uint]model.Subscription {
	result := make(map[uint]model.Subscription)
	if db.DB == nil {
		return result
	}
	var subscriptions []model.Subscription
	if err := db.DB.Find(&subscriptions).Error; err != nil {
		log.Printf("Worker: failed to load subscriptions for download-log matching: %v", err)
		return result
	}
	for _, subscription := range subscriptions {
		result[subscription.ID] = subscription
	}
	return result
}

func torrentEpisodeKey(season, episode string) string {
	return parser.NormalizeSeasonNumber(season) + ":" + parser.NormalizeEpisodeNumber(episode)
}

func addTorrentEpisodeIdentities(index map[string][]downloader.TorrentInfo, torrent downloader.TorrentInfo) {
	if index == nil {
		return
	}
	seen := make(map[string]struct{})
	add := func(season, episode string) {
		season = parser.NormalizeSeasonNumber(season)
		episode = parser.NormalizeEpisodeNumber(episode)
		if episode == "" {
			return
		}
		key := torrentEpisodeKey(season, episode)
		identity := key + "\x00" + strings.ToLower(strings.TrimSpace(torrent.Hash)) + "\x00" + normalizeTorrentPath(torrentContentPath(torrent))
		if _, exists := seen[identity]; exists {
			return
		}
		seen[identity] = struct{}{}
		index[key] = append(index[key], torrent)
	}

	if season, episode := parser.EpisodeIdentityFromTitle(torrent.Name); episode != "" {
		add(season, episode)
	}
	if season, episodes := parser.EpisodeIdentitiesFromPath(torrentContentPath(torrent)); len(episodes) > 0 {
		for _, episode := range episodes {
			add(season, episode)
		}
	}
}

// matchTorrentForLogByEpisode is the conservative fallback used when a
// qBittorrent task has a localized/normalized title or the RSS title differs
// by transport markers such as an extension. It requires the same episode,
// related series title, and—when available—the subscription's save directory.
// This mirrors the directory-first matching used by Jellyfin/Emby while
// retaining AniRSS-style episode identity as the final deterministic key.
func matchTorrentForLogByEpisode(logEntry model.DownloadLog, subscription model.Subscription, byEpisode map[string][]downloader.TorrentInfo) (downloader.TorrentInfo, bool) {
	season, episode := parser.EpisodeIdentityFromTitle(logEntry.Title)
	if episode == "" {
		episode = parser.NormalizeEpisodeNumber(logEntry.Episode)
	}
	if value := parser.NormalizeSeasonNumber(logEntry.SeasonVal); value != "" {
		season = value
	} else if season == "" {
		season = parser.NormalizeSeasonNumber(logEntry.SeasonVal)
	}
	key := torrentEpisodeKey(season, episode)
	if key == ":" {
		return downloader.TorrentInfo{}, false
	}

	expectedPath := ""
	if subscription.ID != 0 {
		expectedPath = normalizeTorrentPath(NewSubscriptionManager(nil).resolveSavePath(&subscription, logEntry.SeasonVal))
	}
	if target := strings.TrimSpace(logEntry.TargetFile); target != "" {
		target = filepath.Dir(filepath.Clean(target))
		if expectedPath == "" {
			expectedPath = normalizeTorrentPath(target)
		}
	}

	var best downloader.TorrentInfo
	bestScore := 0
	bestIdentity := ""
	ambiguous := false
	for _, torrent := range byEpisode[key] {
		score, identity, eligible := torrentFallbackEvidence(torrent, logEntry, subscription, expectedPath, season, episode)
		if !eligible {
			continue
		}
		switch {
		case score > bestScore:
			bestScore = score
			best = torrent
			bestIdentity = identity
			ambiguous = false
		case score == bestScore && sameTorrentIdentity(identity, bestIdentity):
			if preferredTorrent(torrent, best) {
				best = torrent
			}
		case score == bestScore:
			// Progress and state are snapshot quality, not identity evidence.
			// Never use a completed candidate to break a tie between distinct
			// torrents that otherwise look equally plausible.
			ambiguous = true
		}
	}
	if ambiguous {
		log.Printf("Worker: refusing ambiguous episode fallback match (subscription=%d season=%s episode=%s path=%s)",
			logEntry.SubscriptionID, season, episode, expectedPath)
		return downloader.TorrentInfo{}, false
	}
	return best, bestScore > 0
}

func torrentFallbackEvidence(
	torrent downloader.TorrentInfo,
	logEntry model.DownloadLog,
	subscription model.Subscription,
	expectedPath string,
	season string,
	episode string,
) (score int, identity string, eligible bool) {
	if !torrentHasEpisodeIdentity(torrent, season, episode) {
		return 0, "", false
	}
	titleRelated := titlesLookRelated(torrent.Name, logEntry.Title) ||
		titlesLookRelated(torrentContentPath(torrent), logEntry.Title)
	if !titleRelated && subscription.Title != "" {
		titleRelated = titlesLookRelated(torrent.Name, subscription.Title) ||
			titlesLookRelated(torrentContentPath(torrent), subscription.Title)
	}
	pathMatch := expectedPath != "" &&
		(torrentPathMatches(torrent.SavePath, expectedPath) || torrentPathMatches(torrent.ContentPath, expectedPath))
	score = torrentFallbackMatchScore(titleRelated, pathMatch, expectedPath != "")
	if score == 0 {
		return 0, "", false
	}
	if pathMatch {
		score += 10
	}
	identity = strings.ToLower(strings.TrimSpace(torrent.Hash))
	if identity == "" {
		identity = normalizeTorrentPath(torrentContentPath(torrent))
	}
	return score, identity, true
}

func torrentFallbackMatchScore(titleRelated, pathMatch, hasExpectedPath bool) int {
	switch {
	case titleRelated && pathMatch:
		return 100
	case titleRelated && !hasExpectedPath:
		return 82
	case !titleRelated && pathMatch:
		// A qB task can be renamed to a hash or a localized title. A
		// configured subscription directory plus exact season/episode is
		// enough to recover it, but it is deliberately weaker than a title
		// match and participates in ambiguity checks in the caller.
		return 72
	default:
		return 0
	}
}

func sameTorrentIdentity(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	return a != "" && b != "" && a == b
}

func torrentHasEpisodeIdentity(torrent downloader.TorrentInfo, season, episode string) bool {
	targetSeason := parser.NormalizeSeasonNumber(season)
	targetEpisode := parser.NormalizeEpisodeNumber(episode)
	if targetEpisode == "" {
		return false
	}
	if candidateSeason, candidateEpisode := parser.EpisodeIdentityFromTitle(torrent.Name); candidateEpisode != "" &&
		parser.NormalizeEpisodeNumber(candidateEpisode) == targetEpisode &&
		(targetSeason == "" || candidateSeason == "" || parser.NormalizeSeasonNumber(candidateSeason) == targetSeason) {
		return true
	}
	if candidateSeason, episodes := parser.EpisodeIdentitiesFromPath(torrentContentPath(torrent)); len(episodes) > 0 {
		if parser.NormalizeSeasonNumber(candidateSeason) != targetSeason {
			return false
		}
		for _, candidateEpisode := range episodes {
			if parser.NormalizeEpisodeNumber(candidateEpisode) == targetEpisode {
				return true
			}
		}
	}
	return false
}

func matchTorrentForLog(logEntry model.DownloadLog, byHash map[string]downloader.TorrentInfo, byName map[string]downloader.TorrentInfo) (downloader.TorrentInfo, bool) {
	return matchTorrentForLogWithNormalized(logEntry, byHash, byName, nil)
}

func matchTorrentForLogWithNormalized(logEntry model.DownloadLog, byHash map[string]downloader.TorrentInfo, byName, byNormalizedName map[string]downloader.TorrentInfo) (downloader.TorrentInfo, bool) {
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
	if ok {
		return torrent, true
	}
	if normalized := parser.NormalizeReleaseTitle(title); normalized != "" {
		torrent, ok := byNormalizedName[normalized]
		return torrent, ok
	}
	return downloader.TorrentInfo{}, false
}

func addPreferredTorrent(index map[string]downloader.TorrentInfo, key string, candidate downloader.TorrentInfo) {
	if strings.TrimSpace(key) == "" {
		return
	}
	current, ok := index[key]
	if !ok || preferredTorrent(candidate, current) {
		index[key] = candidate
	}
}

func preferredTorrent(candidate, current downloader.TorrentInfo) bool {
	candidateRank := torrentStateRank(candidate)
	currentRank := torrentStateRank(current)
	if candidateRank != currentRank {
		return candidateRank > currentRank
	}
	candidateProgress := normalizeTorrentProgress(candidate.Progress)
	currentProgress := normalizeTorrentProgress(current.Progress)
	if candidateProgress != currentProgress {
		return candidateProgress > currentProgress
	}
	return candidate.DownloadSpeed > current.DownloadSpeed
}

func torrentStateRank(torrent downloader.TorrentInfo) int {
	switch torrentLogStatus(torrent) {
	case downloadLogStatusCompleted:
		return 2
	case downloadLogStatusDownloading:
		return 1
	default:
		return 0
	}
}

func normalizeTorrentProgress(progress float64) float64 {
	if progress > 1 {
		progress /= 100
	}
	if progress < 0 {
		return 0
	}
	if progress > 1 {
		return 1
	}
	return progress
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

func torrentLogStatus(torrent downloader.TorrentInfo) string {
	mapped := mapTorrentStateToLogStatus(torrent.State)
	// Explicit qB error states must win even if stale byte counters still
	// report 100% from an earlier attempt.
	if mapped == downloadLogStatusFailed {
		return mapped
	}
	if normalizeTorrentProgress(torrent.Progress) >= 1 ||
		(torrent.Size > 0 && torrent.Completed >= torrent.Size) {
		return downloadLogStatusCompleted
	}
	return mapped
}

// DownloadLogStatusFromTorrent derives the effective status from both qB's
// state and its progress counters. Some qB versions briefly keep the state as
// "downloading" after all bytes have arrived.
func DownloadLogStatusFromTorrent(torrent downloader.TorrentInfo) string {
	return torrentLogStatus(torrent)
}

// DownloadLogStatusFromTorrentState exposes the same deterministic mapping to
// API presenters. This lets the history view show a just-finished torrent as
// completed before the background synchronizer writes the next database tick.
func DownloadLogStatusFromTorrentState(state string) string {
	return mapTorrentStateToLogStatus(state)
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

	subs, err := loadAllSubscriptions()
	if err != nil {
		return DownloadLogRepairResult{}, err
	}
	subscriptions := make(map[uint]model.Subscription, len(subs))
	result := DownloadLogRepairResult{}
	for _, sub := range subs {
		subscriptions[sub.ID] = sub
		invalidated, invalidateErr := invalidateMismatchedLocalDownloadLogs(sub)
		if invalidateErr != nil {
			return result, invalidateErr
		}
		result.Invalidated += invalidated
	}

	// Reload after invalidation so archived false-completion rows cannot be
	// immediately considered for a normal local-library repair.
	logs, err := logStore.ListByStatuses([]string{downloadLogStatusDownloading, downloadLogStatusFailed, downloadLogStatusCompleted})
	if err != nil {
		return result, err
	}

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

	GlobalDownloadLogSyncStatus.RecordLibraryRepair(result.Repaired+result.Invalidated, result.Scanned)
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
		if path, ok := findEpisodePathByMetadata(*sub.MetadataID, epNum, logEntry.Title, sub); ok {
			return path, true
		}
	}

	return findEpisodePathByTitle(sub.Title, epNum)
}

func findEpisodePathByMetadata(
	metadataID uint,
	episodeNum int,
	releaseTitle string,
	sub model.Subscription,
) (string, bool) {
	st := localAnimeStore()
	if st == nil {
		return "", false
	}
	rows, err := st.EpisodePathsByMetadata(metadataID, episodeNum)
	if err != nil {
		return "", false
	}
	for _, candidate := range rows {
		if !fileExists(candidate.Path) {
			continue
		}
		if !localEpisodeCandidateMatchesSubscription(sub, releaseTitle, candidate) {
			log.Printf(
				"WARN: rejected local episode candidate %q for subscription %q episode %d: shared metadata_id=%d but local series identity is %q (%s)",
				candidate.Path,
				sub.Title,
				episodeNum,
				metadataID,
				candidate.AnimeTitle,
				candidate.AnimePath,
			)
			continue
		}
		return filepath.Clean(candidate.Path), true
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
		if candidateTitle != cleanTitle && !titlesStronglyRelated(candidate.AnimeTitle, title) {
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
	byNormalizedName := map[string]downloader.TorrentInfo{}
	if source != nil {
		torrents, err := source.ListTorrents()
		if err != nil {
			return DownloadLogArchiveResult{}, err
		}
		for _, torrent := range torrents {
			if torrent.Hash != "" {
				addPreferredTorrent(byHash, strings.ToLower(strings.TrimSpace(torrent.Hash)), torrent)
			}
			if torrent.Name != "" {
				addPreferredTorrent(byName, strings.TrimSpace(torrent.Name), torrent)
				if normalized := parser.NormalizeReleaseTitle(torrent.Name); normalized != "" {
					addPreferredTorrent(byNormalizedName, normalized, torrent)
				}
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

		if _, ok := matchTorrentForLogWithNormalized(logEntry, byHash, byName, byNormalizedName); ok {
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
