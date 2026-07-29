package api

import (
	"context"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
)

const subscriptionProgressTimeout = 3 * time.Second

// enrichSubscriptionDownloadProgress adds a best-effort live snapshot from
// qBittorrent to history records. It deliberately does not update the
// database: the background synchronizer remains the source of truth for log
// status transitions, while the UI can show fresh progress immediately.
func enrichSubscriptionDownloadProgress(ctx context.Context, logs []model.DownloadLog) {
	if len(logs) == 0 || ctx == nil {
		return
	}

	qbCfg := qbutil.LoadConfig()
	if qbutil.ManagedBinaryMissing(qbCfg, config.BinDir()) ||
		qbutil.MissingExternalURL(qbCfg) ||
		strings.TrimSpace(qbCfg.URL) == "" {
		return
	}

	progressCtx, cancel := context.WithTimeout(ctx, subscriptionProgressTimeout)
	defer cancel()

	client := downloader.NewQBittorrentClient(qbCfg.URL)
	if err := client.LoginContext(progressCtx, qbCfg.Username, qbCfg.Password); err != nil {
		return
	}
	torrents, err := client.ListTorrentsContext(progressCtx)
	if err != nil {
		return
	}

	byHash := make(map[string]downloader.TorrentInfo, len(torrents))
	byName := make(map[string]downloader.TorrentInfo, len(torrents))
	for _, torrent := range torrents {
		if hash := strings.ToLower(strings.TrimSpace(torrent.Hash)); hash != "" {
			byHash[hash] = torrent
		}
		if name := strings.TrimSpace(torrent.Name); name != "" {
			byName[name] = torrent
		}
	}

	for index := range logs {
		torrent, ok := liveTorrentForLog(logs[index], byHash, byName)
		if !ok {
			continue
		}
		percent := torrent.Progress * 100
		if percent < 0 {
			percent = 0
		}
		if percent > 100 {
			percent = 100
		}
		logs[index].ProgressPercent = percent
		logs[index].DownloadedBytes = max(0, torrent.Completed)
		logs[index].TotalBytes = max(0, torrent.Size)
		logs[index].DownloadSpeed = max(0, torrent.DownloadSpeed)
	}
}

func liveTorrentForLog(logEntry model.DownloadLog, byHash map[string]downloader.TorrentInfo, byName map[string]downloader.TorrentInfo) (downloader.TorrentInfo, bool) {
	if hash := strings.ToLower(strings.TrimSpace(logEntry.InfoHash)); hash != "" {
		if torrent, ok := byHash[hash]; ok {
			return torrent, true
		}
	}
	if title := strings.TrimSpace(logEntry.Title); title != "" {
		torrent, ok := byName[title]
		return torrent, ok
	}
	return downloader.TorrentInfo{}, false
}
