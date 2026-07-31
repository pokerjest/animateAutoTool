package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"gorm.io/gorm"
)

type AgentService struct {
	NetworkWorkerCount int
	NetworkRateLimit   time.Duration
	metaSvc            *MetadataService
	enrichAnime        func(*model.LocalAnime) error
}

func NewAgentService() *AgentService {
	metaSvc := NewMetadataService()
	return &AgentService{
		NetworkWorkerCount: 2, // Low concurrency for network
		NetworkRateLimit:   500 * time.Millisecond,
		metaSvc:            metaSvc,
		enrichAnime:        metaSvc.EnrichAnime,
	}
}

// RunAgentForLibrary starts the metadata/agent phase for all local animes
func (s *AgentService) RunAgentForLibrary() {
	if _, err := s.RunAgentForLibraryWithRepair(nil); err != nil {
		log.Printf("Agent: failed to repair historical metadata database issues: %v", err)
	}
}

// RunAgentForAnimeIDs performs the metadata phase only for the supplied local
// series. Download completion uses this targeted entry point so an episode
// arriving for one show does not trigger a full-library scrape.
func (s *AgentService) RunAgentForAnimeIDs(ids []uint) {
	if db.DB == nil || len(ids) == 0 {
		return
	}
	unique := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if id != 0 {
			unique[id] = struct{}{}
		}
	}
	if len(unique) == 0 {
		return
	}

	networkQueue := make(chan uint, len(unique))
	for id := range unique {
		var anime model.LocalAnime
		if err := db.DB.Preload("Metadata").First(&anime, id).Error; err != nil {
			continue
		}
		s.scanLocalAssets(&anime)
		if s.animeNeedsNetwork(&anime) {
			networkQueue <- anime.ID
		}
	}
	close(networkQueue)
	s.runNetworkWorkers(networkQueue)
}

// RunAgentForLibraryWithRepair performs the normal enrichment phase and then
// serially retries historical scrape failures caused by SQLite write
// contention. The serial pass is intentionally last: it runs after the normal
// workers have stopped, when the database is least likely to be busy again.
func (s *AgentService) RunAgentForLibraryWithRepair(report MetadataIssueRepairProgressFunc) (MetadataIssueRepairResult, error) {
	log.Println("Agent: Starting metadata enrichment (Agent Phase)...")

	networkQueue := make(chan uint, 1000)
	var wg sync.WaitGroup

	// Start Network Workers
	for i := 0; i < s.NetworkWorkerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.networkWorker(networkQueue)
		}()
	}

	// Producer
	go func() {
		var animes []model.LocalAnime
		batchSize := 100
		err := db.DB.Preload("Metadata").FindInBatches(&animes, batchSize, func(tx *gorm.DB, batch int) error {
			for _, anime := range animes {
				// 1. Level 0: Local Assets (NFO/Images) - Fast, Sync
				s.scanLocalAssets(&anime)

				// 2. Decide if Network Needed
				if s.animeNeedsNetwork(&anime) {
					networkQueue <- anime.ID
				}
			}
			return nil
		}).Error
		if err != nil {
			log.Printf("Agent: FindInBatches failed: %v", err)
		}
		close(networkQueue)
	}()

	wg.Wait()
	log.Println("Agent: Metadata enrichment completed.")
	return s.RepairDatabaseMetadataIssues(context.Background(), report)
}

func (s *AgentService) animeNeedsNetwork(anime *model.LocalAnime) bool {
	if anime == nil || anime.MetadataID == nil || *anime.MetadataID == 0 || anime.Metadata == nil {
		return true
	}
	if anime.Metadata.BangumiID == 0 && anime.Metadata.TMDBID == 0 && anime.Metadata.AniListID == 0 {
		return true
	}
	if anime.Metadata.TMDBID != 0 && db.DB != nil {
		var count int64
		if err := db.DB.Model(&model.LocalEpisode{}).
			Where("local_anime_id = ? AND (image IS NULL OR image = '')", anime.ID).
			Count(&count).Error; err == nil && count > 0 {
			return true
		}
	}
	return false
}

