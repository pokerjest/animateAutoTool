package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

const subscriptionProgressTimeout = 8 * time.Second

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
	byEpisode := make(map[string][]downloader.TorrentInfo)
	for _, torrent := range torrents {
		if hash := strings.ToLower(strings.TrimSpace(torrent.Hash)); hash != "" {
			byHash[hash] = preferTorrent(byHash[hash], torrent)
		}
		if name := strings.TrimSpace(torrent.Name); name != "" {
			byName[name] = preferTorrent(byName[name], torrent)
			if normalized := parser.NormalizeReleaseTitle(name); normalized != "" {
				normalizedKey := normalizedTorrentNameKey(normalized)
				byName[normalizedKey] = preferTorrent(byName[normalizedKey], torrent)
			}
			if season, episode := parser.EpisodeIdentityFromTitle(name); episode != "" {
				episodeKey := torrentEpisodeKey(season, episode)
				byEpisode[episodeKey] = append(byEpisode[episodeKey], torrent)
			}
		}
	}

	for index := range logs {
		torrent, ok := liveTorrentForLogWithEpisodes(logs[index], byHash, byName, byEpisode)
		if !ok {
			continue
		}
		if liveStatus := service.DownloadLogStatusFromTorrentState(torrent.State); liveStatus != "" {
			switch logEntryStatus := strings.TrimSpace(logs[index].Status); logEntryStatus {
			case "downloading", "failed":
				logs[index].Status = liveStatus
			}
		}
		ratio := normalizeTorrentProgress(torrent.Progress)
		percent := ratio * 100
		totalBytes := max(0, torrent.Size)
		downloadedBytes := max(0, torrent.Completed)
		// Some qBittorrent versions temporarily return size=0 while the
		// completed byte count and ratio are already available.
		if totalBytes == 0 && downloadedBytes > 0 && ratio > 0 {
			totalBytes = int64(float64(downloadedBytes) / ratio)
		}
		logs[index].ProgressPercent = percent
		logs[index].DownloadedBytes = downloadedBytes
		logs[index].TotalBytes = totalBytes
		logs[index].DownloadSpeed = max(0, torrent.DownloadSpeed)
	}
}

func liveTorrentForLog(logEntry model.DownloadLog, byHash map[string]downloader.TorrentInfo, byName map[string]downloader.TorrentInfo) (downloader.TorrentInfo, bool) {
	return liveTorrentForLogWithEpisodes(logEntry, byHash, byName, nil)
}

func liveTorrentForLogWithEpisodes(logEntry model.DownloadLog, byHash map[string]downloader.TorrentInfo, byName map[string]downloader.TorrentInfo, byEpisode map[string][]downloader.TorrentInfo) (downloader.TorrentInfo, bool) {
	if hash := strings.ToLower(strings.TrimSpace(logEntry.InfoHash)); hash != "" {
		if torrent, ok := byHash[hash]; ok {
			return torrent, true
		}
	}
	if title := strings.TrimSpace(logEntry.Title); title != "" {
		if torrent, ok := byName[title]; ok {
			return torrent, true
		}
		if normalized := parser.NormalizeReleaseTitle(title); normalized != "" {
			if torrent, ok := byName[normalizedTorrentNameKey(normalized)]; ok {
				return torrent, true
			}
		}
	}
	if len(byEpisode) > 0 {
		season, episode := parser.EpisodeIdentityFromTitle(logEntry.Title)
		if episode == "" {
			episode = parser.NormalizeEpisodeNumber(logEntry.Episode)
		}
		if episode != "" {
			if season == "" {
				season = parser.NormalizeSeasonNumber(logEntry.SeasonVal)
			}
			if torrent, ok := bestTorrent(byEpisode[torrentEpisodeKey(season, episode)]); ok {
				return torrent, true
			}
		}
	}
	return downloader.TorrentInfo{}, false
}

func normalizedTorrentNameKey(name string) string {
	return "normalized:" + name
}

func torrentEpisodeKey(season, episode string) string {
	return fmt.Sprintf("%s:%s", parser.NormalizeSeasonNumber(season), parser.NormalizeEpisodeNumber(episode))
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

func preferTorrent(current, candidate downloader.TorrentInfo) downloader.TorrentInfo {
	if strings.TrimSpace(current.Name) == "" {
		return candidate
	}
	currentRatio := normalizeTorrentProgress(current.Progress)
	candidateRatio := normalizeTorrentProgress(candidate.Progress)
	if candidate.DownloadSpeed > current.DownloadSpeed {
		return candidate
	}
	if candidate.DownloadSpeed == current.DownloadSpeed && candidateRatio > currentRatio {
		return candidate
	}
	return current
}

func bestTorrent(candidates []downloader.TorrentInfo) (downloader.TorrentInfo, bool) {
	if len(candidates) == 0 {
		return downloader.TorrentInfo{}, false
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		best = preferTorrent(best, candidate)
	}
	return best, true
}
