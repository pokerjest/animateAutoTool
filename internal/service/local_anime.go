package service

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"io"
	"net/http"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/anilist"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/tmdb"
	"gorm.io/gorm"
)

type RefreshStatus struct {
	Total        int    `json:"total"`
	Current      int    `json:"current"`
	CurrentTitle string `json:"current_title"`
	IsRunning    bool   `json:"is_running"`
	LastResult   string `json:"last_result"`
}

var GlobalRefreshStatus = RefreshStatus{}

type LocalAnimeService struct{}

func NewLocalAnimeService() *LocalAnimeService {
	s := &LocalAnimeService{}
	// s.CleanupGarbage() // Don't run on every request!
	return s
}

// CleanupGarbage 清理数据库中的无效数据
func (s *LocalAnimeService) CleanupGarbage() {
	// 1. 删除没有视频文件的“垃圾”记录
	if err := db.DB.Unscoped().Where("file_count = 0").Delete(&model.LocalAnime{}).Error; err != nil {
		log.Printf("Cleanup: Failed to remove empty anime entries: %v", err)
	} else {
		log.Println("Cleanup: Removed empty anime entries from DB")
	}

	// 2. 删除孤儿记录 - 当目录被删但番剧没删掉时
	var dirIDs []uint
	db.DB.Model(&model.LocalAnimeDirectory{}).Pluck("id", &dirIDs)
	if len(dirIDs) > 0 {
		// Delete anime where directory_id is not in the list of existing directories
		if err := db.DB.Unscoped().Where("directory_id NOT IN ?", dirIDs).Delete(&model.LocalAnime{}).Error; err != nil {
			log.Printf("Cleanup: Failed to remove orphan entries: %v", err)
		} else {
			log.Println("Cleanup: Removed orphan anime entries")
		}
	} else {
		// If no directories exist, all anime are orphans
		db.DB.Unscoped().Where("1 = 1").Delete(&model.LocalAnime{})
		log.Println("Cleanup: No directories found, wiped all local anime")
	}
}

// AddDirectory 添加一个新的根目录
func (s *LocalAnimeService) AddDirectory(path string) error {
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return os.ErrInvalid // 不是目录
	}

	// Check if exists (including soft-deleted)
	var existing model.LocalAnimeDirectory
	if err := db.DB.Unscoped().Where("path = ?", path).First(&existing).Error; err == nil {
		// Found existing record
		if existing.DeletedAt.Valid {
			// It was soft-deleted. Instead of restoring, we HARD DELETE it to allow a fresh creation.
			// This avoids issues with GORM updates and clean slate.
			log.Printf("Removing stale soft-deleted directory to allow fresh add: %s", path)
			if err := db.DB.Unscoped().Delete(&existing).Error; err != nil {
				return err
			}
			// Fallthrough to Create new below
		} else {
			// Already exists and active, nothing to do
			return nil
		}
	}

	// Not found, create new
	dir := model.LocalAnimeDirectory{
		Path: path,
	}
	return db.DB.Create(&dir).Error
}

// RemoveDirectory 删除目录
func (s *LocalAnimeService) RemoveDirectory(id uint) error {
	return db.DB.Transaction(func(tx *gorm.DB) error {
		// 删除关联的 Anime (Hard Delete)
		if err := tx.Unscoped().Where("directory_id = ?", id).Delete(&model.LocalAnime{}).Error; err != nil {
			return err
		}
		// 删除目录 (Hard Delete)
		if err := tx.Unscoped().Delete(&model.LocalAnimeDirectory{}, id).Error; err != nil {
			return err
		}
		return nil
	})
}

// ScanAll 扫描所有已配置的目录
func (s *LocalAnimeService) ScanAll() error {
	var dirs []model.LocalAnimeDirectory
	if err := db.DB.Find(&dirs).Error; err != nil {
		return err
	}

	for _, d := range dirs {
		if err := s.ScanDirectory(d.ID, d.Path); err != nil {
			log.Printf("Error scanning directory %s: %v", d.Path, err)
			// 继续扫描下一个，不中断
		}
	}

	// Phase 2: Background Enrich
	go s.EnrichMissingMetadata()

	return nil
}

