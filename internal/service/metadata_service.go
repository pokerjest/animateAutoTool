package service

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/anilist"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/tmdb"
)

type RefreshStatus struct {
	Total        int    `json:"total"`
	Current      int    `json:"current"`
	CurrentTitle string `json:"current_title"`
	IsRunning    bool   `json:"is_running"`
	LastResult   string `json:"last_result"`
}

var GlobalRefreshStatus = RefreshStatus{}

type MetadataService struct{}

func NewMetadataService() *MetadataService {
	return &MetadataService{}
}

// FetchMetadata performs parallel search across all sources and returns consolidated metadata
func (s *MetadataService) FetchMetadata(query string) (*model.AnimeMetadata, error) {
	m := &model.AnimeMetadata{}
	s.EnrichMetadata(m, query)
	return m, nil
}

// EnrichAnime updates an Anime record with metadata using the full parallel logic
func (s *MetadataService) EnrichAnime(anime *model.LocalAnime) error {
	if anime == nil {
		return fmt.Errorf("anime is nil")
	}

	log.Printf("MetadataService: Enriching '%s' (Path: %s)", anime.Title, anime.Path)

	// 1. Ensure Metadata record exists or link to existing
	if anime.MetadataID == nil || *anime.MetadataID == 0 {
		queryTitle := parser.CleanTitle(anime.Title)
		log.Printf("Enrich: Attempting to link '%s' to existing metadata...", queryTitle)

		var existing model.AnimeMetadata
		err := db.DB.Where("title = ? OR title_cn = ? OR title_jp = ? OR title_en = ?",
			queryTitle, queryTitle, queryTitle, queryTitle).First(&existing).Error

		if err == nil && existing.ID != 0 {
			log.Printf("Enrich: Found existing metadata link for '%s' -> ID %d", anime.Title, existing.ID)
			anime.Metadata = &existing
			anime.MetadataID = &existing.ID
			db.DB.Save(anime)
		} else {
			anime.Metadata = &model.AnimeMetadata{Title: queryTitle}
		}
	}

	// 2. Full Enrichment
	s.EnrichMetadata(anime.Metadata, anime.Title)

	// 3. Link and Save
	if anime.Metadata != nil && anime.Metadata.ID != 0 {
		anime.MetadataID = &anime.Metadata.ID
	}

	// Sync to anime model
	anime.Image = anime.Metadata.Image
	anime.Summary = anime.Metadata.Summary

	db.DB.Save(anime)

	// 4. Align Episodes and Sync Metadata (Phase 4)
	if anime.Metadata.TMDBID != 0 {
		s.SyncEpisodesWithTMDB(anime)
		s.AlignEpisodesWithTMDB(anime)
	}

	// 5. NFO Generation (Phase 4)
	nfoGen := NewNFOGeneratorService()
	_ = nfoGen.SaveLocalImages(anime)
	_ = nfoGen.GenerateTVShowNFO(anime)

	return nil
}

