package service

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/anilist"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/httpx"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
	"github.com/pokerjest/animateAutoTool/internal/tmdb"
)

type MetadataService struct{}

type metadataProviderTrust struct {
	bangumi bool
	tmdb    bool
	aniList bool
}

func NewMetadataService() *MetadataService {
	return &MetadataService{}
}

func MetadataIssueHint(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "sqlite_busy"),
		strings.Contains(message, "database is locked"),
		strings.Contains(message, "database table is locked"):
		return "数据库正在处理其他任务，系统会自动重试；若仍失败，请等待扫描或整理结束后再次刷新元数据。"
	case strings.Contains(message, "timeout"),
		strings.Contains(message, "deadline exceeded"),
		strings.Contains(message, "connection reset"),
		strings.Contains(message, "eof"):
		return "元数据服务连接不稳定，请检查网络或代理设置后再次刷新。"
	default:
		return "检查元数据源配置，或在详情里使用修正匹配手动关联番剧。"
	}
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

	detachMismatchedSharedMetadataLink(anime)

	// 1. Ensure Metadata record exists or link to existing
	if anime.Metadata == nil || anime.MetadataID == nil || *anime.MetadataID == 0 {
		queryTitle := parser.CleanTitle(anime.Title)
		log.Printf("Enrich: Attempting to link '%s' to existing metadata...", queryTitle)

		laStore := localAnimeStore()
		existing, err := findMetadataByTitleVariants(queryTitle)
		if err == nil && existing != nil && existing.ID != 0 &&
			metadataCanBridgeTitles(existing) &&
			localAnimeMatchesMetadataIdentity(anime, existing) {
			log.Printf("Enrich: Found existing metadata link for '%s' -> ID %d", anime.Title, existing.ID)
			anime.Metadata = existing
			anime.MetadataID = &existing.ID
			if laStore != nil {
				if err := laStore.SaveAnime(anime); err != nil {
					return fmt.Errorf("save anime metadata link: %w", err)
				}
			}
		} else {
			if err == nil && existing != nil && existing.ID != 0 {
				log.Printf(
					"WARN: Enrich: rejected suspicious title-only metadata reuse local_anime_id=%d title=%q metadata_id=%d metadata_title=%q source_conflict=%t",
					anime.ID,
					anime.Title,
					existing.ID,
					existing.Title,
					metadataSourcesConflict(existing),
				)
			}
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

	if laStore := localAnimeStore(); laStore != nil {
		if err := laStore.SaveAnime(anime); err != nil {
			return fmt.Errorf("save enriched anime: %w", err)
		}
	}

	// 4. Align Episodes and Sync Metadata (Phase 4)
	if anime.Metadata.TMDBID != 0 {
		s.SyncEpisodesWithTMDB(anime)
		s.AlignEpisodesWithTMDB(anime)
	}

	// 5. NFO Generation (Phase 4)
	nfoGen := NewNFOGeneratorService()
	if err := nfoGen.SaveLocalImages(anime); err != nil {
		log.Printf("WARN: MetadataService: local image write failed local_anime_id=%d recovery_action=retry_nfo_generation error=%v", anime.ID, err)
	}
	if err := nfoGen.GenerateTVShowNFO(anime); err != nil {
		log.Printf("WARN: MetadataService: tvshow NFO write failed local_anime_id=%d recovery_action=retry_nfo_generation error=%v", anime.ID, err)
	}

	return nil
}

// EnrichSubscription isolates a subscription from a suspicious shared
// metadata row before any provider can mutate that row. The caller remains
// responsible for saving the subscription after enrichment succeeds.
func (s *MetadataService) EnrichSubscription(sub *model.Subscription) error {
	if sub == nil {
		return fmt.Errorf("subscription is nil")
	}
	if sub.Metadata == nil && sub.MetadataID != nil && *sub.MetadataID != 0 {
		if mStore := metadataStore(); mStore != nil {
			if metadata, err := mStore.GetByID(*sub.MetadataID); err == nil {
				sub.Metadata = metadata
			}
		}
	}

	detachMismatchedSharedSubscriptionMetadataLink(sub)
	if sub.Metadata == nil || sub.MetadataID == nil || *sub.MetadataID == 0 {
		sub.Metadata = &model.AnimeMetadata{Title: parser.CleanTitle(sub.Title)}
	}

	s.EnrichMetadata(sub.Metadata, sub.Title)
	if sub.Metadata == nil || sub.Metadata.ID == 0 {
		return fmt.Errorf("metadata enrichment returned no persisted metadata for %s", sub.Title)
	}

	sub.MetadataID = &sub.Metadata.ID
	sub.Image = sub.Metadata.Image
	sub.Summary = sub.Metadata.Summary
	return nil
}

// EnrichMetadata is the CORE logic for parallel scraping
func (s *MetadataService) EnrichMetadata(m *model.AnimeMetadata, query string) {
	s.enrichMetadata(m, query, metadataProviderTrust{})
}

func (s *MetadataService) enrichMetadata(m *model.AnimeMetadata, query string, trust metadataProviderTrust) {
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
		runMetadataProvider("bangumi", m, func() {
			s.enrichBangumi(m, bgmClient, queryTitle, trust.bangumi, &mu)
		})
	}()

	// 2. AniList Task
	go func() {
		defer wg.Done()
		runMetadataProvider("anilist", m, func() {
			if anilistClient != nil {
				s.processAniList(m, anilistClient, candidates, queryTitle, trust.aniList, &mu)
			}
		})
	}()

	// 3. TMDB Task
	go func() {
		defer wg.Done()
		runMetadataProvider("tmdb", m, func() {
			if tmdbClient != nil {
				s.processTMDB(m, tmdbClient, candidates, queryTitle, trust.tmdb, &mu)
			}
		})
	}()

	wg.Wait()

	// 4. Cross-reference the now-populated provider details. The first three
	// requests run in parallel and only know the original title; when a
	// provider supplied a better title (or an explicitly selected source ID),
	// retry every missing provider with those title variants. This is the
	// deterministic path used by manual matching as well as background refresh.
	mu.Lock()
	crossReferenceTitles := getCandidateTitles(m, queryTitle)
	mu.Unlock()
	if m.BangumiID == 0 {
		runMetadataProvider("bangumi-cross-reference", m, func() {
			s.enrichBangumi(m, bgmClient, queryTitle, false, &mu)
		})
	}
	if m.TMDBID == 0 && tmdbClient != nil {
		runMetadataProvider("tmdb-cross-reference", m, func() {
			s.processTMDB(m, tmdbClient, crossReferenceTitles, queryTitle, false, &mu)
		})
	}
	if m.AniListID == 0 && anilistClient != nil {
		runMetadataProvider("anilist-cross-reference", m, func() {
			s.processAniList(m, anilistClient, crossReferenceTitles, queryTitle, false, &mu)
		})
	}

	// 5. Save and Consolidate
	s.saveAndConsolidate(m)

	// 6. Set Active Fields
	s.setActiveFields(m, rawQueryTitle)

	if mStore := metadataStore(); mStore != nil {
		if err := mStore.Save(m); err != nil {
			log.Printf("MetadataService: failed to save consolidated metadata for %q: %v", rawQueryTitle, err)
		}
	}
	s.SyncMetadataToModels(m)
}