// ScanDirectory 扫描指定目录并将结果存入 DB
func (s *LocalAnimeService) ScanDirectory(dirID uint, rootPath string) error {
	log.Printf("DEBUG: ScanDirectory started for: %s (ID: %d)", rootPath, dirID)

	entries, err := os.ReadDir(rootPath)
	if err != nil {
		log.Printf("ERROR: ReadDir failed: %v", err)
		return err
	}
	log.Printf("DEBUG: Found %d entries in %s", len(entries), rootPath)

	// Track found paths to handle deletions
	foundPaths := make(map[string]bool)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Check if it's a hidden folder
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		animePath := filepath.Join(rootPath, entry.Name())

		fileCount, totalSize := s.countVideos(animePath)
		if fileCount > 0 {
			// Only mark as found if valid videos exist
			foundPaths[animePath] = true

			// Check if exists
			var anime model.LocalAnime
			if err := db.DB.Where("path = ?", animePath).First(&anime).Error; err == nil {
				// Update stats
				anime.FileCount = fileCount
				anime.TotalSize = totalSize
				// Only update DirectoryID if changed (unlikely here but safe)
				anime.DirectoryID = dirID

				// Retry Metadata if missing (Auto-Repair)
				// DEPRECATED: Do not enrich synchronously during scan.
				// if anime.MetadataID == nil || *anime.MetadataID == 0 || anime.Summary == "" {
				// 	s.EnrichAnimeMetadata(&anime)
				// }

				db.DB.Save(&anime)
			} else {
				// Create new
				anime = model.LocalAnime{
					DirectoryID: dirID,
					Title:       entry.Name(),
					Path:        animePath,
					FileCount:   fileCount,
					TotalSize:   totalSize,
				}

				// Initial Metadata Fetch
				// DEPRECATED: Do not enrich synchronously during scan.
				// s.EnrichAnimeMetadata(&anime)

				if err := db.DB.Create(&anime).Error; err != nil {
					log.Printf("ERROR: Failed to create anime record: %v", err)
				}
			}
		}
	}

	// Remove stale entries for this directory
	// We need to fetch all existing for this dir first?
	// Or just delete where directory_id = ? AND path NOT IN ?
	// Careful with large IN clauses, but usually local anime count is manageable (<1000 per dir)
	if len(foundPaths) > 0 {
		var paths []string
		for p := range foundPaths {
			paths = append(paths, p)
		}
		db.DB.Where("directory_id = ? AND path NOT IN ?", dirID, paths).Delete(&model.LocalAnime{})
	} else {
		// No valid folders found, wipe all for this dir
		db.DB.Where("directory_id = ?", dirID).Delete(&model.LocalAnime{})
	}

	return nil
}

// EnrichMissingMetadata finds local anime without metadata and enriches them
func (s *LocalAnimeService) EnrichMissingMetadata() {
	log.Println("Enrich: Starting background enrichment for missing metadata...")
	var list []model.LocalAnime
	// Find items with missing metadata OR missing summary (indicates incomplete fetch)
	// We join with Metadata to be precise, or just check fields on LocalAnime if valid.
	// LocalAnime has Summary copied from Metadata.
	// But check MetadataID is safer.
	db.DB.Preload("Metadata").Where("metadata_id IS NULL OR metadata_id = 0").Find(&list)

	// Also check items where valid Metadata exists but Summary is empty (incomplete scrape)
	var incomplete []model.LocalAnime
	db.DB.Preload("Metadata").Where("metadata_id > 0 AND summary = ''").Find(&incomplete)
	list = append(list, incomplete...)

	if len(list) == 0 {
		log.Println("Enrich: No items need enrichment.")
		return
	}

	log.Printf("Enrich: Found %d items needing metadata.", len(list))

	count := 0
	for _, anime := range list {
		// Re-check existence to avoid race
		var fresh model.LocalAnime
		if err := db.DB.First(&fresh, anime.ID).Error; err != nil {
			continue
		}

		s.EnrichAnimeMetadata(&fresh) // This now handles proxy correctly too
		if err := db.DB.Save(&fresh).Error; err == nil {
			count++
			// Sync back to db immediately handled by EnrichAnimeMetadata's SyncMetadataToModels
		}

		// Rate limit
		time.Sleep(500 * time.Millisecond)
	}
	log.Printf("Enrich: Completed. Enriched %d items.", count)
}

