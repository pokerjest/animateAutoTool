package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/jellyfin"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"

	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

// FileInfo 简化的文件信息结构
type FileInfo struct {
	Name    string              `json:"name"`
	Path    string              `json:"path"`
	Size    int64               `json:"size"`
	Ext     string              `json:"ext"`
	Episode *model.LocalEpisode `json:"episode"` // Link to DB record if exists
}

// RenamePreviewItem 重命名预览条目
type RenamePreviewItem struct {
	AnimeName string `json:"anime_name"` // 所属番剧名 (for display)
	Original  string `json:"original"`
	New       string `json:"new"`
	Path      string `json:"path"` // 原完整路径 for execution
}

// RenameRequest 重命名请求体
type RenameRequest struct {
	Pattern  string `json:"pattern"`   // e.g. "{Title} S{Season}E{Ep}"
	Season   string `json:"season"`    // e.g. "01" (Deprecated at dir level? Or global override?)
	IsManual bool   `json:"is_manual"` // If true, don't auto-append extension etc.
}

// EpisodeDisplay 展示用的集数信息
type EpisodeDisplay struct {
	ID        uint    `json:"id"` // 0 if not in DB
	Name      string  `json:"name"`
	Path      string  `json:"path"`
	Size      int64   `json:"size"`
	Episode   int     `json:"episode"`
	Season    int     `json:"season"`
	Playable  bool    `json:"playable"`
	Watched   bool    `json:"watched"`
	Thumbnail string  `json:"thumbnail"`
	Overview  string  `json:"overview"`
	Rating    float64 `json:"rating"`
	AirDate   string  `json:"air_date"`
	Duration  string  `json:"duration"`
}

// CollectionStatus 收藏状态信息
type CollectionStatus struct {
	BangumiCollected    bool   `json:"bangumi_collected"`
	AniListCollected    bool   `json:"anilist_collected"`
	BangumiWatchedCount int    `json:"bangumi_watched_count"`
	AniListWatchedCount int    `json:"anilist_watched_count"`
	BangumiStatus       int    `json:"bangumi_status"` // 1=想看, 2=看过, 3=在看, 4=搁置, 5=抛弃
	AniListStatus       string `json:"anilist_status"` // CURRENT, COMPLETED, etc.
}

// EpisodeListResponse 增强的剧集列表响应
type EpisodeListResponse struct {
	Episodes         []EpisodeDisplay  `json:"episodes"`
	CollectionStatus *CollectionStatus `json:"collection_status,omitempty"`
}

// GetLocalAnimeFilesHandler 获取指定本地番剧的文件列表
func GetLocalAnimeFilesHandler(c *gin.Context) {
	id := c.Param("id")

	// 1. Try fetching from LocalEpisodes (DB)
	idUint, _ := strconv.ParseUint(id, 10, 64)
	if laStore := localAnimeStore(); laStore != nil {
		if episodes, err := laStore.ListEpisodesByAnimeIDOrdered(uint(idUint)); err == nil && len(episodes) > 0 {
			handleDBEpisodeList(c, id, episodes)
			return
		}
	}

	// 2. Fallback to file system
	handleFileSystemFileList(c, id)
}

func handleDBEpisodeList(c *gin.Context, id string, episodes []model.LocalEpisode) {
	// Preload anime for metadata fallback
	laStore := localAnimeStore()
	if laStore == nil {
		jsonNotFound(c, "数据库未初始化")
		return
	}
	anime, err := laStore.GetWithMetadata(id)
	if err != nil {
		log.Printf("ERROR: Found episodes but failed to load parent anime %s: %v", id, err)
		jsonNotFound(c, "未找到关联的本地番剧")
		return
	}

	// Fetch Jellyfin Status (Best Effort)
	jfMap, jellyfinUrl := fetchJellyfinProgress(anime)

	// --- Source Progress Logic Overlay ---
	sourceParam := c.Query("source") // Optional: source being previewed in modal
	effectiveSource := ""
	if anime.Metadata != nil && anime.Metadata.DataSource != "" {
		effectiveSource = anime.Metadata.DataSource
	}
	if sourceParam != "" {
		effectiveSource = sourceParam // Override with preview source
	}

	log.Printf("DEBUG: GetLocalAnimeFilesHandler for AnimeID=%d | sourceParam='%s' | effectiveSource='%s' | hasMetadata=%v",
		anime.ID, sourceParam, effectiveSource, anime.Metadata != nil)

	bangumiWatchedCount, bangumiCollectionStatus := fetchBangumiProgress(anime, effectiveSource)
	anilistWatchedCount, anilistStatus := fetchAniListProgress(anime, effectiveSource)

	display := buildEpisodeList(episodes, anime, jfMap, jellyfinUrl, bangumiWatchedCount, anilistWatchedCount)

	// Build collection status
	collStatus := &CollectionStatus{
		BangumiCollected:    bangumiWatchedCount >= 0, // >=0 means we got a valid response
		AniListCollected:    anilistWatchedCount >= 0,
		BangumiWatchedCount: max(0, bangumiWatchedCount),
		AniListWatchedCount: max(0, anilistWatchedCount),
		BangumiStatus:       bangumiCollectionStatus,
		AniListStatus:       anilistStatus,
	}

	// Return enhanced response
	c.JSON(http.StatusOK, EpisodeListResponse{
		Episodes:         display,
		CollectionStatus: collStatus,
	})
}

