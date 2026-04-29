package service

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/anilist"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
	"github.com/pokerjest/animateAutoTool/internal/tmdb"
)

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
			if err := db.DB.Save(anime).Error; err != nil {
				return fmt.Errorf("save anime metadata link: %w", err)
			}
		} else {
			anime.Metadata = &model.AnimeMetadata{Title: queryTitle}
		}
	}

	// 2. Full Enrichment
	s.EnrichMetadata(anime.Metadata, anime.Title)
	if anime.Metadata == nil {
		return fmt.Errorf("metadata enrichment returned nil metadata for %s", anime.Title)
	}

	// 3. Link and Save
	if anime.Metadata != nil && anime.Metadata.ID != 0 {
		anime.MetadataID = &anime.Metadata.ID
	}

	// Sync to anime model
	anime.Image = anime.Metadata.Image
	anime.Summary = anime.Metadata.Summary

	if err := db.DB.Save(anime).Error; err != nil {
		return fmt.Errorf("save enriched anime: %w", err)
	}

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

	if err := db.DB.Save(m).Error; err != nil {
		log.Printf("MetadataService: failed to save consolidated metadata for %q: %v", rawQueryTitle, err)
	}
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
		if !shouldApplyBangumiSubject(m, bgmSubject, queryTitle) {
			log.Printf("MetadataService: skipping mismatched Bangumi subject %d for query=%q (subject=%q/%q)", bgmSubject.ID, queryTitle, bgmSubject.NameCN, bgmSubject.Name)
			if m.ID == 0 {
				m.BangumiID = 0
			}
			return
		}
		s.applyBangumiSubject(m, bgmSubject)
	}
}

func (s *MetadataService) applyBangumiSubject(m *model.AnimeMetadata, bgmSubject *bangumi.Subject) {
	m.BangumiID = bgmSubject.ID
	m.BangumiImage = bgmSubject.Images.Large
	m.BangumiSummary = bgmSubject.Summary
	m.BangumiRating = bgmSubject.Rating.Score
	if bgmSubject.Date != "" {
		m.AirDate = bgmSubject.Date
	}
	if bgmSubject.Name != "" {
		m.TitleJP = bgmSubject.Name
	}
	if bgmSubject.NameCN != "" {
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
			if err := db.DB.Create(m).Error; err != nil {
				log.Printf("MetadataService: failed to create metadata for %q: %v", m.Title, err)
			}
		}
	} else {
		if err := db.DB.Save(m).Error; err != nil {
			log.Printf("MetadataService: failed to persist metadata %d: %v", m.ID, err)
		}
	}
}

func (s *MetadataService) setActiveFields(m *model.AnimeMetadata, rawQueryTitle string) {
	selected := selectMetadataSource(rawQueryTitle, m)
	if selected != nil {
		m.Title = selected.title
		m.Image = selected.image
		m.Summary = selected.summary
		m.DataSource = selected.name
		if m.Summary == "" {
			m.Summary = fallbackSummaryForSource(selected.name, m)
		}
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
	defer safeio.Close(resp.Body)
	data, _ := io.ReadAll(resp.Body)
	return data
}