// MatchSeries Manually match a series to a specific Bangumi ID
func (s *LocalAnimeService) MatchSeries(animeID uint, bangumiID int) error {
	var anime model.LocalAnime
	if err := db.DB.First(&anime, animeID).Error; err != nil {
		return err
	}

	// Fetch fresh metadata for this ID or create new
	var meta model.AnimeMetadata
	// Check if metadata already exists for this BangumiID
	if err := db.DB.Where("bangumi_id = ?", bangumiID).First(&meta).Error; err == nil {
		// Use existing metadata
	} else {
		// New metadata entry
		meta = model.AnimeMetadata{
			BangumiID: bangumiID,
		}
	}

	// Verify ID is valid by fetching from Bangumi
	bgmClient := bangumi.NewClient("", "", "")
	// Apply Proxy
	var bgmProxyConfig model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyProxyBangumi).First(&bgmProxyConfig).Error; err == nil && bgmProxyConfig.Value == "true" {
		var p model.GlobalConfig
		if err := db.DB.Where("key = ?", model.ConfigKeyProxyURL).First(&p).Error; err == nil && p.Value != "" {
			bgmClient.SetProxy(p.Value)
		}
	}

	subject, err := bgmClient.GetSubject(bangumiID)
	if err != nil {
		return fmt.Errorf("failed to verify Bangumi ID: %v", err)
	}
	if subject == nil {
		return fmt.Errorf("bangumi ID %d not found", bangumiID)
	}

	// Update metadata object with fetched data
	meta.Title = subject.NameCN
	if meta.Title == "" {
		meta.Title = subject.Name
	}
	meta.BangumiTitle = meta.Title
	meta.BangumiImage = subject.Images.Large
	meta.BangumiSummary = subject.Summary
	meta.TitleCN = subject.NameCN
	meta.TitleJP = subject.Name
	meta.AirDate = subject.Date
	meta.Image = meta.BangumiImage // Set active image
	meta.Summary = meta.BangumiSummary

	// Save Metadata
	if meta.ID == 0 {
		if err := db.DB.Create(&meta).Error; err != nil {
			return err
		}
	} else {
		if err := db.DB.Save(&meta).Error; err != nil {
			return err
		}
	}

	// Link Anime to Metadata
	anime.MetadataID = &meta.ID
	anime.Metadata = &meta
	db.DB.Save(&anime)

	// Fetch Image Cache
	meta.BangumiImageRaw = s.fetchAndCacheImage(meta.BangumiImage)
	db.DB.Save(&meta)

	// Also trigger standard enrich to fill TMDB/AniList if possible
	// s.EnrichMetadata(&meta, meta.Title) // Optional, might overwrite manual choice?
	// Let's just update active fields.

	return nil
}

// EnrichAnimeMetadata tries to find Bangumi ID and valid Title/Image from BOTH sources
func (s *LocalAnimeService) EnrichAnimeMetadata(anime *model.LocalAnime) {
	// 1. Ensure Metadata record exists
	if anime.MetadataID == nil || *anime.MetadataID == 0 {
		// Try to find by title if we had one? Or just create new.
		// For new anime, we just create. For existing, we might find by ID below.
		m := &model.AnimeMetadata{}
		anime.Metadata = m
	} else if anime.Metadata == nil {
		var m model.AnimeMetadata
		if err := db.DB.Preload("Metadata").First(&anime, anime.ID).Error; err == nil && anime.Metadata != nil {
			// Already loaded
		} else {
			db.DB.First(&m, *anime.MetadataID)
			anime.Metadata = &m
		}
	}

	queryTitle := CleanTitle(anime.Title)
	s.EnrichMetadata(anime.Metadata, queryTitle)

	// Sync metadata to ALL models (this one and others sharing it)
	s.SyncMetadataToModels(anime.Metadata)
}

// RefreshAllMetadata updates metadata records. If force is false, it skips already scraped items.
func (s *LocalAnimeService) RefreshAllMetadata(force bool) int {
	log.Printf("Refresh: Starting %s metadata refresh...", func() string {
		if force {
			return "FULL FORCE"
		}
		return "incremental"
	}())
	var allList []model.AnimeMetadata
	if err := db.DB.Find(&allList).Error; err != nil {
		log.Printf("Refresh: Failed to fetch metadata list: %v", err)
		return 0
	}

	// Filter for items that are not scraped yet if not forced
	var list []model.AnimeMetadata
	if force {
		list = allList
	} else {
		for _, m := range allList {
			// If it has a summary and at least one source ID, we consider it "scraped"
			if m.Summary != "" && (m.BangumiID != 0 || m.TMDBID != 0 || m.AniListID != 0) {
				continue
			}
			list = append(list, m)
		}
	}

	total := len(list)

	// Use a lock for status updates
	var statusMu sync.Mutex

	statusMu.Lock()
	GlobalRefreshStatus.Total = total
	GlobalRefreshStatus.Current = 0
	GlobalRefreshStatus.IsRunning = true
	GlobalRefreshStatus.LastResult = ""
	statusMu.Unlock()

	if total == 0 {
		statusMu.Lock()
		GlobalRefreshStatus.IsRunning = false
		GlobalRefreshStatus.LastResult = "全库元数据已是最新状态，无需刷新"
		statusMu.Unlock()
		return 0
	}

	updatedCount := 0
	var updateMu sync.Mutex

	// Worker Pool Settings
	maxWorkers := 5
	guard := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for i, m := range list {
		guard <- struct{}{} // Block if filled
		wg.Add(1)

		go func(idx int, meta model.AnimeMetadata) {
			defer wg.Done()
			defer func() { <-guard }()

			// Update Status (Non-blocking visual update)
			statusMu.Lock()
			GlobalRefreshStatus.Current = idx + 1
			titleDisplay := meta.Title
			if meta.TitleCN != "" {
				titleDisplay = meta.TitleCN
			}
			GlobalRefreshStatus.CurrentTitle = titleDisplay
			statusMu.Unlock()

			// Re-fetch fresh copy to ensure no race or stale data
			var freshM model.AnimeMetadata
			if err := db.DB.First(&freshM, meta.ID).Error; err == nil {
				// Use the existing Title as fallback query if IDs are missing
				queryTitle := freshM.Title
				if freshM.TitleCN != "" {
					queryTitle = freshM.TitleCN
				}
				s.EnrichMetadata(&freshM, queryTitle)

				updateMu.Lock()
				updatedCount++
				updateMu.Unlock()

				log.Printf("Refresh: Updated metadata for '%s' (%d/%d)", freshM.Title, idx+1, total)
			}
			// Small per-worker delay to be nice to APIs, but parallel execution speeds it up
			time.Sleep(500 * time.Millisecond)
		}(i, m)
	}

	wg.Wait()

	statusMu.Lock()
	GlobalRefreshStatus.IsRunning = false
	GlobalRefreshStatus.CurrentTitle = ""
	GlobalRefreshStatus.LastResult = fmt.Sprintf("后台刷新完成，共更新 %d 条元数据", updatedCount)
	statusMu.Unlock()

	log.Printf("Refresh: Metadata refresh completed. Updated %d items.", updatedCount)
	return updatedCount
}

