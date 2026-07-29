package worker

import (
	"context"
	"errors"
	"log"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

// qBittorrent may expose a completed torrent before the file move is visible
// to the application. Keep this short enough that a newly finished episode
// appears in the local library promptly, while the delayed rescan below
// handles the settling window.
const downloadLogSyncInterval = 15 * time.Second

func StartDownloadLogSyncWorker(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		syncDownloadLogStatuses(ctx)

		ticker := time.NewTicker(downloadLogSyncInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				syncDownloadLogStatuses(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func syncDownloadLogStatuses(ctx context.Context) {
	qbCfg := qbutil.LoadConfig()
	if qbutil.ManagedBinaryMissing(qbCfg, config.BinDir()) || qbutil.MissingExternalURL(qbCfg) {
		return
	}

	client := downloader.NewQBittorrentClient(qbCfg.URL)
	if err := client.Login(qbCfg.Username, qbCfg.Password); err != nil {
		log.Printf("Worker: qB download log sync login failed: %v", err)
		return
	}

	result, err := service.SyncDownloadLogStatusesWithQBClient(client)
	if err != nil {
		return
	}
	if renameResult, renameErr := service.AutoRenameCompletedDownloads(client); renameErr != nil {
		log.Printf("Worker: automatic download rename failed: %v", renameErr)
	} else {
		result.CompletedTargets = service.MergeCompletedTargets(result.CompletedTargets, renameResult)
		if renameResult.Renamed > 0 || renameResult.Failed > 0 {
			log.Printf("Worker: automatic download rename finished (renamed=%d skipped=%d failed=%d)",
				renameResult.Renamed, renameResult.Skipped, renameResult.Failed)
		}
	}

	// Scan as soon as qBittorrent reports completion. The target may still be
	// moving, so scheduleCompletedDownloadRescan performs a second pass after
	// the filesystem has had time to settle.
	autoScanCompletedDownloads(result.CompletedTargets)

	if repairResult, err := service.RepairDownloadLogsFromLocalLibrary(6 * time.Hour); err != nil {
		log.Printf("Worker: download log library repair failed: %v", err)
	} else if repairResult.Repaired > 0 {
		log.Printf("Worker: repaired %d stale download logs from local library matches (scanned=%d matched=%d)",
			repairResult.Repaired, repairResult.Scanned, repairResult.Matched)
	}
	if archiveResult, err := service.ArchiveStaleDownloadLogs(client, 30*24*time.Hour); err != nil {
		log.Printf("Worker: stale download log archive failed: %v", err)
	} else if archiveResult.Archived > 0 {
		log.Printf("Worker: archived %d stale download logs (scanned=%d protected=%d)",
			archiveResult.Archived, archiveResult.Scanned, archiveResult.Protected)
		if len(archiveResult.AffectedSubscriptionIDs) > 0 {
			if err := service.RetrySubscriptionsByID(ctx, client, archiveResult.AffectedSubscriptionIDs, "manual"); err != nil {
				log.Printf("Worker: auto retry after archive failed: %v", err)
			}
		}
	}
	if retried, err := service.RetryStaleSubscriptions(ctx, client, 6*time.Hour, "auto_recovery"); err != nil {
		log.Printf("Worker: stale subscription retry failed: %v", err)
	} else if retried > 0 {
		log.Printf("Worker: retried %d stale subscriptions", retried)
	}

	scheduleCompletedDownloadRescan(ctx, result.CompletedTargets)
}

func scheduleCompletedDownloadRescan(ctx context.Context, targets []string) {
	if len(targets) == 0 {
		return
	}
	queued := append([]string(nil), targets...)
	go func() {
		timer := time.NewTimer(15 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			autoScanCompletedDownloads(queued)
			// The second scan runs after qBittorrent has settled renamed/moved
			// files. Enriching metadata here writes provider IDs to tvshow.nfo,
			// then Jellyfin can scan and be reconciled without a user opening the
			// local-anime or player page first.
			service.NewAgentService().RunAgentForLibrary()
			syncCompletedDownloadsToJellyfin(ctx)
		case <-ctx.Done():
		}
	}()
}

func syncCompletedDownloadsToJellyfin(ctx context.Context) {
	if err := service.RequestJellyfinLibraryRefresh(ctx); err != nil {
		if !errors.Is(err, service.ErrJellyfinNotConfigured) {
			log.Printf("Worker: Jellyfin library refresh after download failed: %v", err)
		}
		return
	}
	log.Printf("Worker: requested Jellyfin library scan after completed download")

	for attempt := 0; attempt < 6; attempt++ {
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}

		result, err := service.SyncJellyfinLibraryMappings(ctx)
		if err != nil {
			log.Printf("Worker: Jellyfin library reconciliation after download failed: %v", err)
			return
		}
		if result.MatchedSeries > 0 {
			log.Printf("Worker: linked %d local series to Jellyfin after download", result.MatchedSeries)
			event.GlobalBus.Publish(event.EventMetadataUpdated, map[string]interface{}{
				"type": "jellyfin_library_sync", "status": "completed", "matched_series": result.MatchedSeries,
			})
			return
		}
		if result.PendingSeries == 0 {
			return
		}
	}
	log.Printf("Worker: Jellyfin accepted the scan request but pending series were not visible after 30 seconds")
}

func autoScanCompletedDownloads(targets []string) {
	if len(targets) == 0 || db.DB == nil {
		return
	}

	var dirs []model.LocalAnimeDirectory
	if err := db.DB.Find(&dirs).Error; err != nil {
		log.Printf("Worker: failed to load local anime directories for auto scan: %v", err)
		return
	}

	if len(dirs) == 0 {
		return
	}

	scanRoots := make(map[uint]model.LocalAnimeDirectory)
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}

		for _, dir := range dirs {
			if pathWithinRoot(target, dir.Path) {
				scanRoots[dir.ID] = dir
				break
			}
		}
	}

	if len(scanRoots) == 0 {
		return
	}

	scanner := service.NewScannerService()
	for _, dir := range scanRoots {
		if _, err := scanner.ScanDirectory(&dir); err != nil {
			log.Printf("Worker: auto scan failed for %s: %v", dir.Path, err)
		}
	}

	publishCompletedDownloadEvents(targets)
}

func pathWithinRoot(path string, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)

	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func publishCompletedDownloadEvents(targets []string) {
	notified := make(map[uint]struct{})
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}

		var episode model.LocalEpisode
		if err := db.DB.Where("path = ?", target).First(&episode).Error; err != nil {
			continue
		}

		var anime model.LocalAnime
		if err := db.DB.First(&anime, episode.LocalAnimeID).Error; err != nil {
			continue
		}
		if _, ok := notified[anime.ID]; ok {
			continue
		}
		notified[anime.ID] = struct{}{}

		event.GlobalBus.Publish(event.EventDownloadReady, map[string]interface{}{
			"title":          anime.Title,
			"local_anime_id": anime.ID,
			"target_file":    target,
			"episode_title":  episode.Title,
			"url":            "/local-anime?highlight=" + strings.TrimSpace(strconv.FormatUint(uint64(anime.ID), 10)) + "&open=1&focus_episode=" + url.QueryEscape(target),
		})
	}
}
