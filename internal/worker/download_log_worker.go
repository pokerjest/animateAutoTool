package worker

import (
	"log"
	"net/url"
	"os"
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

const downloadLogSyncInterval = 90 * time.Second

func StartDownloadLogSyncWorker() {
	go func() {
		syncDownloadLogStatuses()

		ticker := time.NewTicker(downloadLogSyncInterval)
		defer ticker.Stop()

		for range ticker.C {
			syncDownloadLogStatuses()
		}
	}()
}

func syncDownloadLogStatuses() {
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

	autoScanCompletedDownloads(result.CompletedTargets)
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
		if _, err := os.Stat(target); err != nil {
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