// RefreshSingleMetadata forces a refresh of a single metadata record
func (s *LocalAnimeService) RefreshSingleMetadata(id uint) error {
	var m model.AnimeMetadata
	if err := db.DB.First(&m, id).Error; err != nil {
		return err
	}

	queryTitle := m.Title
	if m.TitleCN != "" {
		queryTitle = m.TitleCN
	}

	s.EnrichMetadata(&m, queryTitle)
	return nil
}

// EnrichMetadata is the CORE logic shared by LocalAnime and Subscription
func (s *LocalAnimeService) EnrichMetadata(m *model.AnimeMetadata, queryTitle string) {
	bgmClient, tmdbClient, anilistClient := s.initClients()

	rawQueryTitle := queryTitle // Keep original for fallbacks
	if queryTitle == "" {
		queryTitle = m.Title // Fallback to existing title
	}
	log.Printf("DEBUG: EnrichMetadata for '%s' (ID: %d)", queryTitle, m.ID)

	// 2. Fetch Bangumi Data (Priority: ID > Search Candidates)
	s.enrichBangumi(m, bgmClient, queryTitle)

	// 3. Parallel Fetch for TMDB & AniList
	s.enrichParallel(m, tmdbClient, anilistClient, queryTitle)

	// 4. Bangumi Retry (If initial failed but others succeeded)
	if m.BangumiID == 0 && (m.TMDBID != 0 || m.AniListID != 0) {
		s.retryBangumi(m, bgmClient, queryTitle)
	}

	// 5. Save the enriched metadata
	s.saveAndConsolidate(m)

	// 6. Set Active Fields (Bangumi > TMDB > AniList)
	s.setActiveFields(m, rawQueryTitle)

	db.DB.Save(m)

	// Trigger sync to linked records
	s.SyncMetadataToModels(m)
}

func (s *LocalAnimeService) initClients() (*bangumi.Client, *tmdb.Client, *anilist.Client) {
	// 1. Prepare Clients
	bgmClient := bangumi.NewClient("", "", "")
	// Apply Proxy to Bangumi
	var bgmProxyConfig model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyProxyBangumi).First(&bgmProxyConfig).Error; err == nil && bgmProxyConfig.Value == model.ConfigValueTrue {
		var p model.GlobalConfig
		if err := db.DB.Where("key = ?", model.ConfigKeyProxyURL).First(&p).Error; err == nil && p.Value != "" {
			bgmClient.SetProxy(p.Value)
		}
	}

	// TMDB Client
	var tmdbTokenConfig model.GlobalConfig
	var tmdbClient *tmdb.Client
	if err := db.DB.Where("key = ?", model.ConfigKeyTMDBToken).First(&tmdbTokenConfig).Error; err == nil && tmdbTokenConfig.Value != "" {
		var proxyConfig model.GlobalConfig
		proxyURL := ""
		if err := db.DB.Where("key = ?", model.ConfigKeyProxyTMDB).First(&proxyConfig).Error; err == nil && proxyConfig.Value == model.ConfigValueTrue {
			var p model.GlobalConfig
			if err := db.DB.Where("key = ?", model.ConfigKeyProxyURL).First(&p).Error; err == nil {
				proxyURL = p.Value
			}
		}
		tmdbClient = tmdb.NewClient(tmdbTokenConfig.Value, proxyURL)
	}

	// AniList Client
	var anilistTokenConfig model.GlobalConfig
	var anilistClient *anilist.Client
	if err := db.DB.Where("key = ?", model.ConfigKeyAniListToken).First(&anilistTokenConfig).Error; err == nil && anilistTokenConfig.Value != "" {
		var proxyConfig model.GlobalConfig
		proxyURL := ""
		if err := db.DB.Where("key = ?", model.ConfigKeyProxyAniList).First(&proxyConfig).Error; err == nil && proxyConfig.Value == model.ConfigValueTrue {
			var p model.GlobalConfig
			if err := db.DB.Where("key = ?", model.ConfigKeyProxyURL).First(&p).Error; err == nil {
				proxyURL = p.Value
			}
		}
		anilistClient = anilist.NewClient(anilistTokenConfig.Value, proxyURL)
	}

	return bgmClient, tmdbClient, anilistClient
}