func handleFileSystemFileList(c *gin.Context, id string) {
	laStore := localAnimeStore()
	if laStore == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "数据库未初始化"})
		return
	}
	animePtr, err := laStore.GetAnime(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "未找到番剧记录"})
		return
	}
	anime := *animePtr

	animeIDInt, _ := strconv.Atoi(id)
	files, err := listAnimeFiles(anime.Path, uint(animeIDInt))
	if err != nil {
		jsonServerError(c, "读取本地文件列表", err)
		return
	}

	var display []EpisodeDisplay
	for _, f := range files {
		display = append(display, EpisodeDisplay{
			ID:       0,
			Name:     f.Name,
			Path:     f.Path,
			Size:     f.Size,
			Playable: false,
		})
	}

	c.JSON(http.StatusOK, display)
}

func RefreshLocalAnimeMetadataHandler(c *gin.Context) {
	id := c.Param("id")
	laStore := localAnimeStore()
	if laStore == nil {
		htmlNotFound(c, "数据库未初始化")
		return
	}
	animePtr, err := laStore.GetWithMetadata(id)
	if err != nil {
		htmlNotFound(c, "未找到本地番剧")
		return
	}
	anime := *animePtr

	metaSvc := service.NewMetadataService()

	// Emit Start Event
	event.GlobalBus.Publish(event.EventMetadataUpdated, map[string]interface{}{
		"type":    "progress",
		"current": 1,
		"total":   1,
		"title":   anime.Title,
	})

	refreshSucceeded := true
	if err := metaSvc.EnrichAnime(&anime); err != nil {
		refreshSucceeded = false
		log.Printf("Failed to enrich anime %s: %v", anime.Title, err)
		_ = service.ReportLibraryIssue(service.LibraryIssueInput{
			IssueKey:      "scrape:" + strconv.FormatUint(uint64(anime.ID), 10),
			IssueType:     service.LibraryIssueTypeScrape,
			Title:         anime.Title,
			DirectoryPath: anime.Path,
			LocalAnimeID:  &anime.ID,
			Message:       err.Error(),
			Hint:          "检查元数据源配置，或在详情里使用修正匹配手动关联番剧。",
		})
	} else {
		_ = service.ResolveLibraryIssue("scrape:" + strconv.FormatUint(uint64(anime.ID), 10))
	}

	// Emit Complete Event
	event.GlobalBus.Publish(event.EventMetadataUpdated, map[string]interface{}{
		"type":    "complete",
		"message": "刷新完成",
	})

	if err := db.DB.Save(&anime).Error; err != nil {
		htmlServerError(c, "保存本地番剧信息", err)
		return
	}

	populateLocalAnimeActionHint(&anime)
	if refreshSucceeded {
		triggerAppToast(c, repairSuccessToast(repairActionRetryScrape), "success")
	} else {
		triggerAppToast(c, repairReviewToast(repairActionRetryScrape), "error")
	}

	c.HTML(http.StatusOK, "local_anime_card.html", anime)
}

