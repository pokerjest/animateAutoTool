package service

import (
	"log"

	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/tmdb"
)

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

	laStore := localAnimeStore()
	if laStore == nil {
		return
	}
	episodes, err := laStore.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		log.Printf("Align: failed to list episodes for %s: %v", anime.Title, err)
		return
	}

	for i := range episodes {
		ep := &episodes[i]

		shouldAlign := false
		if ep.SeasonNum <= 1 {
			shouldAlign = true
		} else {
			exists := false
			maxEp := 0
			for _, season := range show.Seasons {
				if season.SeasonNumber == ep.SeasonNum {
					exists = true
					maxEp = season.EpisodeCount
					break
				}
			}
			if !exists || ep.EpisodeNum > maxEp {
				shouldAlign = true
			}
		}

		if shouldAlign {
			targetAbs := ep.EpisodeNum
			for _, currentRange := range ranges {
				if targetAbs >= currentRange.Start && targetAbs <= currentRange.End {
					if currentRange.SeasonNum != ep.SeasonNum || (currentRange.SeasonNum == ep.SeasonNum && ep.EpisodeNum != (targetAbs-currentRange.Start+1)) {
						newEpNum := targetAbs - currentRange.Start + 1
						log.Printf("Align: %s - S%dE%d -> S%dE%d (Abs %d)", anime.Title, ep.SeasonNum, ep.EpisodeNum, currentRange.SeasonNum, newEpNum, targetAbs)
						ep.SeasonNum = currentRange.SeasonNum
						ep.EpisodeNum = newEpNum
						if err := laStore.SaveEpisode(ep); err != nil {
							log.Printf("Align: failed to save episode alignment for %s S%dE%d: %v", anime.Title, currentRange.SeasonNum, newEpNum, err)
						}
					}
					break
				}
			}
		}
	}

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

	laStore := localAnimeStore()
	if laStore == nil {
		return
	}
	localEps, err := laStore.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		log.Printf("MetadataService: list episodes failed for %s: %v", anime.Title, err)
		return
	}
	if len(localEps) == 0 {
		log.Printf("MetadataService: No local episodes found for %s", anime.Title)
		return
	}

	seasons := make(map[int]bool)
	for _, ep := range localEps {
		seasons[ep.SeasonNum] = true
	}

	log.Printf("MetadataService: Found %d local episodes in seasons %v for %s", len(localEps), seasons, anime.Title)

	for seasonNum := range seasons {
		log.Printf("MetadataService: Fetching TMDB Season %d for %s", seasonNum, anime.Title)
		season, err := performWithRetry(func() (*tmdb.SeasonDetails, error) {
			return tmdbClient.GetSeasonDetails(anime.Metadata.TMDBID, seasonNum)
		})
		if err != nil || season == nil {
			log.Printf("MetadataService: Failed to fetch TMDB Season %d for %s: %v", seasonNum, anime.Title, err)
			continue
		}

		tmdbMap := make(map[int]tmdb.Episode)
		for _, ep := range season.Episodes {
			tmdbMap[ep.EpisodeNumber] = ep
		}

		log.Printf("MetadataService: TMDB Season %d has %d episodes for %s", seasonNum, len(season.Episodes), anime.Title)

		for i := range localEps {
			lep := &localEps[i]
			if lep.SeasonNum != seasonNum {
				continue
			}
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
					if err := laStore.SaveEpisode(lep); err != nil {
						log.Printf("MetadataService: failed to save TMDB episode metadata for %s S%dE%d: %v", anime.Title, lep.SeasonNum, lep.EpisodeNum, err)
					}
				}
			} else {
				log.Printf("MetadataService: No TMDB match for S%dE%d for %s", seasonNum, lep.EpisodeNum, anime.Title)
			}
		}
	}
}
