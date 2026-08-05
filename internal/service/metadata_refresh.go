package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/taskstate"
)

// StartMetadataMigration starts the compatibility background wrapper.
func (s *MetadataService) StartMetadataMigration() {
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf(
					"ERROR: MetadataService: background image migration panic recovery_action=retry_next_start panic=%v\n%s",
					recovered,
					debug.Stack(),
				)
			}
		}()
		s.RunMetadataMigration(context.Background())
	}()
}

// RunMetadataMigration caches missing metadata images and returns after ctx is
// canceled. Startup runs this form as a tracked worker during shutdown.
func (s *MetadataService) RunMetadataMigration(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !waitForMetadataMigration(ctx, 5*time.Second) {
		return
	}
	log.Println("Migration: Starting background metadata image migration...")
	mStore := metadataStore()
	if mStore == nil {
		return
	}
	list, err := mStore.ListWithImageRawMissing()
	if err != nil {
		log.Printf("Migration: failed to list metadata for image caching: %v", err)
		return
	}

	log.Printf("Migration: Found %d records needing image caching", len(list))

	for _, m := range list {
		if ctx.Err() != nil {
			return
		}
		updated := false
		if m.BangumiImage != "" && len(m.BangumiImageRaw) == 0 {
			m.BangumiImageRaw = s.fetchAndCacheImage(m.BangumiImage, model.ConfigKeyProxyBangumi)
			updated = true
		}
		if ctx.Err() != nil {
			return
		}
		if m.TMDBImage != "" && len(m.TMDBImageRaw) == 0 {
			m.TMDBImageRaw = s.fetchAndCacheImage(m.TMDBImage, model.ConfigKeyProxyTMDB)
			updated = true
		}
		if ctx.Err() != nil {
			return
		}
		if m.AniListImage != "" && len(m.AniListImageRaw) == 0 {
			m.AniListImageRaw = s.fetchAndCacheImage(m.AniListImage, model.ConfigKeyProxyAniList)
			updated = true
		}

		if updated {
			m.Image = fmt.Sprintf("/api/v1/posters/%d", m.ID)
			if err := mStore.Save(&m); err != nil {
				log.Printf("ERROR: MetadataService: image cache persist failed metadata_id=%d error=%v", m.ID, err)
			} else {
				s.SyncMetadataToModels(&m)
			}
		}
		if !waitForMetadataMigration(ctx, time.Second) {
			return
		}
	}
	log.Println("Migration: Background image migration completed.")
}

func waitForMetadataMigration(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// RegenerateAllNFOs triggers NFO generation for ALL local animes.
func (s *MetadataService) RegenerateAllNFOs() (int, error) {
	return s.RegenerateAllNFOsContext(context.Background())
}

func (s *MetadataService) RegenerateAllNFOsContext(ctx context.Context) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	laStore := localAnimeStore()
	if laStore == nil {
		return 0, nil
	}
	list, err := laStore.ListWithMetadataAndEpisodes()
	if err != nil {
		return 0, err
	}

	count := 0
	nfoGen := NewNFOGeneratorService()
	for _, anime := range list {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		if anime.Metadata == nil {
			continue
		}
		if _, err := os.Stat(anime.Path); os.IsNotExist(err) {
			continue
		}

		_ = nfoGen.SaveLocalImages(&anime)
		_ = nfoGen.GenerateTVShowNFO(&anime)
		for _, ep := range anime.Episodes {
			if err := ctx.Err(); err != nil {
				return count, err
			}
			_ = nfoGen.GenerateEpisodeNFO(&ep, &anime)
		}
		count++
	}
	return count, nil
}

