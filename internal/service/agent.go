package service

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
)

type AgentService struct {
	NetworkWorkerCount int
	NetworkRateLimit   time.Duration
	metaSvc            *MetadataService
}

func NewAgentService() *AgentService {
	return &AgentService{
		NetworkWorkerCount: 2, // Low concurrency for network
		NetworkRateLimit:   500 * time.Millisecond,
		metaSvc:            NewMetadataService(),
	}
}

// RunAgentForLibrary starts the metadata/agent phase for all local animes
func (s *AgentService) RunAgentForLibrary() {
	log.Println("Agent: Starting metadata enrichment (Agent Phase)...")

	var animes []model.LocalAnime
	if err := db.DB.Preload("Metadata").Find(&animes).Error; err != nil {
		log.Printf("Agent: Failed to load library: %v", err)
		return
	}

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
		close(networkQueue)
	}()

	wg.Wait()
	log.Println("Agent: Metadata enrichment completed.")
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

			db.DB.Save(anime.Metadata)
			s.metaSvc.SyncMetadataToModels(anime.Metadata)
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

			data, err := os.ReadFile(imgPath)
			if err == nil && len(data) > 0 {
				if anime.MetadataID == nil || *anime.MetadataID == 0 {
					anime.Metadata = &model.AnimeMetadata{Title: anime.Title}
					db.DB.Create(anime.Metadata)
					anime.MetadataID = &anime.Metadata.ID
					db.DB.Save(anime)
				}

				// Prioritize local image in one of the raw fields or a new field?
				// Reuse TMDBImageRaw for now as generic 'Poster' storage if no source
				// Or better: Logic in GetPosterHandler to fallback.
				// Let's set it to TMDBImageRaw as a hacky "Local/Default" slot if empty
				if len(anime.Metadata.TMDBImageRaw) == 0 {
					anime.Metadata.TMDBImageRaw = data
					anime.Metadata.Image = fmt.Sprintf("/api/posters/%d", anime.Metadata.ID)

					db.DB.Save(anime.Metadata)
					s.metaSvc.SyncMetadataToModels(anime.Metadata)
					log.Printf("Agent: Consumed local poster for %s", anime.Title)
				}
				break // Found best match
			}
		}
	}
}

func (s *AgentService) networkWorker(queue <-chan uint) {
	for id := range queue {
		// Rate Limit
		time.Sleep(s.NetworkRateLimit)

		var anime model.LocalAnime
		if err := db.DB.Preload("Metadata").First(&anime, id).Error; err != nil {
			continue
		}

		// Re-use existing Enrich Logic
		log.Printf("Agent: Network enriching %s", anime.Title)
		if err := s.metaSvc.EnrichAnime(&anime); err != nil {
			log.Printf("Agent: Failed to enrich anime %s: %v", anime.Title, err)
		}

		// Save result handled inside EnrichAnimeMetadata
	}
}
