package service

import (
	"fmt"
	"path"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

// TorrentRenameSource is the qBittorrent capability required by the automatic
// post-download organizer.
type TorrentRenameSource interface {
	ListTorrents() ([]downloader.TorrentInfo, error)
	SetLocation(hash, location string) error
	RenameFile(hash, oldPath, newPath string) error
}

type AutoRenameResult struct {
	Renamed      int
	Skipped      int
	Failed       int
	Targets      []string
	Replacements map[string]string
}

// AutoRenameCompletedDownloads renames completed single-video torrents through
// qBittorrent itself. This keeps torrent state and seeding intact, including
// when qBittorrent is running on another machine.
func AutoRenameCompletedDownloads(source TorrentRenameSource) (AutoRenameResult, error) {
	result := AutoRenameResult{Replacements: map[string]string{}}
	if source == nil || !autoRenameEnabled() {
		return result, nil
	}

	logStore := downloadLogStore()
	if logStore == nil {
		return result, nil
	}
	logs, err := logStore.ListByStatuses([]string{downloadLogStatusCompleted})
	if err != nil {
		return result, err
	}
	if len(logs) == 0 {
		return result, nil
	}

	subs, err := loadAllSubscriptions()
	if err != nil {
		return result, err
	}
	subscriptions := make(map[uint]model.Subscription, len(subs))
	for _, sub := range subs {
		subscriptions[sub.ID] = sub
	}

	torrents, err := source.ListTorrents()
	if err != nil {
		return result, err
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

	seenTargets := map[string]struct{}{}
	for _, logEntry := range logs {
		sub, ok := subscriptions[logEntry.SubscriptionID]
		if !ok || strings.TrimSpace(logEntry.Episode) == "" {
			result.Skipped++
			continue
		}
		torrent, ok := matchTorrentForLog(logEntry, byHash, byName)
		if !ok || strings.TrimSpace(torrent.Hash) == "" {
			result.Skipped++
			continue
		}

		currentPath := torrentContentPath(torrent)
		ext := strings.ToLower(path.Ext(slashPath(currentPath)))
		if !isRenameableVideoExtension(ext) {
			// Multi-file torrents expose their root directory as content_path.
			// They are intentionally left untouched rather than guessing which
			// contained file belongs to this RSS entry.
			result.Skipped++
			continue
		}

		oldRelative := torrentRelativeMediaPath(torrent, currentPath)
		if oldRelative == "" {
			result.Skipped++
			continue
		}
		original := strings.TrimSuffix(path.Base(slashPath(currentPath)), ext)
		newRelative, formatErr := mediaEpisodeFilename(&sub, logEntry.SeasonVal, logEntry.Episode, ext, original)
		if formatErr != nil {
			result.Failed++
			continue
		}

		baseDir := strings.TrimSpace(configValue(model.ConfigKeyBaseDir))
		var targetDir string
		if strings.TrimSpace(sub.SavePath) != "" {
			targetDir = joinTorrentPath(sub.SavePath, mediaSeasonDirectory(&sub, logEntry.SeasonVal))
		} else if baseDir != "" {
			targetDir, formatErr = mediaTargetDirectory(&sub, logEntry.SeasonVal, baseDir)
		} else {
			targetDir, formatErr = mediaTargetDirectory(&sub, logEntry.SeasonVal, "downloads")
		}
		if formatErr != nil {
			result.Failed++
			continue
		}
		newTarget := joinTorrentPath(targetDir, newRelative)
		oldTarget := strings.TrimSpace(logEntry.TargetFile)
		if oldTarget == "" {
			oldTarget = currentPath
		}
		if oldRelative != newRelative {
			if err := source.RenameFile(torrent.Hash, oldRelative, newRelative); err != nil {
				result.Failed++
				continue
			}
		}
		if !sameTorrentDirectory(torrent.SavePath, targetDir) {
			if err := source.SetLocation(torrent.Hash, targetDir); err != nil {
				result.Failed++
				continue
			}
		}
		if err := logStore.UpdateByID(logEntry.ID, map[string]interface{}{
			"status":      downloadLogStatusRenamed,
			"target_file": newTarget,
		}); err != nil {
			return result, fmt.Errorf("save automatic rename result: %w", err)
		}

		result.Renamed++
		result.Replacements[oldTarget] = newTarget
		if _, exists := seenTargets[newTarget]; !exists {
			seenTargets[newTarget] = struct{}{}
			result.Targets = append(result.Targets, newTarget)
		}
	}
	return result, nil
}

func autoRenameEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(configValue(model.ConfigKeyAutoRenameEnabled)))
	return value == "" || value == model.ConfigValueTrue || value == "1" || value == "yes" || value == "on"
}

func torrentContentPath(torrent downloader.TorrentInfo) string {
	if value := strings.TrimSpace(torrent.ContentPath); value != "" {
		return value
	}
	return joinTorrentPath(torrent.SavePath, torrent.Name)
}

func torrentRelativeMediaPath(torrent downloader.TorrentInfo, fullPath string) string {
	full := strings.TrimPrefix(slashPath(fullPath), "./")
	save := strings.TrimSuffix(strings.TrimPrefix(slashPath(torrent.SavePath), "./"), "/")
	if save != "" {
		prefix := save + "/"
		if len(full) > len(prefix) && strings.EqualFold(full[:len(prefix)], prefix) {
			return strings.TrimPrefix(path.Clean(full[len(prefix):]), "./")
		}
	}
	base := path.Base(full)
	if base == "." || base == "/" {
		return ""
	}
	return base
}

func joinTorrentPath(base, relative string) string {
	base = strings.TrimRight(strings.TrimSpace(base), `/\`)
	relative = strings.TrimLeft(strings.TrimSpace(relative), `/\`)
	separator := "/"
	if strings.Contains(base, `\`) && !strings.Contains(base, "/") {
		separator = `\`
		relative = strings.ReplaceAll(relative, "/", `\`)
	}
	if base == "" {
		return relative
	}
	if relative == "" {
		return base
	}
	return base + separator + relative
}

func sameTorrentDirectory(a, b string) bool {
	a = strings.TrimRight(strings.ToLower(slashPath(a)), "/")
	b = strings.TrimRight(strings.ToLower(slashPath(b)), "/")
	return a != "" && a == b
}

func slashPath(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
}

func isRenameableVideoExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mp4", ".mkv", ".avi", ".mov", ".flv", ".wmv", ".ts", ".rmvb", ".webm", ".m2ts", ".m4v", ".mpg", ".mpeg", ".mts", ".vob":
		return true
	default:
		return false
	}
}

func MergeCompletedTargets(targets []string, renamed AutoRenameResult) []string {
	merged := make([]string, 0, len(targets)+len(renamed.Targets))
	seen := make(map[string]struct{}, len(targets)+len(renamed.Targets))
	for _, target := range targets {
		if replacement, ok := renamed.Replacements[target]; ok {
			target = replacement
		}
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		merged = append(merged, target)
	}
	for _, target := range renamed.Targets {
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		merged = append(merged, target)
	}
	return merged
}