func runMetadataProvider(provider string, metadata *model.AnimeMetadata, run func()) {
	defer recoverMetadataProviderPanic(provider, metadata)
	if run != nil {
		run()
	}
}

func recoverMetadataProviderPanic(provider string, metadata *model.AnimeMetadata) {
	recovered := recover()
	if recovered == nil {
		return
	}
	var metadataID uint
	if metadata != nil {
		metadataID = metadata.ID
	}
	log.Printf(
		"ERROR: MetadataService: provider panic provider=%s metadata_id=%d recovery_action=continue_other_providers panic=%v\n%s",
		provider,
		metadataID,
		recovered,
		debug.Stack(),
	)
}

func (s *MetadataService) initClients() (*bangumi.Client, *tmdb.Client, *anilist.Client) {
	// Bangumi
	bgmClient := bangumi.NewClient("", "", "")
	if proxyURL := configuredProxyURL(model.ConfigKeyProxyBangumi); proxyURL != "" {
		if err := bgmClient.SetProxy(proxyURL); err != nil {
			log.Printf("MetadataService: failed to configure Bangumi proxy: %v", err)
		}
	}

	// TMDB
	var tmdbClient *tmdb.Client
	if token := configValue(model.ConfigKeyTMDBToken); token != "" {
		tmdbClient = tmdb.NewClient(token, configuredProxyURL(model.ConfigKeyProxyTMDB))
	}

	// AniList
	var anilistClient *anilist.Client
	if token := configValue(model.ConfigKeyAniListToken); token != "" {
		anilistClient = anilist.NewClient(token, configuredProxyURL(model.ConfigKeyProxyAniList))
	}

	return bgmClient, tmdbClient, anilistClient
}