// SwitchLocalAnimeSourceHandler 切换数据源
func SwitchLocalAnimeSourceHandler(c *gin.Context) {
	id := c.Param("id")
	source := c.Query("source")
	log.Printf("DEBUG: Switch Source Request for ID %s to '%s'", id, source)

	laStore := localAnimeStore()
	if laStore == nil {
		htmlNotFound(c, "数据库未初始化")
		return
	}
	animePtr, err := laStore.GetWithMetadata(id)
	if err != nil {
		htmlNotFound(c, "未找到本地番剧")
		return
	}
	anime := *animePtr

	if anime.Metadata == nil {
		htmlBadRequest(c, "当前本地番剧还没有关联元数据")
		return
	}

	m := anime.Metadata
	switch source {
	case SourceTMDB:
		if m.TMDBID != 0 {
			m.Title = m.TMDBTitle
			m.Image = m.TMDBImage
			m.Summary = m.TMDBSummary
			m.DataSource = SourceTMDB
		}
	case SourceBangumi:
		if m.BangumiID != 0 {
			m.Title = m.BangumiTitle
			m.Image = m.BangumiImage
			m.Summary = m.BangumiSummary
			m.DataSource = SourceBangumi
		}
	case SourceAniList:
		if m.AniListID != 0 {
			m.Title = m.AniListTitle
			m.Image = m.AniListImage
			m.Summary = m.AniListSummary
			m.DataSource = SourceAniList
		}
	}

	if err := db.DB.Save(m).Error; err != nil {
		htmlServerError(c, "切换数据源", err)
		return
	}

	// Trigger global sync (this will update 'anime' results too)
	metaSvc := service.NewMetadataService()
	metaSvc.SyncMetadataToModels(m)
	populateLocalAnimeActionHint(&anime)

	c.HTML(http.StatusOK, "local_anime_card.html", anime)
}

// PreviewDirectoryRenameHandler 预览目录下所有番剧的重命名
func PreviewDirectoryRenameHandler(c *gin.Context) {
	id := c.Param("id")
	var req RenameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonBadRequest(c, "重命名预览请求格式不正确")
		return
	}

	// 1. Get Directory
	log.Printf("DEBUG: Preview Rename Request: Pattern='%s', Season='%s', Manual=%v", req.Pattern, req.Season, req.IsManual)
	laStore := localAnimeStore()
	if laStore == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "数据库未初始化"})
		return
	}
	dir, err := laStore.GetDirectory(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "未找到目录记录"})
		return
	}

	// 2. Get All Anime in this Directory
	animeList, err := laStore.ListAnimesByDirectory(dir.ID)
	if err != nil {
		jsonServerError(c, "读取目录下的番剧列表", err)
		return
	}

	var allPreviews []RenamePreviewItem

	// 3. Loop and Generate
	for _, anime := range animeList {
		files, err := listAnimeFiles(anime.Path, anime.ID)
		if err != nil {
			continue // Skip bad ones
		}

		previews := generateRenamePreview(files, anime, req)
		for i := range previews {
			previews[i].AnimeName = anime.Title
		}
		allPreviews = append(allPreviews, previews...)
	}

	c.JSON(http.StatusOK, allPreviews)
}

