package worker

import (
	"context"
	"errors"
	"log"
	"net/url"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	go RunDownloadLogSyncWorker(ctx)
}

// RunDownloadLogSyncWorker runs the periodic worker until ctx is canceled.
// Startup uses the synchronous form so shutdown can wait before closing SQLite.
func RunDownloadLogSyncWorker(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	log.Printf("DownloadLogWorker: started interval=%s", downloadLogSyncInterval)
	defer func() {
		log.Printf("DownloadLogWorker: stopped reason=%v", ctx.Err())
	}()

	runDownloadLogSyncCycle(ctx, "startup")

	ticker := time.NewTicker(downloadLogSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			runDownloadLogSyncCycle(ctx, "periodic")
		case <-ctx.Done():
			return
		}
	}
}

func runDownloadLogSyncCycle(ctx context.Context, trigger string) {
	runDownloadLogSyncCycleWith(ctx, trigger, syncDownloadLogStatuses)
}

func runDownloadLogSyncCycleWith(ctx context.Context, trigger string, run func(context.Context)) {
	start := time.Now()
	log.Printf("DownloadLogWorker: cycle starting trigger=%s", trigger)
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf(
				"ERROR: DownloadLogWorker: cycle panic trigger=%s duration=%s recovery_action=continue_next_cycle panic=%v\n%s",
				trigger,
				time.Since(start).Round(time.Millisecond),
				recovered,
				debug.Stack(),
			)
		}
	}()
	if run != nil {
		run(ctx)
	}
	log.Printf("DownloadLogWorker: cycle completed trigger=%s duration=%s", trigger, time.Since(start).Round(time.Millisecond))
}

func syncDownloadLogStatuses(ctx context.Context) {
	if err := ctx.Err(); err != nil {
		log.Printf("DownloadLogWorker: cycle skipped reason=context_canceled error=%v", err)
		return
	}
	qbCfg := qbutil.LoadConfig()
	if qbutil.ManagedBinaryMissing(qbCfg, config.BinDir()) {
		log.Printf("DownloadLogWorker: cycle skipped reason=managed_qbittorrent_missing")
		return
	}
	if qbutil.MissingExternalURL(qbCfg) {
		log.Printf("DownloadLogWorker: cycle skipped reason=external_qbittorrent_url_missing")
		return
	}

	client := downloader.NewQBittorrentClient(qbCfg.URL)
	if err := client.LoginContext(ctx, qbCfg.Username, qbCfg.Password); err != nil {
		log.Printf("ERROR: DownloadLogWorker: qBittorrent login failed recovery_action=retry_next_cycle error=%v", err)
		return
	}
	log.Printf("DownloadLogWorker: qBittorrent login succeeded")

	result, err := service.SyncDownloadLogStatusesWithQBClient(client)
	if err != nil {
		log.Printf(
			"ERROR: DownloadLogWorker: status sync failed updated=%d completed=%d failed=%d active=%d unmatched=%d recovery_action=retry_next_cycle error=%v",
			result.Updated,
			result.Completed,
			result.Failed,
			result.Active,
			result.Unmatched,
			err,
		)
		return
	}
	log.Printf(
		"DownloadLogWorker: status sync completed updated=%d completed=%d failed=%d active=%d unmatched=%d targets=%d",
		result.Updated,
		result.Completed,
		result.Failed,
		result.Active,
		result.Unmatched,
		len(result.CompletedTargets),
	)
	if renameResult, renameErr := service.AutoRenameCompletedDownloads(client); renameErr != nil {
		log.Printf("ERROR: DownloadLogWorker: automatic rename failed recovery_action=continue_without_rename error=%v", renameErr)
	} else {
		result.CompletedTargets = service.MergeCompletedTargets(result.CompletedTargets, renameResult)
		log.Printf("DownloadLogWorker: automatic rename completed renamed=%d skipped=%d failed=%d targets=%d",
			renameResult.Renamed, renameResult.Skipped, renameResult.Failed, len(result.CompletedTargets))
	}

	// Scan as soon as qBittorrent reports completion. The target may still be
	// moving, so scheduleCompletedDownloadRescan performs a second pass after
	// the filesystem has had time to settle.
	initialIDs := autoScanCompletedDownloads(result.CompletedTargets)

	if repairResult, err := service.RepairDownloadLogsFromLocalLibrary(6 * time.Hour); err != nil {
		log.Printf("ERROR: DownloadLogWorker: library repair failed recovery_action=continue_to_reconciliation error=%v", err)
	} else {
		log.Printf("DownloadLogWorker: library repair completed repaired=%d invalidated=%d scanned=%d matched=%d",
			repairResult.Repaired, repairResult.Invalidated, repairResult.Scanned, repairResult.Matched)
	}
	if updated, err := service.ReconcileSubscriptionResourcesFromDownloadLogs(); err != nil {
		log.Printf("ERROR: DownloadLogWorker: resource reconciliation failed recovery_action=retry_next_cycle error=%v", err)
	} else {
		log.Printf("DownloadLogWorker: resource reconciliation completed updated=%d", updated)
	}

	scheduleCompletedDownloadRescan(ctx, result.CompletedTargets, initialIDs)
}

