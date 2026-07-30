package api

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

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
		}
		addLiveTorrentEpisodeIdentities(byEpisode, torrent)
	}

	for index := range logs {
		torrent, ok := liveTorrentForLogWithEpisodes(logs[index], byHash, byName, byEpisode)
		if !ok {
			continue
		}
		if liveStatus := service.DownloadLogStatusFromTorrent(torrent); liveStatus != "" {
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
	// Archived rows are retained only as audit evidence. Never attach a live
	// qBittorrent snapshot to them or they can appear active again in history.
	if strings.EqualFold(strings.TrimSpace(logEntry.Status), "archived") {
		return downloader.TorrentInfo{}, false
	}
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
			if value := parser.NormalizeSeasonNumber(logEntry.SeasonVal); value != "" {
				season = value
			}
			if torrent, ok := uniqueRelatedLiveTorrent(logEntry, byEpisode[torrentEpisodeKey(season, episode)]); ok {
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

func addLiveTorrentEpisodeIdentities(index map[string][]downloader.TorrentInfo, torrent downloader.TorrentInfo) {
	if index == nil {
		return
	}
	seen := make(map[string]struct{})
	add := func(season, episode string) {
		episode = parser.NormalizeEpisodeNumber(episode)
		if episode == "" {
			return
		}
		key := torrentEpisodeKey(season, episode)
		identity := key + "\x00" + liveTorrentIdentity(torrent)
		if _, exists := seen[identity]; exists {
			return
		}
		seen[identity] = struct{}{}
		index[key] = append(index[key], torrent)
	}
	if season, episode := parser.EpisodeIdentityFromTitle(torrent.Name); episode != "" {
		add(season, episode)
	}
	if season, episodes := parser.EpisodeIdentitiesFromPath(subscriptionTorrentContentPath(torrent)); len(episodes) > 0 {
		for _, episode := range episodes {
			add(season, episode)
		}
	}
}

func uniqueRelatedLiveTorrent(logEntry model.DownloadLog, candidates []downloader.TorrentInfo) (downloader.TorrentInfo, bool) {
	var best downloader.TorrentInfo
	identity := ""
	found := false
	for _, candidate := range candidates {
		if !liveProgressTitlesRelated(logEntry.Title, candidate) {
			continue
		}
		candidateIdentity := liveTorrentIdentity(candidate)
		if !found {
			best = candidate
			identity = candidateIdentity
			found = true
			continue
		}
		if candidateIdentity == "" || identity == "" || candidateIdentity != identity {
			// Showing no live progress is preferable to displaying another
			// series' same-number episode. The background synchronizer has
			// subscription paths available and can resolve the safe match.
			return downloader.TorrentInfo{}, false
		}
		best = preferTorrent(best, candidate)
	}
	return best, found
}

func liveProgressTitlesRelated(logTitle string, torrent downloader.TorrentInfo) bool {
	expected := compactProgressSeriesTitle(logTitle)
	if expected == "" {
		return false
	}
	for _, candidate := range []string{torrent.Name, subscriptionTorrentContentPath(torrent)} {
		actual := compactProgressSeriesTitle(candidate)
		if actual == "" {
			continue
		}
		if actual == expected || (len([]rune(actual)) >= 4 && len([]rune(expected)) >= 4 &&
			(strings.Contains(actual, expected) || strings.Contains(expected, actual))) {
			return true
		}
	}
	return false
}

func compactProgressSeriesTitle(raw string) string {
	raw = strings.ReplaceAll(strings.TrimSpace(raw), `\`, "/")
	parsed := parser.ParseFilename(raw)
	title := strings.TrimSpace(parsed.Title)
	if title == "" {
		title = parser.CleanTitle(raw)
	}
	var builder strings.Builder
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func liveTorrentIdentity(torrent downloader.TorrentInfo) string {
	if hash := strings.ToLower(strings.TrimSpace(torrent.Hash)); hash != "" {
		return "hash:" + hash
	}
	return "path:" + strings.ToLower(strings.ReplaceAll(strings.TrimSpace(subscriptionTorrentContentPath(torrent)), `\`, "/"))
}

func subscriptionTorrentContentPath(torrent downloader.TorrentInfo) string {
	if value := strings.TrimSpace(torrent.ContentPath); value != "" {
		return value
	}
	savePath := strings.TrimRight(strings.TrimSpace(torrent.SavePath), `/\`)
	name := strings.TrimLeft(strings.TrimSpace(torrent.Name), `/\`)
	if savePath == "" {
		return name
	}
	if name == "" {
		return savePath
	}
	separator := "/"
	if strings.Contains(savePath, `\`) && !strings.Contains(savePath, "/") {
		separator = `\`
	}
	return savePath + separator + name
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
	currentRank := liveTorrentStatusRank(current)
	candidateRank := liveTorrentStatusRank(candidate)
	if candidateRank != currentRank {
		if candidateRank > currentRank {
			return candidate
		}
		return current
	}
	currentRatio := normalizeTorrentProgress(current.Progress)
	candidateRatio := normalizeTorrentProgress(candidate.Progress)
	if candidateRatio > currentRatio {
		return candidate
	}
	if candidateRatio == currentRatio && candidate.DownloadSpeed > current.DownloadSpeed {
		return candidate
	}
	return current
}

func liveTorrentStatusRank(torrent downloader.TorrentInfo) int {
	switch service.DownloadLogStatusFromTorrent(torrent) {
	case "completed":
		return 3
	case "downloading":
		return 2
	case "failed":
		return 1
	default:
		return 0
	}
}