// ApplyDirectoryRenameHandler 执行目录级别的批量重命名
func ApplyDirectoryRenameHandler(c *gin.Context) {
	id := c.Param("id")
	var req RenameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonBadRequest(c, "批量重命名请求格式不正确")
		return
	}

	laStore := localAnimeStore()
	if laStore == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "数据库未初始化"})
		return
	}
	dir, err := laStore.GetDirectory(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "未找到目录记录"})
		return
	}

	animeList, err := laStore.ListAnimesByDirectory(dir.ID)
	if err != nil {
		jsonServerError(c, "读取目录下的番剧列表", err)
		return
	}

	successCount := 0
	failCount := 0
	refreshedSeries := make(map[string]struct{})

	for _, anime := range animeList {
		files, err := listAnimeFiles(anime.Path, anime.ID)
		if err != nil {
			continue
		}

		previews := generateRenamePreview(files, anime, req)

		for _, item := range previews {
			if item.New == item.Original {
				continue // Skip unchanged
			}

			oldPath := item.Path
			var episode model.LocalEpisode
			episodePtr, episodeErr := laStore.FindEpisodeByPath(oldPath)
			episodeFound := episodeErr == nil && episodePtr != nil
			if episodeFound {
				episode = *episodePtr
			}
			newPath, err := buildSafeRenamePath(anime.Path, item.New)
			if err != nil {
				log.Printf("Rename skipped for %s: %v", oldPath, err)
				failCount++
				continue
			}

			if oldPath == newPath {
				continue
			}

			renamedByQB := false
			if ok, err := renameManagedQBFile(oldPath, newPath); err != nil {
				log.Printf("qB rename failed for %s -> %s: %v", oldPath, newPath, err)
			} else {
				renamedByQB = ok
			}

			if !renamedByQB {
				// Ensure parent directory exists (for Season folders)
				newDir := filepath.Dir(newPath)
				if err := os.MkdirAll(newDir, 0755); err != nil {
					fmt.Printf("Failed to create directory %s: %v\n", newDir, err)
					failCount++
					continue
				}

				if err := os.Rename(oldPath, newPath); err != nil {
					fmt.Printf("Rename failed: %s -> %s (%v)\n", oldPath, newPath, err)
					failCount++
					continue
				}
			}

			successCount++
			if episodeFound {
				_ = laStore.UpdateEpisodePathByID(episode.ID, newPath)
				episode.Path = newPath
			} else {
				_ = laStore.UpdateEpisodePathByOldPath(oldPath, newPath)
			}
			if logStore := downloadLogStore(); logStore != nil {
				_ = logStore.UpdateTargetFileByOld(oldPath, newPath)
			}
			backfillRenamedDownloadLog(anime, episode, oldPath, newPath)
			if anime.JellyfinSeriesID != "" {
				refreshedSeries[anime.JellyfinSeriesID] = struct{}{}
			}
		}
	}
	syncDownloadLogsFromQB()
	triggerJellyfinRefreshForSeries(c.Request.Context(), refreshedSeries)

	msg := fmt.Sprintf("批量整理完成: 成功 %d, 失败 %d", successCount, failCount)
	c.JSON(http.StatusOK, gin.H{"message": msg, "success": successCount, "failed": failCount})
}

func triggerJellyfinRefreshForSeries(ctx context.Context, seriesIDs map[string]struct{}) {
	if len(seriesIDs) == 0 {
		return
	}
	urlValue := configValue(model.ConfigKeyJellyfinUrl)
	apiKey := configValue(model.ConfigKeyJellyfinApiKey)
	if strings.TrimSpace(urlValue) == "" || strings.TrimSpace(apiKey) == "" {
		return
	}

	client := jellyfin.NewClient(urlValue, apiKey)
	for seriesID := range seriesIDs {
		if err := client.RefreshItemContext(ctx, seriesID); err != nil {
			log.Printf("Jellyfin series refresh failed for %s: %v", seriesID, err)
		}
	}
}

func backfillRenamedDownloadLog(anime model.LocalAnime, episode model.LocalEpisode, oldPath, newPath string) {
	if db.DB == nil || episode.EpisodeNum <= 0 {
		return
	}

	var subscriptionIDs []uint
	query := db.DB.Model(&model.Subscription{})
	if anime.MetadataID != nil {
		query = query.Where("metadata_id = ?", *anime.MetadataID)
	} else {
		query = query.Where("title = ?", anime.Title)
	}
	if err := query.Pluck("id", &subscriptionIDs).Error; err != nil || len(subscriptionIDs) == 0 {
		return
	}

	updates := map[string]interface{}{
		"target_file": newPath,
		"status":      "completed",
	}
	episodeVal := fmt.Sprintf("%02d", episode.EpisodeNum)
	seasonVal := fmt.Sprintf("S%02d", max(1, episode.SeasonNum))

	tx := db.DB.Model(&model.DownloadLog{}).
		Where("subscription_id IN ?", subscriptionIDs).
		Where("episode = ?", episodeVal).
		Where("(season_val = ? OR season_val = '' OR season_val IS NULL)", seasonVal).
		Where("(target_file = ? OR target_file = '' OR target_file IS NULL)", oldPath).
		Updates(updates)
	if tx.Error != nil {
		log.Printf("Download log backfill failed for %s -> %s: %v", oldPath, newPath, tx.Error)
		return
	}
	if tx.RowsAffected > 0 {
		log.Printf("Backfilled %d download logs for %s -> %s", tx.RowsAffected, oldPath, newPath)
	}
}

func syncDownloadLogsFromQB() {
	qbURL, qbUsername, qbPassword := FetchQBConfig()
	if strings.TrimSpace(qbURL) == "" {
		return
	}

	client := downloader.NewQBittorrentClient(qbURL)
	if err := client.Login(qbUsername, qbPassword); err != nil {
		log.Printf("Download log sync skipped: qB login failed: %v", err)
		return
	}

	if _, err := service.SyncDownloadLogStatusesWithQBClient(client); err != nil {
		log.Printf("Download log sync skipped: %v", err)
	}
}