func (s *MetadataService) enrichBangumi(
	m *model.AnimeMetadata,
	bgmClient *bangumi.Client,
	queryTitle string,
	trustCurrentID bool,
	mu *sync.Mutex,
) {
	var bgmSubject *bangumi.Subject
	mu.Lock()
	references := bangumiMatchReferences(m, queryTitle)
	currentID := m.BangumiID
	mu.Unlock()

	if currentID != 0 {
		bgmSubject, _ = performWithRetry(func() (*bangumi.Subject, error) {
			return bgmClient.GetSubject(currentID)
		})
	}

	if bgmSubject == nil {
		var bestResult *bangumi.SearchResult
		bestScore := 0
		for _, t := range references {
			if t == "" {
				continue
			}
			results, err := performWithRetry(func() ([]bangumi.SearchResult, error) {
				return bgmClient.SearchSubjects(t)
			})
			if err != nil {
				continue
			}
			result, score := bestBangumiSearchResult(results, references)
			if result != nil && score > bestScore {
				bestResult = result
				bestScore = score
			}
			if bestScore == 100 {
				break
			}
		}
		if bestResult != nil {
			bgmSubject, _ = performWithRetry(func() (*bangumi.Subject, error) {
				return bgmClient.GetSubject(bestResult.ID)
			})
		}
	}

	if bgmSubject != nil {
		mu.Lock()
		defer mu.Unlock()
		if !trustCurrentID && !shouldApplyBangumiSubject(m, bgmSubject, queryTitle) {
			log.Printf("MetadataService: skipping mismatched Bangumi subject %d for query=%q (subject=%q/%q)", bgmSubject.ID, queryTitle, bgmSubject.NameCN, bgmSubject.Name)
			s.clearMismatchedBangumiSubject(m, bgmSubject)
			return
		}
		s.applyBangumiSubject(m, bgmSubject)
	}
}