// scanLocalAssets looks for NFOs and local images.
//
//nolint:gocyclo // Asset ingestion keeps field-level precedence in one transaction.
func (s *AgentService) scanLocalAssets(anime *model.LocalAnime) {
	// 1. Check NFO
	nfoPath := filepath.Join(anime.Path, "tvshow.nfo")
	if _, err := os.Stat(nfoPath); err == nil {
		nfo, err := parser.ParseTVShowNFO(nfoPath)
		if err == nil {
			log.Printf("Agent: Found local NFO for %s", anime.Title)
			// Upsert Metadata
			if anime.Metadata == nil || anime.MetadataID == nil || *anime.MetadataID == 0 {
				m := &model.AnimeMetadata{}
				anime.Metadata = m
			}

			// Local NFO is authoritative for identifiers and the primary title,
			// while descriptive network fields only fill gaps by default.
			localSources := map[string]string{}
			if strings.TrimSpace(nfo.Title) != "" {
				anime.Metadata.Title = strings.TrimSpace(nfo.Title)
				localSources["title"] = metadataSourceLocalNFO
			}
			if strings.TrimSpace(nfo.Original) != "" {
				anime.Metadata.OriginalTitle = strings.TrimSpace(nfo.Original)
				localSources["original_title"] = metadataSourceLocalNFO
			}
			if anime.Metadata.TitleJP == "" {
				anime.Metadata.TitleJP = strings.TrimSpace(nfo.Original)
			}
			if strings.TrimSpace(nfo.SortTitle) != "" {
				anime.Metadata.SortTitle = strings.TrimSpace(nfo.SortTitle)
				localSources["sort_title"] = metadataSourceLocalNFO
			}
			if strings.TrimSpace(nfo.Plot) != "" {
				anime.Metadata.Summary = nfo.Plot
				localSources["summary"] = metadataSourceLocalNFO
			}
			if strings.TrimSpace(nfo.Premiered) != "" {
				anime.Metadata.AirDate = nfo.Premiered
				localSources["air_date"] = metadataSourceLocalNFO
			}
			if len(nfo.Genre) > 0 {
				anime.Metadata.Genres = encodeStringList(nfo.Genre)
				localSources["genres"] = metadataSourceLocalNFO
			}
			if len(nfo.Studio) > 0 {
				anime.Metadata.Studios = encodeStringList(nfo.Studio)
				localSources["studios"] = metadataSourceLocalNFO
			}
			if len(nfo.Actor) > 0 {
				names := make([]string, 0, len(nfo.Actor))
				for _, actor := range nfo.Actor {
					if strings.TrimSpace(actor.Name) != "" {
						names = append(names, strings.TrimSpace(actor.Name))
					}
				}
				anime.Metadata.Actors = encodeStringList(names)
				localSources["actors"] = metadataSourceLocalNFO
			}

			hasLocalProviderID := false
			if nfo.BangumiID != 0 {
				anime.Metadata.BangumiID = nfo.BangumiID
				hasLocalProviderID = true
			}
			if nfo.TMDBID != 0 {
				anime.Metadata.TMDBID = nfo.TMDBID
				hasLocalProviderID = true
			}
			for _, uniqueID := range nfo.UniqueIDs {
				id, convErr := strconv.Atoi(strings.TrimSpace(uniqueID.Value))
				if convErr != nil || id <= 0 {
					continue
				}
				switch strings.ToLower(strings.TrimSpace(uniqueID.Type)) {
				case metadataSourceBangumi:
					anime.Metadata.BangumiID = id
					hasLocalProviderID = true
				case metadataSourceTMDB:
					anime.Metadata.TMDBID = id
					hasLocalProviderID = true
				case metadataSourceAniList:
					anime.Metadata.AniListID = id
					hasLocalProviderID = true
				}
			}
			if hasLocalProviderID {
				localSources["provider_ids"] = metadataSourceLocalNFO
			}
			anime.Metadata.FieldSources = mergeFieldSources(anime.Metadata.FieldSources, localSources)

			if err := s.persistLocalAssetMetadata(anime); err != nil {
				log.Printf("Agent: Failed to persist local NFO metadata for %s: %v", anime.Title, err)
			}
		}
	}

	// 2. Check Local Images
	// Priority: poster.jpg > cover.jpg > folder.jpg
	imageNames := []string{"poster.jpg", "poster.png", "cover.jpg", "cover.png", "folder.jpg", "folder.png"}
	for _, name := range imageNames {
		imgPath := filepath.Join(anime.Path, name)
		if _, err := os.Stat(imgPath); err == nil {
			// Found local image!
			// We need to serve this.
			// For now, simple way: Read bytes and store in BLOB (Offline Cache logic)
			// Or better: Agent should prefer local blob over re-downloading.

			data, err := os.ReadFile(filepath.Clean(imgPath)) //nolint:gosec // imgPath is constrained to known local image names inside the anime directory.
			if err == nil && len(data) > 0 {
				if anime.Metadata == nil || anime.MetadataID == nil || *anime.MetadataID == 0 {
					anime.Metadata = &model.AnimeMetadata{Title: anime.Title}
				}

				// Prioritize local image in one of the raw fields or a new field?
				// Reuse TMDBImageRaw for now as generic 'Poster' storage if no source
				// Or better: Logic in GetPosterHandler to fallback.
				// Let's set it to TMDBImageRaw as a hacky "Local/Default" slot if empty
				if len(anime.Metadata.TMDBImageRaw) == 0 {
					anime.Metadata.TMDBImageRaw = data
					anime.Metadata.Image = fmt.Sprintf("/api/v1/posters/%d", anime.Metadata.ID)

					if err := s.persistLocalAssetMetadata(anime); err != nil {
						log.Printf("Agent: Failed to persist local poster for %s: %v", anime.Title, err)
					} else {
						log.Printf("Agent: Consumed local poster for %s", anime.Title)
					}
				}
				break // Found best match
			}
		}
	}
}

