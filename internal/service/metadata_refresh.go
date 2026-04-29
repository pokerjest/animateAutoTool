package service

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

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
	case metadataSourceBangumi:
		m.BangumiID = sourceID
	case metadataSourceTMDB:
		m.TMDBID = sourceID
	case metadataSourceAniList:
		m.AniListID = sourceID
	}

	s.EnrichMetadata(m, anime.Title)

	anime.Metadata = m
	anime.MetadataID = &m.ID
	anime.Image = m.Image
	anime.Summary = m.Summary
	db.DB.Save(&anime)

	if m.TMDBID != 0 {
		s.SyncEpisodesWithTMDB(&anime)
		s.AlignEpisodesWithTMDB(&anime)
	}
	nfoGen := NewNFOGeneratorService()
	_ = nfoGen.SaveLocalImages(&anime)
	_ = nfoGen.GenerateTVShowNFO(&anime)
	_ = ResolveLibraryIssue("scrape:" + strconv.FormatUint(uint64(anime.ID), 10))

	return nil
}

// RefreshAllMetadata updates metadata records.
func (s *MetadataService) StartRefreshAllMetadata(force bool) bool {
	if !GlobalRefreshStatus.TryStart() {
		return false
	}

	go s.RefreshAllMetadata(force)
	return true
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
	GlobalRefreshStatus.SetTotal(total)

	if total == 0 {
		GlobalRefreshStatus.Finish("已是最新")
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

			GlobalRefreshStatus.UpdateProgress(idx+1, meta.Title)

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

	finalStatus := GlobalRefreshStatus.Finish(fmt.Sprintf("已更新 %d 条", updatedCount))

	event.GlobalBus.Publish(event.EventMetadataUpdated, map[string]interface{}{
		"type":    "complete",
		"message": finalStatus.LastResult,
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

// MatchMetadata links a metadata record directly to a source ID
// This is used for Library items that might not be LocalAnime (e.g. Subscriptions)
func (s *MetadataService) MatchMetadata(metadataID uint, source string, sourceID int) error {
	var m model.AnimeMetadata
	if err := db.DB.First(&m, metadataID).Error; err != nil {
		return err
	}

	switch source {
	case metadataSourceBangumi:
		m.BangumiID = sourceID
	case metadataSourceTMDB:
		m.TMDBID = sourceID
	case metadataSourceAniList:
		m.AniListID = sourceID
	}

	s.EnrichMetadata(&m, m.Title)
	db.DB.Save(&m)
	s.SyncMetadataToModels(&m)

	return nil
}