func (s *MetadataService) clearMismatchedBangumiSubject(m *model.AnimeMetadata, subject *bangumi.Subject) {
	if m == nil || subject == nil {
		return
	}
	staleTitles := map[string]bool{
		strings.TrimSpace(subject.Name):   true,
		strings.TrimSpace(subject.NameCN): true,
		strings.TrimSpace(m.BangumiTitle): true,
	}
	if staleTitles[strings.TrimSpace(m.Title)] {
		m.Title = ""
	}
	if staleTitles[strings.TrimSpace(m.TitleCN)] {
		m.TitleCN = ""
	}
	if staleTitles[strings.TrimSpace(m.TitleJP)] {
		m.TitleJP = ""
	}
	if strings.TrimSpace(m.AirDate) == strings.TrimSpace(subject.Date) {
		m.AirDate = ""
	}
	if m.Summary == m.BangumiSummary || m.Summary == subject.Summary {
		m.Summary = ""
	}
	m.BangumiID = 0
	m.BangumiTitle = ""
	m.BangumiImage = ""
	m.BangumiSummary = ""
	m.BangumiRating = 0
	m.BangumiImageRaw = nil
	if m.DataSource == metadataSourceBangumi {
		m.DataSource = ""
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
	m.BangumiImageRaw = s.fetchAndCacheImage(m.BangumiImage, model.ConfigKeyProxyBangumi)
}

func (s *MetadataService) processTMDB(
	m *model.AnimeMetadata,
	client *tmdb.Client,
	candidates []string,
	queryTitle string,
	trustCurrentID bool,
	mu *sync.Mutex,
) {
	var tmdbShow *tmdb.TVShow

	mu.Lock()
	currentID := m.TMDBID
	references := providerMatchReferences(m, queryTitle, metadataSourceTMDB)
	mu.Unlock()

	if currentID != 0 {
		tmdbShow, _ = performWithRetry(func() (*tmdb.TVShow, error) {
			return client.GetTVDetails(currentID)
		})
		if tmdbShow != nil && !trustCurrentID &&
			providerCandidateScore(references, []string{tmdbShow.Name, tmdbShow.OriginalName}) < 45 {
			log.Printf(
				"MetadataService: skipping mismatched TMDB show %d for query=%q (show=%q/%q)",
				tmdbShow.ID,
				queryTitle,
				tmdbShow.Name,
				tmdbShow.OriginalName,
			)
			mu.Lock()
			s.clearMismatchedTMDBShow(m)
			mu.Unlock()
			tmdbShow = nil
		}
	}

	if tmdbShow == nil {
		bestScore := 0
		for _, t := range candidates {
			if t == "" {
				continue
			}
			shows, err := performWithRetry(func() ([]tmdb.TVShow, error) {
				return client.SearchTV(t)
			})
			if err != nil {
				continue
			}
			show, score := bestTMDBSearchResult(shows, references)
			if show != nil && score > bestScore {
				tmdbShow = show
				bestScore = score
			}
			if bestScore == 100 {
				break
			}
		}
	}

	if tmdbShow != nil {
		imgRaw := s.fetchAndCacheImage(tmdbShow.PosterPath, model.ConfigKeyProxyTMDB)
		backdropRaw := s.fetchAndCacheImage(tmdbShow.BackdropPath, model.ConfigKeyProxyTMDB)
		mu.Lock()
		m.TMDBID = tmdbShow.ID
		m.TMDBTitle = tmdbShow.Name
		m.TMDBImage = tmdbShow.PosterPath
		m.TMDBBackdrop = tmdbShow.BackdropPath
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
		m.TMDBBackdropRaw = backdropRaw
		mu.Unlock()
	}
}

func (s *MetadataService) clearMismatchedTMDBShow(m *model.AnimeMetadata) {
	if m == nil {
		return
	}
	m.TMDBID = 0
	m.TMDBTitle = ""
	m.TMDBImage = ""
	m.TMDBBackdrop = ""
	m.TMDBSummary = ""
	m.TMDBRating = 0
	m.TMDBImageRaw = nil
	m.TMDBBackdropRaw = nil
	if m.DataSource == metadataSourceTMDB {
		m.DataSource = ""
	}
}

func bestTMDBSearchResult(results []tmdb.TVShow, references []string) (*tmdb.TVShow, int) {
	bestIndex := -1
	bestScore := 0
	for i := range results {
		score := providerCandidateScore(references, []string{results[i].Name, results[i].OriginalName})
		if score > bestScore {
			bestIndex = i
			bestScore = score
		}
	}
	if bestIndex < 0 || bestScore < 45 {
		return nil, bestScore
	}
	result := results[bestIndex]
	return &result, bestScore
}

func (s *MetadataService) processAniList(
	m *model.AnimeMetadata,
	client *anilist.Client,
	candidates []string,
	queryTitle string,
	trustCurrentID bool,
	mu *sync.Mutex,
) {
	var alMedia *anilist.Media

	mu.Lock()
	currentID := m.AniListID
	references := providerMatchReferences(m, queryTitle, metadataSourceAniList)
	mu.Unlock()

	if currentID != 0 {
		alMedia, _ = performWithRetry(func() (*anilist.Media, error) {
			return client.GetAnimeDetails(currentID)
		})
		if alMedia != nil && !trustCurrentID &&
			providerCandidateScore(references, aniListMediaTitles(alMedia)) < 45 {
			log.Printf(
				"MetadataService: skipping mismatched AniList media %d for query=%q (titles=%q/%q/%q)",
				alMedia.ID,
				queryTitle,
				alMedia.Title.Romaji,
				alMedia.Title.English,
				alMedia.Title.Native,
			)
			mu.Lock()
			s.clearMismatchedAniListMedia(m)
			mu.Unlock()
			alMedia = nil
		}
	}

	if alMedia == nil {
		bestScore := 0
		for _, t := range candidates {
			if t == "" {
				continue
			}
			results, err := performWithRetry(func() ([]anilist.Media, error) {
				return client.SearchAnimeResultsContext(context.Background(), t, 10)
			})
			if err != nil {
				continue
			}
			media, score := bestAniListSearchResult(results, references)
			if media != nil && score > bestScore {
				alMedia = media
				bestScore = score
			}
			if bestScore == 100 {
				break
			}
		}
	}

	if alMedia != nil {
		imgRaw := s.fetchAndCacheImage(alMedia.CoverImage.ExtraLarge, model.ConfigKeyProxyAniList)
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

func (s *MetadataService) clearMismatchedAniListMedia(m *model.AnimeMetadata) {
	if m == nil {
		return
	}
	m.AniListID = 0
	m.AniListTitle = ""
	m.AniListImage = ""
	m.AniListSummary = ""
	m.AniListRating = 0
	m.AniListImageRaw = nil
	if m.DataSource == metadataSourceAniList {
		m.DataSource = ""
	}
}

func aniListMediaTitles(media *anilist.Media) []string {
	if media == nil {
		return nil
	}
	return []string{media.Title.Romaji, media.Title.English, media.Title.Native}
}

func bestAniListSearchResult(results []anilist.Media, references []string) (*anilist.Media, int) {
	bestIndex := -1
	bestScore := 0
	for i := range results {
		score := providerCandidateScore(references, aniListMediaTitles(&results[i]))
		if score > bestScore {
			bestIndex = i
			bestScore = score
		}
	}
	if bestIndex < 0 || bestScore < 45 {
		return nil, bestScore
	}
	result := results[bestIndex]
	return &result, bestScore
}

func (s *MetadataService) saveAndConsolidate(m *model.AnimeMetadata) {
	mStore := metadataStore()
	if mStore == nil {
		return
	}
	if m.ID == 0 {
		existing := findExistingMetadataByProviderIDs(mStore, m)
		if existing != nil {
			mergeEnrichedMetadata(existing, m)
			if err := mStore.SaveIncludingDeleted(existing); err != nil {
				log.Printf("MetadataService: failed to reuse metadata %d for %q: %v", existing.ID, m.Title, err)
			}
			*m = *existing
		} else {
			if err := mStore.Create(m); err != nil {
				// Another enrichment worker may have inserted the same provider ID,
				// or a soft-deleted row may still own its unique index. Resolve and
				// reuse that row after the failed insert instead of leaving m.ID at 0.
				existing, findErr := mStore.FindByExternalIDsIncludingDeleted(m.BangumiID, m.TMDBID, m.AniListID)
				if findErr != nil {
					log.Printf("MetadataService: failed to create metadata for %q: %v", m.Title, err)
					return
				}
				mergeEnrichedMetadata(existing, m)
				if saveErr := mStore.SaveIncludingDeleted(existing); saveErr != nil {
					log.Printf("MetadataService: failed to recover metadata %d for %q: %v", existing.ID, m.Title, saveErr)
					return
				}
				*m = *existing
			}
		}
	} else {
		if err := mStore.Save(m); err != nil {
			log.Printf("MetadataService: failed to persist metadata %d: %v", m.ID, err)
		}
	}
}

func findExistingMetadataByProviderIDs(mStore interface {
	FindByBangumiID(int) (*model.AnimeMetadata, error)
	FindByTMDBID(int) (*model.AnimeMetadata, error)
	FindByAniListID(int) (*model.AnimeMetadata, error)
	FindByExternalIDsIncludingDeleted(int, int, int) (*model.AnimeMetadata, error)
}, m *model.AnimeMetadata) *model.AnimeMetadata {
	if m.BangumiID != 0 {
		if found, err := mStore.FindByBangumiID(m.BangumiID); err == nil {
			return found
		}
	}
	if m.TMDBID != 0 {
		if found, err := mStore.FindByTMDBID(m.TMDBID); err == nil {
			return found
		}
	}
	if m.AniListID != 0 {
		if found, err := mStore.FindByAniListID(m.AniListID); err == nil {
			return found
		}
	}
	found, err := mStore.FindByExternalIDsIncludingDeleted(m.BangumiID, m.TMDBID, m.AniListID)
	if err != nil {
		return nil
	}
	return found
}

func mergeEnrichedMetadata(dst, src *model.AnimeMetadata) {
	copyString := func(target *string, value string) {
		if value != "" {
			*target = value
		}
	}
	copyInt := func(target *int, value int) {
		if value != 0 {
			*target = value
		}
	}
	copyFloat := func(target *float64, value float64) {
		if value != 0 {
			*target = value
		}
	}
	copyBytes := func(target *[]byte, value []byte) {
		if len(value) != 0 {
			*target = value
		}
	}

	copyString(&dst.Title, src.Title)
	copyString(&dst.Image, src.Image)
	copyString(&dst.Summary, src.Summary)
	copyString(&dst.AirDate, src.AirDate)
	copyString(&dst.TitleCN, src.TitleCN)
	copyString(&dst.TitleEN, src.TitleEN)
	copyString(&dst.TitleJP, src.TitleJP)
	copyString(&dst.SortTitle, src.SortTitle)
	copyString(&dst.OriginalTitle, src.OriginalTitle)
	copyString(&dst.Genres, src.Genres)
	copyString(&dst.Studios, src.Studios)
	copyString(&dst.Tags, src.Tags)
	copyString(&dst.Actors, src.Actors)
	copyString(&dst.Directors, src.Directors)
	copyInt(&dst.RuntimeMinutes, src.RuntimeMinutes)
	copyString(&dst.ContentRating, src.ContentRating)
	copyString(&dst.OriginalCountry, src.OriginalCountry)
	copyInt(&dst.BangumiID, src.BangumiID)
	copyInt(&dst.TMDBID, src.TMDBID)
	copyInt(&dst.AniListID, src.AniListID)
	copyString(&dst.BangumiTitle, src.BangumiTitle)
	copyString(&dst.BangumiImage, src.BangumiImage)
	copyString(&dst.BangumiSummary, src.BangumiSummary)
	copyFloat(&dst.BangumiRating, src.BangumiRating)
	copyBytes(&dst.BangumiImageRaw, src.BangumiImageRaw)
	copyString(&dst.TMDBTitle, src.TMDBTitle)
	copyString(&dst.TMDBImage, src.TMDBImage)
	copyString(&dst.TMDBBackdrop, src.TMDBBackdrop)
	copyString(&dst.TMDBSummary, src.TMDBSummary)
	copyFloat(&dst.TMDBRating, src.TMDBRating)
	copyBytes(&dst.TMDBImageRaw, src.TMDBImageRaw)
	copyBytes(&dst.TMDBBackdropRaw, src.TMDBBackdropRaw)
	copyString(&dst.AniListTitle, src.AniListTitle)
	copyString(&dst.AniListImage, src.AniListImage)
	copyString(&dst.AniListSummary, src.AniListSummary)
	copyFloat(&dst.AniListRating, src.AniListRating)
	copyBytes(&dst.AniListImageRaw, src.AniListImageRaw)
	copyString(&dst.DataSource, src.DataSource)
	copyString(&dst.FieldSources, src.FieldSources)
	copyInt(&dst.BangumiWatchedEps, src.BangumiWatchedEps)
	copyInt(&dst.AniListWatchedEps, src.AniListWatchedEps)
	dst.DeletedAt.Valid = false
}

func (s *MetadataService) setActiveFields(m *model.AnimeMetadata, rawQueryTitle string) {
	selected := selectMetadataSource(rawQueryTitle, m)
	fieldSources := metadataFieldSources(m.FieldSources)
	if selected != nil {
		m.DataSource = selected.name
		if !metadataFieldLocked(fieldSources, "title") && selected.title != "" {
			m.Title = selected.title
			fieldSources["title"] = selected.name
		}
	}

	if !metadataFieldLocked(fieldSources, "image") {
		if value, source := firstConfiguredMetadataField(m, "image"); value != "" {
			m.Image = value
			fieldSources["image"] = source
		}
	}
	if !metadataFieldLocked(fieldSources, "summary") {
		if value, source := firstConfiguredMetadataField(m, "summary"); value != "" {
			m.Summary = value
			fieldSources["summary"] = source
		}
	}

	if m.ID != 0 {
		m.Image = fmt.Sprintf("/api/v1/posters/%d", m.ID)
	}
	if m.Title == "" {
		m.Title = rawQueryTitle
	}
	m.FieldSources = mergeFieldSources(m.FieldSources, fieldSources)
}

func (s *MetadataService) fetchAndCacheImage(url, proxyFlagKey string) []byte {
	if url == "" {
		return nil
	}
	if strings.HasPrefix(url, "/") {
		// TMDB partial path, internal/tmdb might handle this, but being safe:
		url = "https://image.tmdb.org/t/p/w500" + url
	}
	client := httpx.NewHTTPClientWithProxy(15*time.Second, configuredProxyURL(proxyFlagKey))
	resp, err := client.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil
	}
	defer safeio.Close(resp.Body)
	data, _ := io.ReadAll(resp.Body)
	return data
}
