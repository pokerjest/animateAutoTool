package service

import (
	"fmt"
	"log"

	"github.com/pokerjest/animateAutoTool/internal/anilist"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/tmdb"
)

type MetadataService struct {
	bgmClient     *bangumi.Client
	tmdbClient    *tmdb.Client
	anilistClient *anilist.Client
}

func NewMetadataService() *MetadataService {
	// Configs should be loaded from DB or Cache

	// Load TMDB Key
	var tmdbKey model.GlobalConfig
	db.DB.Where("key = ?", model.ConfigKeyTMDBToken).First(&tmdbKey)

	// Load Proxy from config
	var proxyCfg model.GlobalConfig
	db.DB.Where("key = ?", model.ConfigKeyProxyURL).First(&proxyCfg)
	proxy := proxyCfg.Value

	return &MetadataService{
		bgmClient:     bangumi.NewClient("", "", ""), // No auth needed for public read
		tmdbClient:    tmdb.NewClient(tmdbKey.Value, proxy),
		anilistClient: anilist.NewClient("", proxy),
	}
}

// FetchMetadata inputs a parsed title and returns unified metadata
// It DOES NOT save to DB. It just returns data.
func (s *MetadataService) FetchMetadata(keyword string) (*model.AnimeMetadata, error) {
	// Note: SearchSubject returns *SearchResult, SearchSubjects returns []SearchResult
	bgmRes, err := s.bgmClient.SearchSubjects(keyword)
	if err != nil {
		return nil, err
	}

	if len(bgmRes) == 0 {
		return nil, fmt.Errorf("no results found")
	}

	match := bgmRes[0]

	// Convert to internal model
	meta := &model.AnimeMetadata{
		BangumiID: match.ID,
		TitleCN:   match.NameCN,
		TitleJP:   match.Name,
		// Summary might not be available in Search Result (ResponseGroup=small)
		Image: match.Images.Large,
	}
	if meta.TitleCN == "" {
		meta.TitleCN = meta.TitleJP
	}

	return meta, nil
}

// EnrichAnime updates an Anime record with metadata
func (s *MetadataService) EnrichAnime(anime *model.LocalAnime) error {
	if anime == nil {
		return fmt.Errorf("anime is nil")
	}

	log.Printf("MetadataService: Enriching %s", anime.Title)

	meta, err := s.FetchMetadata(anime.Title)
	if err != nil {
		log.Printf("MetadataService: Failed to find metadata for %s: %v", anime.Title, err)
		return err
	}

	// Update DB
	db.DB.Create(meta) // Create metadata record

	anime.MetadataID = &meta.ID
	anime.Metadata = meta
	anime.Image = meta.Image
	anime.Summary = meta.Summary

	return db.DB.Save(anime).Error
}