func (s *LocalAnimeService) enrichBangumi(m *model.AnimeMetadata, bgmClient *bangumi.Client, queryTitle string) {
	var bgmSubject *bangumi.Subject
	var err error

	if m.BangumiID != 0 {
		// ID exists, fetch directly with retry
		bgmSubject, err = performWithRetry(func() (*bangumi.Subject, error) {
			return bgmClient.GetSubject(m.BangumiID)
		})
		if err != nil {
			log.Printf("DEBUG: Bangumi ID %d fetch failed after retries, will try search: %v", m.BangumiID, err)
		}
	}

	if bgmSubject == nil {
		// Init candidates with current state
		initialCandidates := getCandidateTitles(m, queryTitle)
		// Try search with all candidates
		for _, t := range initialCandidates {
			if t == "" {
				continue
			}
			// Search with retry
			res, err := performWithRetry(func() (*bangumi.SearchResult, error) {
				return bgmClient.SearchSubject(t)
			})
			if err == nil && res != nil {
				log.Printf("DEBUG: Match found on Bangumi using title '%s' -> ID: %d", t, res.ID)
				m.BangumiID = res.ID
				// Get details with retry
				bgmSubject, _ = performWithRetry(func() (*bangumi.Subject, error) {
					return bgmClient.GetSubject(res.ID)
				})
				break // Stop on first match
			}
		}
	}

	if bgmSubject != nil {
		s.applyBangumiSubject(m, bgmSubject)
	}
}

func (s *LocalAnimeService) applyBangumiSubject(m *model.AnimeMetadata, bgmSubject *bangumi.Subject) {
	m.BangumiID = bgmSubject.ID
	m.BangumiImage = bgmSubject.Images.Large
	m.BangumiSummary = bgmSubject.Summary
	m.BangumiRating = bgmSubject.Rating.Score
	if m.AirDate == "" {
		m.AirDate = bgmSubject.Date
	}
	// Populate titles if missing
	if m.TitleJP == "" {
		m.TitleJP = bgmSubject.Name
	}
	if m.TitleCN == "" {
		m.TitleCN = bgmSubject.NameCN
	}
	// Default fields priority
	if bgmSubject.NameCN != "" {
		m.BangumiTitle = bgmSubject.NameCN
	} else {
		m.BangumiTitle = bgmSubject.Name
	}
	// Cache Image
	m.BangumiImageRaw = s.fetchAndCacheImage(m.BangumiImage)
}

func (s *LocalAnimeService) enrichParallel(m *model.AnimeMetadata, tmdbClient *tmdb.Client, anilistClient *anilist.Client, queryTitle string) {
	// Re-generate candidates including potential new Bangumi titles
	finalCandidates := getCandidateTitles(m, queryTitle)

	var wg sync.WaitGroup
	var mu sync.Mutex // Protect m writes

	wg.Add(2)

	// --- 3. TMDB Task ---
	go func() {
		defer wg.Done()
		if tmdbClient == nil {
			return
		}

		s.processTMDB(m, tmdbClient, finalCandidates, &mu)
	}()

	// --- 4. AniList Task ---
	go func() {
		defer wg.Done()
		if anilistClient == nil {
			return
		}

		s.processAniList(m, anilistClient, finalCandidates, &mu)
	}()

	wg.Wait()
}