func scheduleCompletedDownloadRescan(ctx context.Context, targets []string, initialIDs []uint) {
	if len(targets) == 0 {
		return
	}
	log.Printf("DownloadLogWorker: delayed rescan scheduled targets=%d anime_ids=%d delay=%s",
		len(targets), len(initialIDs), completedDownloadRescan.delay)
	completedDownloadRescan.schedule(ctx, targets, initialIDs)
}

type completedDownloadRescanCoordinator struct {
	mu            sync.Mutex
	timer         *time.Timer
	running       bool
	delay         time.Duration
	ctx           context.Context
	pendingTarget map[string]struct{}
	pendingAnime  map[uint]struct{}
	run           func(context.Context, []string, []uint)
}

func newCompletedDownloadRescanCoordinator(delay time.Duration, run func(context.Context, []string, []uint)) *completedDownloadRescanCoordinator {
	if delay <= 0 {
		delay = downloadLogSyncInterval
	}
	return &completedDownloadRescanCoordinator{
		delay:         delay,
		pendingTarget: make(map[string]struct{}),
		pendingAnime:  make(map[uint]struct{}),
		run:           run,
	}
}

func (c *completedDownloadRescanCoordinator) schedule(ctx context.Context, targets []string, animeIDs []uint) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if ctx != nil {
		c.ctx = ctx
	}
	for _, target := range targets {
		if target = strings.TrimSpace(target); target != "" {
			c.pendingTarget[target] = struct{}{}
		}
	}
	for _, id := range animeIDs {
		if id != 0 {
			c.pendingAnime[id] = struct{}{}
		}
	}
	if c.running {
		return
	}
	if c.timer == nil {
		c.timer = time.AfterFunc(c.delay, c.flush)
	} else {
		c.timer.Reset(c.delay)
	}
}

func (c *completedDownloadRescanCoordinator) flush() {
	c.mu.Lock()
	if c.running || len(c.pendingTarget) == 0 {
		c.mu.Unlock()
		return
	}
	targets := make([]string, 0, len(c.pendingTarget))
	for target := range c.pendingTarget {
		targets = append(targets, target)
	}
	animeIDs := sortedUintIDs(c.pendingAnime)
	c.pendingTarget = make(map[string]struct{})
	c.pendingAnime = make(map[uint]struct{})
	c.timer = nil
	c.running = true
	ctx := c.ctx
	run := c.run
	c.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() == nil && run != nil {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					log.Printf(
						"ERROR: DownloadLogWorker: rescan coordinator panic targets=%d anime_ids=%d recovery_action=continue_future_batches panic=%v\n%s",
						len(targets),
						len(animeIDs),
						recovered,
						debug.Stack(),
					)
				}
			}()
			run(ctx, targets, animeIDs)
		}()
	}

	c.mu.Lock()
	c.running = false
	if ctx.Err() == nil && len(c.pendingTarget) > 0 && c.timer == nil {
		c.timer = time.AfterFunc(c.delay, c.flush)
	} else if ctx.Err() != nil {
		c.pendingTarget = make(map[string]struct{})
		c.pendingAnime = make(map[uint]struct{})
	}
	c.mu.Unlock()
}

var completedDownloadRescan = newCompletedDownloadRescanCoordinator(downloadLogSyncInterval, runCompletedDownloadPostProcessing)

func runCompletedDownloadPostProcessing(ctx context.Context, targets []string, initialIDs []uint) {
	start := time.Now()
	log.Printf("DownloadLogWorker: delayed rescan starting targets=%d anime_ids=%d", len(targets), len(initialIDs))
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf(
				"ERROR: DownloadLogWorker: delayed post-processing panic targets=%d anime_ids=%d recovery_action=retry_future_batch panic=%v\n%s",
				len(targets),
				len(initialIDs),
				recovered,
				debug.Stack(),
			)
		}
		log.Printf("DownloadLogWorker: delayed rescan finished targets=%d anime_ids=%d duration=%s",
			len(targets), len(initialIDs), time.Since(start).Round(time.Millisecond))
	}()
	delayedIDs := autoScanCompletedDownloads(targets)
	affected := mergeAnimeIDs(initialIDs, delayedIDs)
	if _, err := service.ReconcileSubscriptionResourcesFromDownloadLogs(); err != nil {
		log.Printf("ERROR: DownloadLogWorker: resource reconciliation after delayed scan failed recovery_action=continue_to_jellyfin error=%v", err)
	}
	// Enrich only series touched by this batch. This also avoids the global
	// historical repair pass, which remains available from the health tools.
	if len(affected) > 0 {
		service.NewAgentService().RunAgentForAnimeIDs(affected)
	}
	// A single serialized post-processing run refreshes Jellyfin once for the
	// whole debounce window, even when several torrents finish together.
	syncCompletedDownloadsToJellyfin(ctx, affected)
}