// AlignEpisodesWithTMDB corrects local episode Season/Episode numbers based on TMDB logic
func (s *MetadataService) AlignEpisodesWithTMDB(anime *model.LocalAnime) {
	if anime.Metadata == nil || anime.Metadata.TMDBID == 0 {
		return
	}

	_, tmdbClient, _ := s.initClients()
	if tmdbClient == nil {
		return
	}

	show, err := performWithRetry(func() (*tmdb.TVShow, error) {
		return tmdbClient.GetTVDetails(anime.Metadata.TMDBID)
	})
	if err != nil || show == nil {
		return
	}

	type SeasonRange struct {
		SeasonNum int
		Start     int
		End       int
	}
	var ranges []SeasonRange
	currentTotal := 0
	for _, season := range show.Seasons {
		if season.SeasonNumber == 0 {
			continue
		}
		start := currentTotal + 1
		end := currentTotal + season.EpisodeCount
		ranges = append(ranges, SeasonRange{
			SeasonNum: season.SeasonNumber,
			Start:     start,
			End:       end,
		})
		currentTotal = end
	}

	var episodes []model.LocalEpisode
	db.DB.Where("local_anime_id = ?", anime.ID).Find(&episodes)

	for i := range episodes {
		ep := &episodes[i]

		// Decide if we should try aligning this episode
		// 1. It's marked as Season 1 (common for absolute numbering)
		// 2. Its episode number is higher than what TMDB says for its current season (likely absolute)
		// 3. Or its current season number doesn't exist on TMDB
		shouldAlign := false
		if ep.SeasonNum <= 1 {
			shouldAlign = true
		} else {
			// Check if season exists and has enough episodes
			exists := false
			maxEp := 0
			for _, s := range show.Seasons {
				if s.SeasonNumber == ep.SeasonNum {
					exists = true
					maxEp = s.EpisodeCount
					break
				}
			}
			if !exists || ep.EpisodeNum > maxEp {
				shouldAlign = true
			}
		}

		if shouldAlign {
			targetAbs := ep.EpisodeNum
			// If it's already in a high season but we think it's absolute,
			// we need the REAL absolute number.
			// But usually files like "S3E40" have 40 as the absolute number already.
			// If it was "S3E03" but absolute ep is 40, our parser would have set Ep=3, S=3.
			// In that case, we can't easily recover the absolute number without more heuristics.
			// However, most "missing cover" cases are from absolute numbering files (01-24.mkv).

			for _, r := range ranges {
				if targetAbs >= r.Start && targetAbs <= r.End {
					if r.SeasonNum != ep.SeasonNum || (r.SeasonNum == ep.SeasonNum && ep.EpisodeNum != (targetAbs-r.Start+1)) {
						newEpNum := targetAbs - r.Start + 1
						log.Printf("Align: %s - S%dE%d -> S%dE%d (Abs %d)", anime.Title, ep.SeasonNum, ep.EpisodeNum, r.SeasonNum, newEpNum, targetAbs)
						ep.SeasonNum = r.SeasonNum
						ep.EpisodeNum = newEpNum
						db.DB.Save(ep)
					}
					break
				}
			}
		}
	}

	// Re-sync metadata after alignment
	s.SyncEpisodesWithTMDB(anime)
}

// SyncEpisodesWithTMDB fetches episode details (images/summaries) from TMDB
func (s *MetadataService) SyncEpisodesWithTMDB(anime *model.LocalAnime) {
	if anime.ID == 0 || anime.Metadata == nil || anime.Metadata.TMDBID == 0 {
		return
	}

	log.Printf("MetadataService: Syncing episodes with TMDB for %s (TMDB ID: %d)", anime.Title, anime.Metadata.TMDBID)

	_, tmdbClient, _ := s.initClients()
	if tmdbClient == nil {
		log.Printf("MetadataService: Failed to init TMDB client for %s", anime.Title)
		return
	}

	// Group episodes by season to minimize API calls
	var localEps []model.LocalEpisode
	db.DB.Where("local_anime_id = ?", anime.ID).Find(&localEps)
	if len(localEps) == 0 {
		log.Printf("MetadataService: No local episodes found for %s", anime.Title)
		return
	}

	seasons := make(map[int]bool)
	for _, ep := range localEps {
		seasons[ep.SeasonNum] = true
	}

	log.Printf("MetadataService: Found %d local episodes in seasons %v for %s", len(localEps), seasons, anime.Title)

	for sNum := range seasons {
		log.Printf("MetadataService: Fetching TMDB Season %d for %s", sNum, anime.Title)
		season, err := performWithRetry(func() (*tmdb.SeasonDetails, error) {
			return tmdbClient.GetSeasonDetails(anime.Metadata.TMDBID, sNum)
		})
		if err != nil || season == nil {
			log.Printf("MetadataService: Failed to fetch TMDB Season %d for %s: %v", sNum, anime.Title, err)
			continue
		}

		// Map TMDB episodes for quick lookup
		tmdbMap := make(map[int]tmdb.Episode)
		for _, ep := range season.Episodes {
			tmdbMap[ep.EpisodeNumber] = ep
		}

		log.Printf("MetadataService: TMDB Season %d has %d episodes for %s", sNum, len(season.Episodes), anime.Title)

		// Update local episodes
		for i := range localEps {
			lep := &localEps[i]
			if lep.SeasonNum == sNum {
				if tep, ok := tmdbMap[lep.EpisodeNum]; ok {
					updated := false
					if lep.Image != tep.StillPath {
						lep.Image = tep.StillPath
						updated = true
					}
					if lep.Summary != tep.Overview {
						lep.Summary = tep.Overview
						updated = true
					}
					if updated {
						log.Printf("MetadataService: Updating Ep %d with image from TMDB for %s", lep.EpisodeNum, anime.Title)
						db.DB.Save(lep)
					}
				} else {
					log.Printf("MetadataService: No TMDB match for S%dE%d for %s", sNum, lep.EpisodeNum, anime.Title)
				}
			}
		}
	}
}