func encodeStringList(values []string) string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			filtered = append(filtered, strings.TrimSpace(value))
		}
	}
	if len(filtered) == 0 {
		return ""
	}
	encoded, _ := json.Marshal(filtered)
	return string(encoded)
}

func mergeFieldSources(raw string, updates map[string]string) string {
	sources := map[string]string{}
	_ = json.Unmarshal([]byte(raw), &sources)
	for field, source := range updates {
		sources[field] = source
	}
	encoded, _ := json.Marshal(sources)
	return string(encoded)
}

func (s *AgentService) persistLocalAssetMetadata(anime *model.LocalAnime) error {
	if anime == nil || anime.Metadata == nil {
		return fmt.Errorf("local metadata is empty")
	}
	mStore := metadataStore()
	laStore := localAnimeStore()
	if mStore == nil || laStore == nil {
		return gorm.ErrInvalidDB
	}

	if anime.Metadata.ID == 0 {
		if err := mStore.Create(anime.Metadata); err != nil {
			return err
		}
	} else if err := mStore.Save(anime.Metadata); err != nil {
		return err
	}
	if len(anime.Metadata.BangumiImageRaw) > 0 || len(anime.Metadata.TMDBImageRaw) > 0 || len(anime.Metadata.AniListImageRaw) > 0 {
		posterURL := fmt.Sprintf("/api/v1/posters/%d", anime.Metadata.ID)
		if anime.Metadata.Image != posterURL {
			anime.Metadata.Image = posterURL
			if err := mStore.Save(anime.Metadata); err != nil {
				return err
			}
		}
	}

	anime.MetadataID = &anime.Metadata.ID
	anime.Image = anime.Metadata.Image
	anime.Summary = anime.Metadata.Summary
	if err := laStore.SaveAnime(anime); err != nil {
		return err
	}
	s.metadataService().SyncMetadataToModels(anime.Metadata)
	return nil
}

func (s *AgentService) metadataService() *MetadataService {
	if s.metaSvc == nil {
		s.metaSvc = NewMetadataService()
	}
	return s.metaSvc
}

func (s *AgentService) enrich(anime *model.LocalAnime) error {
	if s.enrichAnime != nil {
		return s.enrichAnime(anime)
	}
	return s.metadataService().EnrichAnime(anime)
}

func (s *AgentService) networkWorker(queue <-chan uint) {
	for id := range queue {
		// Rate Limit
		time.Sleep(s.NetworkRateLimit)

		var anime model.LocalAnime
		if err := db.DB.Preload("Metadata").First(&anime, id).Error; err != nil {
			continue
		}

		// The metadata clients already enforce request timeouts. Run enrichment
		// synchronously inside the worker so wg.Wait truly means every database
		// writer has stopped before the serial repair pass begins.
		s.enrichAndReport(&anime)
	}
}

func (s *AgentService) runNetworkWorkers(queue <-chan uint) {
	workerCount := s.NetworkWorkerCount
	if workerCount <= 0 {
		workerCount = 1
	}
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.networkWorker(queue)
		}()
	}
	wg.Wait()
}

func (s *AgentService) enrichAndReport(anime *model.LocalAnime) {
	if anime == nil {
		return
	}
	log.Printf("Agent: Network enriching %s", anime.Title)
	if err := s.enrich(anime); err != nil {
		log.Printf("Agent: Failed to enrich anime %s: %v", anime.Title, err)
		_ = ReportLibraryIssue(LibraryIssueInput{
			IssueKey:      "scrape:" + strconv.FormatUint(uint64(anime.ID), 10),
			IssueType:     LibraryIssueTypeScrape,
			Title:         anime.Title,
			DirectoryPath: anime.Path,
			LocalAnimeID:  &anime.ID,
			Message:       err.Error(),
			Hint:          MetadataIssueHint(err),
		})
	} else {
		_ = ResolveLibraryIssue("scrape:" + strconv.FormatUint(uint64(anime.ID), 10))
	}
}
