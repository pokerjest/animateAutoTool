package service

import (
	"log"
	"path/filepath"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/db"
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

const (
	downloadLogStatusDownloading = "downloading"
	downloadLogStatusCompleted   = "completed"
	downloadLogStatusFailed      = "failed"
)

func SyncDownloadLogStatuses(source TorrentStatusSource) (DownloadLogStatusSyncResult, error) {
	if source == nil {
		return DownloadLogStatusSyncResult{}, nil
	}

	torrents, err := source.ListTorrents()
	if err != nil {
		return DownloadLogStatusSyncResult{}, err
	}

	if db.DB == nil {
		return DownloadLogStatusSyncResult{}, nil
	}

	var logs []model.DownloadLog
	if err := db.DB.Where("status IN ?", []string{downloadLogStatusDownloading, downloadLogStatusFailed}).
		Order("created_at DESC").
		Find(&logs).Error; err != nil {
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
		if err := db.DB.Model(&model.DownloadLog{}).Where("id = ?", logEntry.ID).Updates(updates).Error; err != nil {
			return result, err
		}
		result.Updated++
		if nextStatus == downloadLogStatusCompleted && logEntry.Status != downloadLogStatusCompleted && targetFile != "" {
			result.CompletedTargets = append(result.CompletedTargets, targetFile)
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
		return strings.TrimSpace(torrent.ContentPath)
	}
	if strings.TrimSpace(torrent.SavePath) == "" || strings.TrimSpace(torrent.Name) == "" {
		return ""
	}
	return filepath.Join(strings.TrimSpace(torrent.SavePath), strings.TrimSpace(torrent.Name))
}

func SyncDownloadLogStatusesWithQBClient(client *downloader.QBittorrentClient) (DownloadLogStatusSyncResult, error) {
	if client == nil {
		return DownloadLogStatusSyncResult{}, nil
	}

	result, err := SyncDownloadLogStatuses(client)
	if err != nil {
		log.Printf("Worker: qB download log sync failed: %v", err)
		return result, err
	}

	if result.Updated > 0 {
		log.Printf("Worker: qB download log sync updated %d records (completed=%d failed=%d active=%d unmatched=%d)",
			result.Updated, result.Completed, result.Failed, result.Active, result.Unmatched)
	}
	return result, nil
}
