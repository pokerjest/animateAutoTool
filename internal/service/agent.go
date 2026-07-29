package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
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
				// If missing IDs or Summary, queue it
				needsNetwork := false
				if anime.MetadataID == nil || *anime.MetadataID == 0 {
					needsNetwork = true
				} else {
					if anime.Metadata.BangumiID == 0 && anime.Metadata.TMDBID == 0 {
						needsNetwork = true
					} else if anime.Metadata.TMDBID != 0 {
						// Check if any episode is missing metadata (handle NULL or empty)
						var count int64
						db.DB.Model(&model.LocalEpisode{}).Where("local_anime_id = ? AND (image IS NULL OR image = '')", anime.ID).Count(&count)
						if count > 0 {
							needsNetwork = true
						}
					}
				}

				if needsNetwork {
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

// scanLocalAssets looks for NFOs and local images
func (s *AgentService) scanLocalAssets(anime *model.LocalAnime) {
	// 1. Check NFO
	nfoPath := filepath.Join(anime.Path, "tvshow.nfo")
	if _, err := os.Stat(nfoPath); err == nil {
		nfo, err := parser.ParseTVShowNFO(nfoPath)
		if err == nil {
			log.Printf("Agent: Found local NFO for %s", anime.Title)
			// Upsert Metadata
			if anime.MetadataID == nil || *anime.MetadataID == 0 {
				m := &model.AnimeMetadata{}
				anime.Metadata = m
			}

			// Fill Data from NFO
			anime.Metadata.Title = nfo.Title
			anime.Metadata.TitleCN = nfo.Title // Assumption
			anime.Metadata.Summary = nfo.Plot

			if nfo.BangumiID != 0 {
				anime.Metadata.BangumiID = nfo.BangumiID
			}
			if nfo.TMDBID != 0 {
				anime.Metadata.TMDBID = nfo.TMDBID
			}

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
				if anime.MetadataID == nil || *anime.MetadataID == 0 {
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
		log.Printf("Agent: Network enriching %s", anime.Title)
		if err := s.enrich(&anime); err != nil {
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
}