func (s *LocalAnimeService) processTMDB(m *model.AnimeMetadata, client *tmdb.Client, candidates []string, mu *sync.Mutex) {
	var tmdbShow *tmdb.TVShow
	var err error

	// Copy ID to avoid race on read (though integer read is usually fine, being safe)
	mu.Lock()
	currentTMDBID := m.TMDBID
	mu.Unlock()

	if currentTMDBID != 0 {
		tmdbShow, err = performWithRetry(func() (*tmdb.TVShow, error) {
			return client.GetTVDetails(currentTMDBID)
		})
		if err != nil {
			log.Printf("DEBUG: TMDB ID %d fetch failed: %v", currentTMDBID, err)
			tmdbShow = nil
		}
	}

	if tmdbShow == nil {
		for _, t := range candidates {
			if t == "" {
				continue
			}
			show, err := performWithRetry(func() (*tmdb.TVShow, error) {
				return client.SearchTV(t)
			})
			if err == nil && show != nil {
				log.Printf("DEBUG: Match found on TMDB using title '%s' -> ID: %d", t, show.ID)
				tmdbShow = show
				break
			}
		}
	}

	if tmdbShow != nil {
		imgRaw := s.fetchAndCacheImage(tmdbShow.PosterPath)

		mu.Lock()
		m.TMDBID = tmdbShow.ID
		m.TMDBTitle = tmdbShow.Name
		m.TMDBImage = tmdbShow.PosterPath
		m.TMDBSummary = tmdbShow.Overview
		m.TMDBRating = tmdbShow.VoteAverage
		if m.AirDate == "" {
			m.AirDate = tmdbShow.FirstAirDate
		}
		if m.TitleCN == "" {
			m.TitleCN = tmdbShow.Name
		}
		if m.TitleJP == "" {
			m.TitleJP = tmdbShow.OriginalName
		}
		m.TMDBImageRaw = imgRaw
		mu.Unlock()
	}
}

func (s *LocalAnimeService) processAniList(m *model.AnimeMetadata, client *anilist.Client, candidates []string, mu *sync.Mutex) {
	var alMedia *anilist.Media
	var err error

	mu.Lock()
	currentAniListID := m.AniListID
	mu.Unlock()

	if currentAniListID != 0 {
		alMedia, err = performWithRetry(func() (*anilist.Media, error) {
			return client.GetAnimeDetails(currentAniListID)
		})
		if err != nil {
			log.Printf("DEBUG: AniList ID %d fetch failed: %v", currentAniListID, err)
			alMedia = nil
		}
	}

	if alMedia == nil {
		for _, t := range candidates {
			if t == "" {
				continue
			}
			media, err := performWithRetry(func() (*anilist.Media, error) {
				return client.SearchAnime(t)
			})
			if err == nil && media != nil {
				log.Printf("DEBUG: Match found on AniList using title '%s' -> ID: %d", t, media.ID)
				alMedia = media
				break
			}
		}
	}

	if alMedia != nil {
		imgRaw := s.fetchAndCacheImage(alMedia.CoverImage.ExtraLarge)

		mu.Lock()
		m.AniListID = alMedia.ID
		m.AniListTitle = alMedia.Title.Romaji
		m.AniListImage = alMedia.CoverImage.ExtraLarge
		m.AniListSummary = alMedia.Description
		m.AniListRating = float64(alMedia.AverageScore) / 10.0

		if m.TitleEN == "" {
			m.TitleEN = alMedia.Title.English
		}
		if m.TitleJP == "" {
			m.TitleJP = alMedia.Title.Native
		}
		m.AniListImageRaw = imgRaw
		mu.Unlock()
	}
}

func (s *LocalAnimeService) retryBangumi(m *model.AnimeMetadata, bgmClient *bangumi.Client, queryTitle string) {
	log.Printf("DEBUG: Bangumi Retry triggered for '%s' (TMDB:%d, AL:%d)", queryTitle, m.TMDBID, m.AniListID)

	retryCandidates := getCandidateTitles(m, queryTitle)
	for _, t := range retryCandidates {
		if t == "" {
			continue
		}
		res, err := performWithRetry(func() (*bangumi.SearchResult, error) {
			return bgmClient.SearchSubject(t)
		})
		if err == nil && res != nil {
			log.Printf("DEBUG: RETRY MATCH on Bangumi using title '%s' -> ID: %d", t, res.ID)
			bgmSubject, _ := performWithRetry(func() (*bangumi.Subject, error) {
				return bgmClient.GetSubject(res.ID)
			})
			if bgmSubject != nil {
				s.applyBangumiSubject(m, bgmSubject)
			}
			break
		}
	}
}

func (s *LocalAnimeService) saveAndConsolidate(m *model.AnimeMetadata) {
	if m.ID == 0 {
		var existing model.AnimeMetadata
		found := false

		// Check by BangumiID first (Priority)
		if m.BangumiID != 0 {
			if err := db.DB.Where("bangumi_id = ?", m.BangumiID).First(&existing).Error; err == nil {
				found = true
			}
		}

		// Fallback checks (TMDB/AniList) - only if not found by Bangumi
		if !found && m.TMDBID != 0 {
			if err := db.DB.Where("tmdb_id = ?", m.TMDBID).First(&existing).Error; err == nil {
				found = true
			}
		}
		if !found && m.AniListID != 0 {
			if err := db.DB.Where("anilist_id = ?", m.AniListID).First(&existing).Error; err == nil {
				found = true
			}
		}

		if found {
			// Found existing! Merge new IDs into it.
			if m.BangumiID != 0 {
				existing.BangumiID = m.BangumiID
			}
			if m.TMDBID != 0 {
				existing.TMDBID = m.TMDBID
			}
			if m.AniListID != 0 {
				existing.AniListID = m.AniListID
			}

			// Point current object to the existing ID to update it
			*m = existing
		} else {
			// Create new
			if err := db.DB.Create(m).Error; err != nil {
				// Handle race condition: Unique constraint failed?
				if strings.Contains(err.Error(), "UNIQUE constraint failed") {
					// Rare race: Try to fetch again
					if m.BangumiID != 0 {
						if err := db.DB.Where("bangumi_id = ?", m.BangumiID).First(&existing).Error; err == nil {
							*m = existing
						}
					}
				} else {
					log.Printf("ERROR: Failed to create metadata: %v", err)
				}
			}
		}
	} else {
		db.DB.Save(m)
	}
}