func renameManagedQBFile(oldPath, newPath string) (bool, error) {
	qbURL, qbUsername, qbPassword := FetchQBConfig()
	if strings.TrimSpace(qbURL) == "" {
		return false, nil
	}

	client := downloader.NewQBittorrentClient(qbURL)
	if err := client.Login(qbUsername, qbPassword); err != nil {
		return false, err
	}

	torrents, err := client.ListTorrents()
	if err != nil {
		return false, err
	}

	for _, torrent := range torrents {
		if !sameFilesystemPath(torrent.ContentPath, oldPath) {
			continue
		}
		oldRelative, err := torrentRelativePath(torrent, oldPath)
		if err != nil {
			return false, err
		}
		newRelative := filepath.ToSlash(filepath.Join(filepath.Dir(oldRelative), filepath.Base(newPath)))
		if filepath.ToSlash(oldRelative) == filepath.ToSlash(newRelative) {
			return true, nil
		}
		if err := client.RenameFile(torrent.Hash, oldRelative, newRelative); err != nil {
			return false, err
		}
		return true, nil
	}

	return false, nil
}

func sameFilesystemPath(a, b string) bool {
	cleanA := filepath.Clean(strings.TrimSpace(a))
	cleanB := filepath.Clean(strings.TrimSpace(b))
	return cleanA != "" && cleanA == cleanB
}

func torrentRelativePath(torrent downloader.TorrentInfo, fullPath string) (string, error) {
	savePath := strings.TrimSpace(torrent.SavePath)
	if savePath != "" {
		if rel, err := filepath.Rel(savePath, fullPath); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel), nil
		}
	}
	base := filepath.Base(fullPath)
	if strings.TrimSpace(base) == "" || base == "." || base == string(filepath.Separator) {
		return "", fmt.Errorf("cannot derive qB relative path for %s", fullPath)
	}
	return filepath.ToSlash(base), nil
}

// Helpers

func listAnimeFiles(rootPath string, animeID uint) ([]FileInfo, error) {
	var files []FileInfo

	// Fetch existing episodes from DB to get technical tags
	var dbEpisodes []model.LocalEpisode
	db.DB.Where("local_anime_id = ?", animeID).Find(&dbEpisodes)
	epMap := make(map[string]model.LocalEpisode)
	for _, e := range dbEpisodes {
		epMap[e.Path] = e
	}

	err := filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if isVideoExt(ext) {
				info, _ := d.Info()
				f := FileInfo{
					Name: d.Name(),
					Path: path,
					Size: info.Size(),
					Ext:  ext,
				}
				if dbEp, ok := epMap[path]; ok {
					f.Episode = &dbEp
				}
				files = append(files, f)
			}
		}
		return nil
	})

	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})

	return files, err
}

func isVideoExt(ext string) bool {
	switch ext {
	case ".mp4", ".mkv", ".avi", ".mov", ".flv", ".wmv", ".ts", ".rmvb", ".webm", ".m2ts":
		return true
	}
	return false
}

func buildSafeRenamePath(rootPath, relativePath string) (string, error) {
	cleanRoot := filepath.Clean(rootPath)
	cleanRelative := filepath.Clean(relativePath)

	if filepath.IsAbs(cleanRelative) {
		return "", errors.New("absolute target paths are not allowed")
	}

	targetPath := filepath.Clean(filepath.Join(cleanRoot, cleanRelative))
	rel, err := filepath.Rel(cleanRoot, targetPath)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("target path escapes the anime root")
	}

	return targetPath, nil
}