// EnrichMetadata is the CORE logic for parallel scraping
func (s *MetadataService) EnrichMetadata(m *model.AnimeMetadata, query string) {
	bgmClient, tmdbClient, anilistClient := s.initClients()

	queryTitle := parser.CleanTitle(query)
	rawQueryTitle := queryTitle

	// Prepare candidates
	candidates := getCandidateTitles(m, queryTitle)

	var wg sync.WaitGroup
	var mu sync.Mutex

	wg.Add(3)

	// 1. Bangumi Task
	go func() {
		defer wg.Done()
		s.enrichBangumi(m, bgmClient, queryTitle)
	}()

	// 2. AniList Task
	go func() {
		defer wg.Done()
		if anilistClient != nil {
			s.processAniList(m, anilistClient, candidates, &mu)
		}
	}()

	// 3. TMDB Task
	go func() {
		defer wg.Done()
		if tmdbClient != nil {
			s.processTMDB(m, tmdbClient, candidates, &mu)
		}
	}()

	wg.Wait()

	// 4. Cross-Reference: AniList -> Bangumi
	if m.BangumiID == 0 && m.AniListID != 0 {
		retryTitle := ""
		if m.TitleJP != "" {
			retryTitle = m.TitleJP
		} else if m.TitleEN != "" {
			retryTitle = m.TitleEN
		}
		if retryTitle != "" && retryTitle != queryTitle {
			s.enrichBangumi(m, bgmClient, retryTitle)
		}
	}

	// 5. Save and Consolidate
	s.saveAndConsolidate(m)

	// 6. Set Active Fields
	s.setActiveFields(m, rawQueryTitle)

	db.DB.Save(m)
	s.SyncMetadataToModels(m)
}

func (s *MetadataService) initClients() (*bangumi.Client, *tmdb.Client, *anilist.Client) {
	// Bangumi
	bgmClient := bangumi.NewClient("", "", "")
	var bgmProxyConfig model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyProxyBangumi).First(&bgmProxyConfig).Error; err == nil && bgmProxyConfig.Value == model.ConfigValueTrue {
		var p model.GlobalConfig
		if err := db.DB.Where("key = ?", model.ConfigKeyProxyURL).First(&p).Error; err == nil && p.Value != "" {
			bgmClient.SetProxy(p.Value)
		}
	}

	// TMDB
	var tmdbToken model.GlobalConfig
	var tmdbClient *tmdb.Client
	if err := db.DB.Where("key = ?", model.ConfigKeyTMDBToken).First(&tmdbToken).Error; err == nil && tmdbToken.Value != "" {
		proxyURL := ""
		var proxyEnabled model.GlobalConfig
		if err := db.DB.Where("key = ?", model.ConfigKeyProxyTMDB).First(&proxyEnabled).Error; err == nil && proxyEnabled.Value == model.ConfigValueTrue {
			var p model.GlobalConfig
			if err := db.DB.Where("key = ?", model.ConfigKeyProxyURL).First(&p).Error; err == nil {
				proxyURL = p.Value
			}
		}
		tmdbClient = tmdb.NewClient(tmdbToken.Value, proxyURL)
	}

	// AniList
	var anilistToken model.GlobalConfig
	var anilistClient *anilist.Client
	if err := db.DB.Where("key = ?", model.ConfigKeyAniListToken).First(&anilistToken).Error; err == nil && anilistToken.Value != "" {
		proxyURL := ""
		var proxyEnabled model.GlobalConfig
		if err := db.DB.Where("key = ?", model.ConfigKeyProxyAniList).First(&proxyEnabled).Error; err == nil && proxyEnabled.Value == model.ConfigValueTrue {
			var p model.GlobalConfig
			if err := db.DB.Where("key = ?", model.ConfigKeyProxyURL).First(&p).Error; err == nil {
				proxyURL = p.Value
			}
		}
		anilistClient = anilist.NewClient(anilistToken.Value, proxyURL)
	}

	return bgmClient, tmdbClient, anilistClient
}