func (s *LocalAnimeService) setActiveFields(m *model.AnimeMetadata, rawQueryTitle string) {
	if m.BangumiID != 0 {
		m.Title = m.BangumiTitle
		m.Image = m.BangumiImage
		m.Summary = m.BangumiSummary
		if m.Summary == "" && m.TMDBSummary != "" {
			m.Summary = m.TMDBSummary
		}
	} else if m.TMDBID != 0 {
		m.Title = m.TMDBTitle
		m.Image = m.TMDBImage
		m.Summary = m.TMDBSummary
	} else if m.AniListID != 0 {
		m.Title = m.AniListTitle
		m.Image = m.AniListImage
		m.Summary = m.AniListSummary
	}

	// Ensure Image Points to Local API
	if m.ID != 0 {
		m.Image = fmt.Sprintf("/api/posters/%d", m.ID)
	}
	// Fallback Title
	if m.Title == "" {
		m.Title = rawQueryTitle
	}
}

func getCandidateTitles(m *model.AnimeMetadata, query string) []string {
	seen := make(map[string]bool)
	var candidates []string

	add := func(t string) {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			seen[t] = true
			candidates = append(candidates, t)
		}
	}

	// Priority: CN -> JP -> EN -> Query -> Current Title
	add(m.TitleCN)
	add(m.TitleJP)
	add(m.TitleEN)
	add(query)
	add(m.Title)

	return candidates
}

func (s *LocalAnimeService) fetchAndCacheImage(url string) []byte {
	if url == "" {
		return nil
	}
	log.Printf("DEBUG: Downloading image: %s", url)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("ERROR: Failed to fetch image %s: %v", url, err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("ERROR: Non-OK status while fetching image %s: %d", url, resp.StatusCode)
		return nil
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("ERROR: Failed to read image data from %s: %v", url, err)
		return nil
	}
	return data
}

// CleanTitle removes common tags like [Group] or [1080p] to get a search-friendly title
func CleanTitle(raw string) string {
	// 1. Remove leading [...]
	// 2. Remove trailing [...] or (...)
	// 3. Remove date patterns like (2024), [2024], 2024

	// Simple heuristic: Take the "middle" part if wrapped in tags, or just the string
	// [Group] Title [Res] -> Title

	s := raw
	// Remove leading [...]
	s = regexp.MustCompile(`^\[.*?\]\s*`).ReplaceAllString(s, "")
	// Remove trailing [...]
	s = regexp.MustCompile(`\s*\[.*?\]$`).ReplaceAllString(s, "")
	// Remove trailing (...)
	s = regexp.MustCompile(`\s*\(.*?\)$`).ReplaceAllString(s, "")

	// Remove S1, S01, Season 1 suffix (case insensitive)
	// Matches: space + S + digits OR space + Season + space + digits at end of string
	s = regexp.MustCompile(`(?i)\s+S\d+$`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`(?i)\s+Season\s*\d+$`).ReplaceAllString(s, "")

	// Split by '/' and take the first part
	// This handles dual language titles like "CN / EN"
	if idx := strings.Index(s, "/"); idx != -1 {
		s = s[:idx]
	}

	return strings.TrimSpace(s)
}

func (s *LocalAnimeService) countVideos(path string) (int, int64) {
	count := 0
	var size int64 = 0

	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			log.Printf("ERROR: WalkDir error at %s: %v", p, err)
			return nil
		}
		if !d.IsDir() {
			// Skip hidden files/system files
			if strings.HasPrefix(d.Name(), ".") {
				return nil
			}

			ext := strings.ToLower(filepath.Ext(d.Name()))
			if IsVideoExt(ext) {
				count++
				info, _ := d.Info()
				if info != nil {
					size += info.Size()
				}
			} else {
				// Only log first few skipped files to avoid spam
				if count == 0 {
					log.Printf("DEBUG: Skipped non-video file: %s", d.Name())
				}
			}
		}
		return nil
	})

	return count, size
}