// MatchSeriesSources links a local series to one or more real provider IDs.
// Zero IDs mean "leave the existing value unchanged", which preserves the
// legacy single-source matching behavior while allowing a confirmed tri-source
// proposal to apply all IDs atomically through the same enrichment pipeline.
func (s *MetadataService) MatchSeriesSources(animeID uint, bangumiID, tmdbID, aniListID int) error {
	laStore := localAnimeStore()
	if laStore == nil {
		return nil
	}
	anime, err := laStore.GetWithMetadata(animeID)
	if err != nil {
		return err
	}

	m := anime.Metadata
	if m == nil {
		m = &model.AnimeMetadata{}
	}

	if bangumiID > 0 {
		m.BangumiID = bangumiID
	}
	if tmdbID > 0 {
		m.TMDBID = tmdbID
	}
	if aniListID > 0 {
		m.AniListID = aniListID
	}
	if m.BangumiID == 0 && m.TMDBID == 0 && m.AniListID == 0 {
		return fmt.Errorf("至少需要一个有效的元数据来源 ID")
	}

	s.EnrichMetadata(m, anime.Title)

	anime.Metadata = m
	anime.MetadataID = &m.ID
	anime.Image = m.Image
	anime.Summary = m.Summary
	if err := laStore.SaveAnime(anime); err != nil {
		log.Printf("MatchSeries: save anime failed: %v", err)
	}

	if m.TMDBID != 0 {
		s.SyncEpisodesWithTMDB(anime)
		s.AlignEpisodesWithTMDB(anime)
	}
	nfoGen := NewNFOGeneratorService()
	_ = nfoGen.SaveLocalImages(anime)
	_ = nfoGen.GenerateTVShowNFO(anime)
	_ = ResolveLibraryIssue("scrape:" + strconv.FormatUint(uint64(anime.ID), 10))

	return nil
}

// MatchSeries manually links a series to a specific metadata record from a
// source. It remains as a compatibility wrapper for older clients.
func (s *MetadataService) MatchSeries(animeID uint, source string, sourceID int) error {
	switch source {
	case metadataSourceBangumi:
		return s.MatchSeriesSources(animeID, sourceID, 0, 0)
	case metadataSourceTMDB:
		return s.MatchSeriesSources(animeID, 0, sourceID, 0)
	case metadataSourceAniList:
		return s.MatchSeriesSources(animeID, 0, 0, sourceID)
	default:
		return fmt.Errorf("不支持的元数据来源 %q", source)
	}
}

// BeginRefreshAllMetadata reserves the global refresh slot without creating a
// goroutine. API handlers use it before registering the work with app lifecycle.
func (s *MetadataService) BeginRefreshAllMetadata() bool {
	if !GlobalRefreshStatus.TryStart() {
		return false
	}
	taskstate.Global.Start("metadata-refresh", "metadata", "刷新全部元数据", "正在刷新番剧元数据")
	return true
}

// StartRefreshAllMetadata keeps the compatibility wrapper for non-server callers.
func (s *MetadataService) StartRefreshAllMetadata(force bool) bool {
	if !s.BeginRefreshAllMetadata() {
		return false
	}
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				err := fmt.Errorf("metadata refresh panic: %v", recovered)
				GlobalRefreshStatus.Finish("元数据刷新异常中止")
				taskstate.Global.Fail("metadata-refresh", err)
				log.Printf(
					"ERROR: MetadataService: refresh panic recovery_action=retry_refresh panic=%v\n%s",
					recovered,
					debug.Stack(),
				)
			}
		}()
		s.RefreshAllMetadataContext(context.Background(), force)
	}()
	return true
}

// RefreshAllMetadata updates metadata records.
func (s *MetadataService) RefreshAllMetadata(force bool) int {
	return s.RefreshAllMetadataContext(context.Background(), force)
}