func (s *MetadataService) enrichBangumi(m *model.AnimeMetadata, bgmClient *bangumi.Client, queryTitle string) {
	var bgmSubject *bangumi.Subject

	if m.BangumiID != 0 {
		bgmSubject, _ = performWithRetry(func() (*bangumi.Subject, error) {
			return bgmClient.GetSubject(m.BangumiID)
		})
	}

	if bgmSubject == nil {
		initialCandidates := getCandidateTitles(m, queryTitle)
		for _, t := range initialCandidates {
			if t == "" {
				continue
			}
			res, err := performWithRetry(func() (*bangumi.SearchResult, error) {
				return bgmClient.SearchSubject(t)
			})
			if err == nil && res != nil {
				m.BangumiID = res.ID
				bgmSubject, _ = performWithRetry(func() (*bangumi.Subject, error) {
					return bgmClient.GetSubject(res.ID)
				})
				break
			}
		}
	}

	if bgmSubject != nil {
		s.applyBangumiSubject(m, bgmSubject)
	}
}

func (s *MetadataService) applyBangumiSubject(m *model.AnimeMetadata, bgmSubject *bangumi.Subject) {
	m.BangumiID = bgmSubject.ID
	m.BangumiImage = bgmSubject.Images.Large
	m.BangumiSummary = bgmSubject.Summary
	m.BangumiRating = bgmSubject.Rating.Score
	if m.AirDate == "" {
		m.AirDate = bgmSubject.Date
	}
	if m.TitleJP == "" {
		m.TitleJP = bgmSubject.Name
	}
	if m.TitleCN == "" {
		m.TitleCN = bgmSubject.NameCN
	}
	if bgmSubject.NameCN != "" {
		m.BangumiTitle = bgmSubject.NameCN
	} else {
		m.BangumiTitle = bgmSubject.Name
	}
	m.BangumiImageRaw = s.fetchAndCacheImage(m.BangumiImage)
}