func IsVideoExt(ext string) bool {
	switch ext {
	// Added .!qB, .bc! for partial downloads
	case ".mp4", ".mkv", ".avi", ".mov", ".flv", ".wmv", ".ts", ".rmvb", ".webm", ".m2ts", ".!qb", ".bc!":
		return true
	}
	return false
}

// EnrichSubscriptionMetadata populates TMDB, Bangumi and AniList metadata for a subscription
func (s *LocalAnimeService) EnrichSubscriptionMetadata(sub *model.Subscription) {
	// 1. Ensure Metadata record exists
	if sub.MetadataID == nil || *sub.MetadataID == 0 {
		m := &model.AnimeMetadata{}
		sub.Metadata = m
	} else if sub.Metadata == nil {
		var m model.AnimeMetadata
		if err := db.DB.Preload("Metadata").First(sub, sub.ID).Error; err == nil && sub.Metadata != nil {
			// Already loaded
		} else {
			db.DB.First(&m, *sub.MetadataID)
			sub.Metadata = &m
		}
	}

	queryTitle := CleanTitle(sub.Title)
	s.EnrichMetadata(sub.Metadata, queryTitle)

	// Sync metadata to ALL models
	s.SyncMetadataToModels(sub.Metadata)
}

// SyncMetadataToModels propagates metadata fields to all linked Subscription and LocalAnime records
func (s *LocalAnimeService) SyncMetadataToModels(m *model.AnimeMetadata) {
	if m == nil || m.ID == 0 {
		return
	}

	// 1. Update Subscriptions
	db.DB.Model(&model.Subscription{}).Where("metadata_id = ?", m.ID).Updates(map[string]interface{}{
		"image": m.Image,
		"title": m.Title, // Optional: should we sync Title? Usually yes if they match.
	})

	// 2. Update LocalAnime
	db.DB.Model(&model.LocalAnime{}).Where("metadata_id = ?", m.ID).Updates(map[string]interface{}{
		"image":    m.Image,
		"title":    m.Title,
		"summary":  m.Summary,
		"air_date": m.AirDate,
	})
}

// StartMetadataMigration background task to cache images for existing records
func (s *LocalAnimeService) StartMetadataMigration() {
	go func() {
		// Wait a bit for server to fully start
		time.Sleep(5 * time.Second)
		log.Println("Migration: Starting background metadata image migration...")
		var list []model.AnimeMetadata
		// Find records that have an image URL but no binary data cached.
		// Use empty check for blobs as they might be empty bytes rather than NULL in some cases.
		db.DB.Where("(bangumi_image != '' AND (bangumi_image_raw IS NULL OR bangumi_image_raw = '')) OR " +
			"(tmdb_image != '' AND (tmdb_image_raw IS NULL OR tmdb_image_raw = '')) OR " +
			"(ani_list_image != '' AND (ani_list_image_raw IS NULL OR ani_list_image_raw = ''))").Find(&list)

		log.Printf("Migration: Found %d records needing image caching", len(list))

		for _, m := range list {
			updated := false
			if m.BangumiImage != "" && len(m.BangumiImageRaw) == 0 {
				m.BangumiImageRaw = s.fetchAndCacheImage(m.BangumiImage)
				updated = true
			}
			if m.TMDBImage != "" && len(m.TMDBImageRaw) == 0 {
				m.TMDBImageRaw = s.fetchAndCacheImage(m.TMDBImage)
				updated = true
			}
			if m.AniListImage != "" && len(m.AniListImageRaw) == 0 {
				m.AniListImageRaw = s.fetchAndCacheImage(m.AniListImage)
				updated = true
			}

			if updated {
				// Ensure active image points to local API
				m.Image = fmt.Sprintf("/api/posters/%d", m.ID)
				if err := db.DB.Save(&m).Error; err != nil {
					log.Printf("Migration: Failed to save metadata %d: %v", m.ID, err)
				} else {
					// Trigger sync to all models (Subscription/LocalAnime)
					s.SyncMetadataToModels(&m)
					log.Printf("Migration: Successfully cached images for Metadata ID %d (%s)", m.ID, m.Title)
				}
			}
			// Sleep a bit to avoid hitting APIs too hard
			time.Sleep(1 * time.Second)
		}
		log.Println("Migration: Background image migration completed.")
	}()
}

// Helper: Generic Retry with exponential backoff
// Attempts: 3
// Backoff: 500ms, 1s
func performWithRetry[T any](op func() (T, error)) (T, error) {
	var result T
	var err error
	for i := 0; i < 3; i++ {
		if i > 0 {
			time.Sleep(time.Duration(1<<(i-1)) * 500 * time.Millisecond)
		}
		result, err = op()
		if err == nil {
			return result, nil
		}
		// Optional: Log retry warning for debugging
		// log.Printf("DEBUG: API Call retry %d failed: %v", i+1, err)
	}
	return result, err
}