func (s *MetadataService) RefreshAllMetadataContext(ctx context.Context, force bool) int {
	if ctx == nil {
		ctx = context.Background()
	}
	log.Printf("Refresh: Starting metadata refresh (force=%v)...", force)
	mStore := metadataStore()
	if mStore == nil {
		GlobalRefreshStatus.Finish("数据库未就绪")
		taskstate.Global.Fail("metadata-refresh", fmt.Errorf("数据库未就绪"))
		return 0
	}
	allList, err := listAllMetadata()
	if err != nil {
		log.Printf("Refresh: failed to list metadata: %v", err)
		GlobalRefreshStatus.Finish("加载元数据失败")
		taskstate.Global.Fail("metadata-refresh", err)
		return 0
	}

	var list []model.AnimeMetadata
	if force {
		list = allList
	} else {
		for _, m := range allList {
			if !metadataSourcesConflict(&m) && m.Summary != "" && (m.BangumiID != 0 || m.TMDBID != 0 || m.AniListID != 0) {
				continue
			}
			list = append(list, m)
		}
	}

	total := len(list)
	GlobalRefreshStatus.SetTotal(total)

	if total == 0 {
		GlobalRefreshStatus.Finish("已是最新")
		taskstate.Global.Complete("metadata-refresh", "元数据已是最新")
		return 0
	}

	updatedCount := 0
	var updateMu sync.Mutex
	maxWorkers := 5
	guard := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	refreshCanceled := false
refreshLoop:
	for i, m := range list {
		select {
		case guard <- struct{}{}:
		case <-ctx.Done():
			refreshCanceled = true
			break refreshLoop
		}
		wg.Add(1)
		go func(idx int, meta model.AnimeMetadata) {
			defer wg.Done()
			defer func() { <-guard }()
			defer func() {
				if recovered := recover(); recovered != nil {
					log.Printf(
						"ERROR: MetadataService: refresh worker panic metadata_id=%d index=%d recovery_action=continue_other_records panic=%v\n%s",
						meta.ID,
						idx,
						recovered,
						debug.Stack(),
					)
				}
			}()
			if ctx.Err() != nil {
				return
			}

			GlobalRefreshStatus.UpdateProgress(idx+1, meta.Title)
			taskstate.Global.Progress("metadata-refresh", "正在刷新 "+meta.Title, int64(idx+1), int64(total))
			event.GlobalBus.Publish(event.EventMetadataUpdated, map[string]interface{}{
				"type": "progress", "current": idx + 1, "total": total, "title": meta.Title,
			})

			if freshM, err := mStore.GetByID(meta.ID); err == nil && ctx.Err() == nil {
				s.EnrichMetadata(freshM, metadataRefreshQuery(freshM))
				updateMu.Lock()
				updatedCount++
				updateMu.Unlock()
			}
			timer := time.NewTimer(500 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
			}
		}(i, m)
	}
	wg.Wait()
	if ctx.Err() != nil {
		refreshCanceled = true
	}
	if refreshCanceled {
		GlobalRefreshStatus.Finish("元数据刷新已取消")
		taskstate.Global.Fail("metadata-refresh", ctx.Err())
		return updatedCount
	}
	finalStatus := GlobalRefreshStatus.Finish(fmt.Sprintf("已更新 %d 条", updatedCount))

	event.GlobalBus.Publish(event.EventMetadataUpdated, map[string]interface{}{
		"type":    "complete",
		"message": finalStatus.LastResult,
	})
	taskstate.Global.Complete("metadata-refresh", finalStatus.LastResult)

	return updatedCount
}

// RefreshSingleMetadata forces a refresh of a single metadata record
func (s *MetadataService) RefreshSingleMetadata(id uint) error {
	mStore := metadataStore()
	if mStore == nil {
		return nil
	}
	m, err := mStore.GetByID(id)
	if err != nil {
		return err
	}
	s.EnrichMetadata(m, metadataRefreshQuery(m))
	return nil
}

// MatchMetadata links a metadata record directly to a source ID
// This is used for Library items that might not be LocalAnime (e.g. Subscriptions)
func (s *MetadataService) MatchMetadata(metadataID uint, source string, sourceID int) error {
	switch source {
	case metadataSourceBangumi:
		return s.MatchMetadataSources(metadataID, sourceID, 0, 0)
	case metadataSourceTMDB:
		return s.MatchMetadataSources(metadataID, 0, sourceID, 0)
	case metadataSourceAniList:
		return s.MatchMetadataSources(metadataID, 0, 0, sourceID)
	default:
		return fmt.Errorf("不支持的元数据来源 %q", source)
	}
}

// MatchMetadataSources links a library metadata row to one or more provider
// IDs. It shares the same enrichment and synchronization logic as the legacy
// single-source method.
func (s *MetadataService) MatchMetadataSources(metadataID uint, bangumiID, tmdbID, aniListID int) error {
	mStore := metadataStore()
	if mStore == nil {
		return nil
	}
	m, err := mStore.GetByID(metadataID)
	if err != nil {
		return err
	}

	if bangumiID > 0 {
		m.BangumiID = bangumiID
	}
	if tmdbID > 0 {
		m.TMDBID = tmdbID
	}
	if aniListID > 0 {
		m.AniListID = aniListID
	}
	if m.BangumiID == 0 && m.TMDBID == 0 && m.AniListID == 0 {
		return fmt.Errorf("至少需要一个有效的元数据来源 ID")
	}

	s.EnrichMetadata(m, m.Title)
	if err := mStore.Save(m); err != nil {
		log.Printf("MatchMetadata: save metadata failed: %v", err)
	}
	s.SyncMetadataToModels(m)

	return nil
}