func mergeAnimeIDs(groups ...[]uint) []uint {
	unique := make(map[uint]struct{})
	for _, group := range groups {
		for _, id := range group {
			if id != 0 {
				unique[id] = struct{}{}
			}
		}
	}
	return sortedUintIDs(unique)
}

func sortedUintIDs(values map[uint]struct{}) []uint {
	result := make([]uint, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func syncCompletedDownloadsToJellyfin(ctx context.Context, animeIDs []uint) {
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

		var (
			result service.JellyfinLibrarySyncResult
			err    error
		)
		if len(animeIDs) > 0 {
			result, err = service.SyncJellyfinLibraryMappingsForAnimeIDs(ctx, animeIDs)
		} else {
			result, err = service.SyncJellyfinLibraryMappings(ctx)
		}
		if err != nil {
			log.Printf("Worker: Jellyfin library reconciliation after download failed: %v", err)
			return
		}
		if result.MatchedSeries > 0 {
			log.Printf("Worker: linked %d local series to Jellyfin after download", result.MatchedSeries)
			event.GlobalBus.Publish(event.EventMetadataUpdated, map[string]interface{}{
				"type": "jellyfin_library_sync", "status": "completed", "matched_series": result.MatchedSeries,
			})
		}
		if completedDownloadJellyfinBatchSettled(result) {
			return
		}
	}
	log.Printf("Worker: Jellyfin accepted the scan request but pending series were not visible after 30 seconds")
}

func completedDownloadJellyfinBatchSettled(result service.JellyfinLibrarySyncResult) bool {
	return result.PendingSeries == 0 || result.MatchedSeries >= result.PendingSeries
}

func autoScanCompletedDownloads(targets []string) []uint {
	if len(targets) == 0 {
		return nil
	}
	if db.DB == nil {
		log.Printf("ERROR: DownloadLogWorker: auto scan skipped reason=database_unavailable targets=%d", len(targets))
		return nil
	}
	if !service.IncrementalScanEnabled() {
		log.Printf("DownloadLogWorker: auto scan skipped reason=incremental_scan_disabled targets=%d", len(targets))
		return nil
	}

	var dirs []model.LocalAnimeDirectory
	if err := db.DB.Find(&dirs).Error; err != nil {
		log.Printf("Worker: failed to load local anime directories for auto scan: %v", err)
		return nil
	}

	if len(dirs) == 0 {
		log.Printf("DownloadLogWorker: auto scan skipped reason=no_local_directories targets=%d", len(targets))
		return nil
	}

	scanTargets := make(map[uint][]string)
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		var selected *model.LocalAnimeDirectory
		for _, dir := range dirs {
			if pathWithinRoot(target, dir.Path) {
				if selected == nil || len(filepath.Clean(dir.Path)) > len(filepath.Clean(selected.Path)) {
					dirCopy := dir
					selected = &dirCopy
				}
			}
		}
		if selected != nil {
			scanTargets[selected.ID] = append(scanTargets[selected.ID], target)
		}
	}

	if len(scanTargets) == 0 {
		log.Printf("DownloadLogWorker: auto scan skipped reason=targets_outside_local_directories targets=%d", len(targets))
		return nil
	}

	scanner := service.NewScannerService()
	affected := make(map[uint]struct{})
	for _, dir := range dirs {
		targetsForDir := scanTargets[dir.ID]
		if len(targetsForDir) == 0 {
			continue
		}
		result, err := scanner.ScanTargets(&dir, targetsForDir)
		if err != nil {
			log.Printf("ERROR: DownloadLogWorker: auto scan failed directory_id=%d target_count=%d path=%s error=%v",
				dir.ID, len(targetsForDir), filepath.Base(filepath.Clean(dir.Path)), err)
		}
		if result != nil {
			for _, id := range result.AffectedAnimeIDs {
				affected[id] = struct{}{}
			}
		}
	}

	log.Printf("DownloadLogWorker: auto scan completed directories=%d targets=%d affected_animes=%d",
		len(scanTargets), len(targets), len(affected))
	publishCompletedDownloadEvents(targets)
	return sortedUintIDs(affected)
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