func (s *MetadataService) processTMDB(m *model.AnimeMetadata, client *tmdb.Client, candidates []string, mu *sync.Mutex) {
	var tmdbShow *tmdb.TVShow

	mu.Lock()
	currentID := m.TMDBID
	mu.Unlock()

	if currentID != 0 {
		tmdbShow, _ = performWithRetry(func() (*tmdb.TVShow, error) {
			return client.GetTVDetails(currentID)
		})
	}

	if tmdbShow == nil {
		for _, t := range candidates {
			if t == "" {
				continue
			}
			shows, err := performWithRetry(func() ([]tmdb.TVShow, error) {
				return client.SearchTV(t)
			})
			if err == nil && len(shows) > 0 {
				tmdbShow = &shows[0]
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

func (s *MetadataService) processAniList(m *model.AnimeMetadata, client *anilist.Client, candidates []string, mu *sync.Mutex) {
	var alMedia *anilist.Media

	mu.Lock()
	currentID := m.AniListID
	mu.Unlock()

	if currentID != 0 {
		alMedia, _ = performWithRetry(func() (*anilist.Media, error) {
			return client.GetAnimeDetails(currentID)
		})
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

func (s *MetadataService) saveAndConsolidate(m *model.AnimeMetadata) {
	if m.ID == 0 {
		var existing model.AnimeMetadata
		found := false
		if m.BangumiID != 0 {
			if err := db.DB.Where("bangumi_id = ?", m.BangumiID).First(&existing).Error; err == nil {
				found = true
			}
		}
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
			if m.BangumiID != 0 {
				existing.BangumiID = m.BangumiID
			}
			if m.TMDBID != 0 {
				existing.TMDBID = m.TMDBID
			}
			if m.AniListID != 0 {
				existing.AniListID = m.AniListID
			}
			*m = existing
		} else {
			db.DB.Create(m)
		}
	} else {
		db.DB.Save(m)
	}
}

func (s *MetadataService) setActiveFields(m *model.AnimeMetadata, rawQueryTitle string) {
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

	if m.ID != 0 {
		m.Image = fmt.Sprintf("/api/posters/%d", m.ID)
	}
	if m.Title == "" {
		m.Title = rawQueryTitle
	}
}

func (s *MetadataService) fetchAndCacheImage(url string) []byte {
	if url == "" {
		return nil
	}
	if strings.HasPrefix(url, "/") {
		// TMDB partial path, internal/tmdb might handle this, but being safe:
		url = "https://image.tmdb.org/t/p/w500" + url
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data
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
	add(m.TitleCN)
	add(m.TitleJP)
	add(m.TitleEN)
	add(query)
	if strings.Contains(query, "-") {
		for _, part := range strings.Split(query, "-") {
			add(part)
		}
	}
	add(m.Title)
	return candidates
}

// StartMetadataMigration background task to cache images for existing records
func (s *MetadataService) StartMetadataMigration() {
	go func() {
		time.Sleep(5 * time.Second)
		log.Println("Migration: Starting background metadata image migration...")
		var list []model.AnimeMetadata
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
				m.Image = fmt.Sprintf("/api/posters/%d", m.ID)
				if err := db.DB.Save(&m).Error; err == nil {
					s.SyncMetadataToModels(&m)
				}
			}
			time.Sleep(1 * time.Second)
		}
		log.Println("Migration: Background image migration completed.")
	}()
}

// SyncMetadataToModels propagates metadata fields to all linked Subscription and LocalAnime records
func (s *MetadataService) SyncMetadataToModels(m *model.AnimeMetadata) {
	if m == nil || m.ID == 0 {
		return
	}

	updates := map[string]interface{}{
		"image":   m.Image,
		"title":   m.Title,
		"summary": m.Summary,
	}

	// 1. Update Subscriptions
	db.DB.Model(&model.Subscription{}).Where("metadata_id = ?", m.ID).Updates(updates)

	// 2. Update LocalAnime
	localUpdates := map[string]interface{}{
		"image":    m.Image,
		"title":    m.Title,
		"summary":  m.Summary,
		"air_date": m.AirDate,
	}
	db.DB.Model(&model.LocalAnime{}).Where("metadata_id = ?", m.ID).Updates(localUpdates)
}

func performWithRetry[T any](op func() (T, error)) (T, error) {
	var result T
	var err error
	for i := 0; i < 3; i++ {
		if i > 0 {
			time.Sleep(1 * time.Second)
		}
		result, err = op()
		if err == nil {
			return result, nil
		}
	}
	return result, err
}

// RegenerateAllNFOs triggers NFO generation for ALL local animes.
func (s *MetadataService) RegenerateAllNFOs() (int, error) {
	var list []model.LocalAnime
	if err := db.DB.Preload("Metadata").Preload("Episodes").Find(&list).Error; err != nil {
		return 0, err
	}

	count := 0
	nfoGen := NewNFOGeneratorService()
	for _, anime := range list {
		if anime.Metadata == nil {
			continue
		}
		if _, err := os.Stat(anime.Path); os.IsNotExist(err) {
			continue
		}

		_ = nfoGen.SaveLocalImages(&anime)
		_ = nfoGen.GenerateTVShowNFO(&anime)
		for _, ep := range anime.Episodes {
			_ = nfoGen.GenerateEpisodeNFO(&ep, &anime)
		}
		count++
	}
	return count, nil
}

// MatchSeries manually links a series to a specific metadata record from a source
func (s *MetadataService) MatchSeries(animeID uint, source string, sourceID int) error {
	var anime model.LocalAnime
	if err := db.DB.Preload("Metadata").First(&anime, animeID).Error; err != nil {
		return err
	}

	m := anime.Metadata
	if m == nil {
		m = &model.AnimeMetadata{}
	}

	switch source {
	case "bangumi":
		m.BangumiID = sourceID
	case "tmdb":
		m.TMDBID = sourceID
	case "anilist":
		m.AniListID = sourceID
	}

	// Re-enrich with fixed ID
	s.EnrichMetadata(m, anime.Title)

	anime.Metadata = m
	anime.MetadataID = &m.ID
	anime.Image = m.Image
	anime.Summary = m.Summary
	db.DB.Save(&anime)

	// Trigger Align and NFO
	if m.TMDBID != 0 {
		s.SyncEpisodesWithTMDB(&anime)
		s.AlignEpisodesWithTMDB(&anime)
	}
	nfoGen := NewNFOGeneratorService()
	_ = nfoGen.SaveLocalImages(&anime)
	_ = nfoGen.GenerateTVShowNFO(&anime)

	return nil
}

// RefreshAllMetadata updates metadata records.
func (s *MetadataService) RefreshAllMetadata(force bool) int {
	log.Printf("Refresh: Starting metadata refresh (force=%v)...", force)
	var allList []model.AnimeMetadata
	db.DB.Find(&allList)

	var list []model.AnimeMetadata
	if force {
		list = allList
	} else {
		for _, m := range allList {
			if m.Summary != "" && (m.BangumiID != 0 || m.TMDBID != 0 || m.AniListID != 0) {
				continue
			}
			list = append(list, m)
		}
	}

	total := len(list)
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
		GlobalRefreshStatus.LastResult = "已是最新"
		statusMu.Unlock()
		return 0
	}

	updatedCount := 0
	var updateMu sync.Mutex
	maxWorkers := 5
	guard := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for i, m := range list {
		guard <- struct{}{}
		wg.Add(1)
		go func(idx int, meta model.AnimeMetadata) {
			defer wg.Done()
			defer func() { <-guard }()

			statusMu.Lock()
			GlobalRefreshStatus.Current = idx + 1
			GlobalRefreshStatus.CurrentTitle = meta.Title
			statusMu.Unlock()

			// Publish Event for SSE
			event.GlobalBus.Publish(event.EventMetadataUpdated, map[string]interface{}{
				"type":    "progress",
				"current": idx + 1,
				"total":   total,
				"title":   meta.Title,
			})

			var freshM model.AnimeMetadata
			if err := db.DB.First(&freshM, meta.ID).Error; err == nil {
				queryTitle := freshM.Title
				if freshM.TitleCN != "" {
					queryTitle = freshM.TitleCN
				}
				s.EnrichMetadata(&freshM, queryTitle)
				updateMu.Lock()
				updatedCount++
				updateMu.Unlock()
			}
			time.Sleep(500 * time.Millisecond)
		}(i, m)
	}
	wg.Wait()

	statusMu.Lock()
	GlobalRefreshStatus.IsRunning = false
	GlobalRefreshStatus.LastResult = fmt.Sprintf("已更新 %d 条", updatedCount)
	statusMu.Unlock()

	// Publish Final Event
	event.GlobalBus.Publish(event.EventMetadataUpdated, map[string]interface{}{
		"type":    "complete",
		"message": GlobalRefreshStatus.LastResult,
	})

	return updatedCount
}

// RefreshSingleMetadata forces a refresh of a single metadata record
func (s *MetadataService) RefreshSingleMetadata(id uint) error {
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