func generateRenamePreview(files []FileInfo, anime model.LocalAnime, req RenameRequest) []RenamePreviewItem {
	var results []RenamePreviewItem

	// Default patterns
	pattern := req.Pattern
	if pattern == "" {
		pattern = "{Title} - S{Season}E{Ep}.{Ext}"
	}

	// Prepare Metadata Variables
	titleCN := anime.Title
	titleJP := anime.Title
	titleEN := anime.Title
	year := ""

	if anime.Metadata != nil {
		if anime.Metadata.TitleCN != "" {
			titleCN = anime.Metadata.TitleCN
		}
		if anime.Metadata.TitleJP != "" {
			titleJP = anime.Metadata.TitleJP
		}
		if anime.Metadata.TitleEN != "" {
			titleEN = anime.Metadata.TitleEN
		}
		if len(anime.Metadata.AirDate) >= 4 {
			year = anime.Metadata.AirDate[:4]
		}
	} else if len(anime.AirDate) >= 4 {
		year = anime.AirDate[:4]
	}

	for _, f := range files {
		// Use parser for initial pass if DB info is missing
		var parsed parser.ParsedInfo
		if f.Episode != nil {
			parsed = parser.ParsedInfo{
				Title:      f.Episode.ParsedTitle,
				Season:     f.Episode.SeasonNum,
				Episode:    f.Episode.EpisodeNum,
				Resolution: f.Episode.Resolution,
				Group:      f.Episode.SubGroup,
				Extension:  f.Episode.Container,
				VideoCodec: f.Episode.VideoCodec,
				AudioCodec: f.Episode.AudioCodec,
				BitDepth:   f.Episode.BitDepth,
				Source:     f.Episode.Source,
			}
		} else {
			parsed = parser.ParseFilename(f.Path)
		}

		if parsed.Episode == 0 {
			results = append(results, RenamePreviewItem{
				Original: f.Name,
				New:      f.Name,
				Path:     f.Path,
			})
			continue
		}

		// Determine Season for this file
		epSeasonVal := req.Season // Priority 1: User Override
		if epSeasonVal == "" {
			// Priority 2: Per-Episode Season (from DB or Parser)
			if parsed.Season > 0 {
				epSeasonVal = strconv.Itoa(parsed.Season)
			} else if anime.Season > 0 {
				// Priority 3: Series Level Default
				epSeasonVal = strconv.Itoa(anime.Season)
			} else {
				epSeasonVal = "01"
			}
		}
		// Pad to 2 digits
		if len(epSeasonVal) == 1 {
			epSeasonVal = "0" + epSeasonVal
		}

		newName := pattern
		// 1. Basic Variables
		newName = strings.ReplaceAll(newName, "{Title}", anime.Title)
		newName = strings.ReplaceAll(newName, "{TitleCN}", titleCN)
		newName = strings.ReplaceAll(newName, "{TitleJP}", titleJP)
		newName = strings.ReplaceAll(newName, "{TitleEN}", titleEN)
		newName = strings.ReplaceAll(newName, "{Year}", year)
		newName = strings.ReplaceAll(newName, "{Season}", epSeasonVal)

		// 2. Technical Variables
		newName = strings.ReplaceAll(newName, "{SubGroup}", parsed.Group)
		newName = strings.ReplaceAll(newName, "{Resolution}", parsed.Resolution)
		newName = strings.ReplaceAll(newName, "{VideoCodec}", parsed.VideoCodec)
		newName = strings.ReplaceAll(newName, "{AudioCodec}", parsed.AudioCodec)
		newName = strings.ReplaceAll(newName, "{BitDepth}", parsed.BitDepth)
		newName = strings.ReplaceAll(newName, "{10bit}", parsed.BitDepth) // Alias
		newName = strings.ReplaceAll(newName, "{Source}", parsed.Source)

		// 3. Episode Padding
		epNum := strconv.Itoa(parsed.Episode)
		if parsed.Episode < 10 {
			epNum = "0" + epNum
		}
		newName = strings.ReplaceAll(newName, "{Ep}", epNum)

		// 4. Extension
		ext := strings.TrimPrefix(parsed.Extension, ".")
		if ext == "" {
			ext = strings.TrimPrefix(filepath.Ext(f.Name), ".")
		}
		newName = strings.ReplaceAll(newName, "{Ext}", ext)

		// 5. Cleanup and Path Normalization
		// Ensure extension if missing in rule
		if !strings.Contains(newName, "."+ext) && !req.IsManual { // Avoid double ext if manual? keep simple
			if !strings.HasSuffix(newName, "."+ext) {
				newName += "." + ext
			}
		}

		if safePath, err := buildSafeRenamePath(anime.Path, newName); err == nil {
			newName, _ = filepath.Rel(anime.Path, safePath)
		} else {
			newName = f.Name
		}

		results = append(results, RenamePreviewItem{
			Original: f.Name,
			New:      newName,
			Path:     f.Path,
		})
	}
	return results
}
