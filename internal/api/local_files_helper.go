package api

import (
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/anilist"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/jellyfin"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

type JfEpisodeData struct {
	Id       string
	Watched  bool
	Overview string
	Rating   float64
	AirDate  string
	Duration string // Pre-formatted
}

func fetchJellyfinProgress(anime *model.LocalAnime) (map[string]JfEpisodeData, string) {
	jfMap := make(map[string]JfEpisodeData)
	var jellyfinUrl string

	if anime.ID == 0 {
		return jfMap, ""
	}

	var urlCfg, keyCfg model.GlobalConfig
	db.DB.Where("key = ?", model.ConfigKeyJellyfinUrl).First(&urlCfg)
	db.DB.Where("key = ?", model.ConfigKeyJellyfinApiKey).First(&keyCfg)
	if urlCfg.Value == "" || keyCfg.Value == "" {
		return jfMap, ""
	}

	jellyfinUrl = strings.TrimSuffix(urlCfg.Value, "/")

	client := jellyfin.NewClient(urlCfg.Value, keyCfg.Value)
	users, _ := client.GetUsers()
	if len(users) > 0 {
		client.UserID = users[0].Id
	} else {
		return jfMap, ""
	}

	var seriesId = anime.JellyfinSeriesID

	// Determine which ID to use based on preferences or availability
	// If DataSource is explicit, prioritize it
	dataSource := "jellyfin"
	if anime.Metadata != nil && anime.Metadata.DataSource != "" {
		dataSource = anime.Metadata.DataSource
	}
	log.Printf("DEBUG: AnimeID=%d Title='%s' DataSource='%s'", anime.ID, anime.Title, dataSource)

	if seriesId == "" && anime.Metadata != nil {
		// Priority: Based on DataSource or fallback
		targetProvider := SourceBangumi
		targetID := strconv.Itoa(anime.Metadata.BangumiID)

		if dataSource == "tmdb" && anime.Metadata.TMDBID != 0 {
			targetProvider = "tmdb"
			targetID = strconv.Itoa(anime.Metadata.TMDBID)
		}

		if targetID != "0" {
			sid, err := client.GetItemByProviderID(targetProvider, targetID)
			if err == nil {
				seriesId = sid
			} else if targetProvider == SourceBangumi {
				// Try lowercase "Bangumi"
				sid, err = client.GetItemByProviderID("Bangumi", targetID)
				if err == nil {
					seriesId = sid
				}
			}
		}

		// If strictly failed, try fallback
		if seriesId == "" && anime.Metadata.TMDBID != 0 {
			sid, err := client.GetItemByProviderID("tmdb", strconv.Itoa(anime.Metadata.TMDBID))
			if err == nil {
				seriesId = sid
			}
		}

		if seriesId != "" {
			anime.JellyfinSeriesID = seriesId
			db.DB.Save(&anime)
		}
	}

	if seriesId != "" {
		sEps, err := client.GetSeriesEpisodes(seriesId)
		if err == nil {
			for _, item := range sEps {
				key := fmt.Sprintf("S%dE%d", item.Season, item.Index)

				// Format Duration (Ticks -> Minutes)
				mins := item.Duration / 600000000
				durStr := ""
				if mins > 0 {
					durStr = fmt.Sprintf("%dm", mins)
				}

				// Format AirDate
				dateStr := item.AirDate
				if t, err := time.Parse(time.RFC3339, item.AirDate); err == nil {
					dateStr = t.Format("2006-01-02")
				}

				jfMap[key] = JfEpisodeData{
					Id:       item.Id,
					Watched:  item.UserData.Played,
					Overview: item.Overview,
					Rating:   item.Rating,
					AirDate:  dateStr,
					Duration: durStr,
				}
			}
		}
	}
	return jfMap, jellyfinUrl
}

func fetchBangumiProgress(anime *model.LocalAnime, effectiveSource string) (int, int) {
	bangumiWatchedCount := -1
	bangumiCollectionStatus := 0 // 0=not collected, 1=想看, 2=看过, 3=在看, 4=搁置, 5=抛弃

	if anime.Metadata != nil && effectiveSource == "bangumi" && anime.Metadata.BangumiID != 0 {
		log.Printf("DEBUG: Attempting to fetch Bangumi progress for BangumiID=%d", anime.Metadata.BangumiID)
		var bgmTokenCfg model.GlobalConfig
		db.DB.Where("key = ?", model.ConfigKeyBangumiAccessToken).First(&bgmTokenCfg)

		var appID, appSecret model.GlobalConfig
		db.DB.Where("key = ?", model.ConfigKeyBangumiAppID).First(&appID)
		db.DB.Where("key = ?", model.ConfigKeyBangumiAppSecret).First(&appSecret)

		if bgmTokenCfg.Value != "" {
			bgmClient := bangumi.NewClient(appID.Value, appSecret.Value, "")
			col, err := bgmClient.GetSubjectCollection(bgmTokenCfg.Value, "me", anime.Metadata.BangumiID)
			if err != nil {
				log.Printf("DEBUG: ❌ Failed to fetch Bangumi collection for ID %d: %v", anime.Metadata.BangumiID, err)
			} else if col == nil {
				// 404 response - user hasn't added this to collection yet
				bangumiWatchedCount = -1
				bangumiCollectionStatus = 0
				log.Printf("DEBUG: ℹ️  Bangumi ID %d not in user collection", anime.Metadata.BangumiID)
			} else {
				// Success - user has this in collection
				bangumiWatchedCount = col.EpStatus
				bangumiCollectionStatus = col.Type // Capture collection status
				log.Printf("DEBUG: ✅ Fetched Bangumi Progress for ID %d: %d eps watched, status=%d", anime.Metadata.BangumiID, bangumiWatchedCount, bangumiCollectionStatus)
				// Update Cached Progress
				if anime.Metadata.BangumiWatchedEps != col.EpStatus {
					anime.Metadata.BangumiWatchedEps = col.EpStatus
					db.DB.Save(anime.Metadata)
				}
			}
		} else {
			log.Printf("DEBUG: ⚠️  Missing Bangumi Token to fetch progress")
		}
	} else {
		if anime.Metadata == nil {
			log.Printf("DEBUG: Skipping Bangumi fetch: no metadata")
		} else if effectiveSource != "bangumi" {
			log.Printf("DEBUG: Skipping Bangumi fetch: effectiveSource is '%s', not 'bangumi'", effectiveSource)
		} else if anime.Metadata.BangumiID == 0 {
			log.Printf("DEBUG: Skipping Bangumi fetch: BangumiID is 0")
		}
	}
	return bangumiWatchedCount, bangumiCollectionStatus
}

func fetchAniListProgress(anime *model.LocalAnime, effectiveSource string) int {
	anilistWatchedCount := -1
	if anime.Metadata != nil && effectiveSource == "anilist" && anime.Metadata.AniListID != 0 {
		log.Printf("DEBUG: Attempting to fetch AniList progress for AniListID=%d", anime.Metadata.AniListID)
		var alTokenCfg model.GlobalConfig
		db.DB.Where("key = ?", model.ConfigKeyAniListToken).First(&alTokenCfg)

		if alTokenCfg.Value != "" {
			alClient := anilist.NewClient(alTokenCfg.Value, "")
			entry, err := alClient.GetMediaListEntry(anime.Metadata.AniListID)
			if err != nil {
				log.Printf("DEBUG: ❌ Failed to fetch AniList entry for ID %d: %v", anime.Metadata.AniListID, err)
			} else if entry == nil {
				// Not in user's list yet
				anilistWatchedCount = 0
				log.Printf("DEBUG: ℹ️  AniList ID %d not in user list (0 eps watched)", anime.Metadata.AniListID)
			} else {
				// Success - user has this in their list
				anilistWatchedCount = entry.Progress
				log.Printf("DEBUG: ✅ Fetched AniList Progress for ID %d: %d eps watched", anime.Metadata.AniListID, anilistWatchedCount)
				// Update Cached Progress
				if anime.Metadata.AniListWatchedEps != entry.Progress {
					anime.Metadata.AniListWatchedEps = entry.Progress
					db.DB.Save(anime.Metadata)
				}
			}
		}
	}
	return anilistWatchedCount
}

func buildEpisodeList(episodes []model.LocalEpisode, anime *model.LocalAnime, jfMap map[string]JfEpisodeData, jellyfinUrl string, bangumiWatchedCount int, anilistWatchedCount int) []EpisodeDisplay {
	var display []EpisodeDisplay
	for _, ep := range episodes {
		key := fmt.Sprintf("S%dE%d", ep.SeasonNum, ep.EpisodeNum)
		jfData := jfMap[key]

		// Update cached ID if missing
		if ep.JellyfinItemID == "" && jfData.Id != "" {
			ep.JellyfinItemID = jfData.Id
			db.DB.Model(&ep).Update("jellyfin_item_id", ep.JellyfinItemID)
		}

		thumb := ""
		// 1. Priority: TMDB Episode Image (Official Stills)
		if ep.Image != "" {
			thumb = ep.Image
		}

		// 2. Secondary: Jellyfin Episode Thumbnail
		if thumb == "" && ep.JellyfinItemID != "" && jellyfinUrl != "" {
			thumb = fmt.Sprintf("%s/Items/%s/Images/Primary?fillHeight=270&fillWidth=480&quality=90", jellyfinUrl, ep.JellyfinItemID)
		}

		// 3. Fallback: Series Poster
		if thumb == "" {
			thumb = anime.Image
			if thumb == "" && anime.Metadata != nil {
				thumb = anime.Metadata.Image
			}
		}

		// Final Step: Ensure TMDB URLs are proxied
		if strings.HasPrefix(thumb, "https://image.tmdb.org/") {
			thumb = "/api/tmdb/image?path=" + url.QueryEscape(thumb)
		}

		// Use TMDB Summary if Jellyfin is missing
		overview := jfData.Overview
		if overview == "" {
			overview = ep.Summary
		}

		// Determine Watched Status
		isWatched := jfData.Watched
		if bangumiWatchedCount >= 0 {
			if ep.EpisodeNum <= bangumiWatchedCount {
				isWatched = true
			} else {
				isWatched = false
			}
		} else if anilistWatchedCount >= 0 {
			if ep.EpisodeNum <= anilistWatchedCount {
				isWatched = true
			} else {
				isWatched = false
			}
		}

		display = append(display, EpisodeDisplay{
			ID:        ep.ID,
			Name:      filepath.Base(ep.Path),
			Path:      ep.Path,
			Size:      ep.FileSize,
			Episode:   ep.EpisodeNum,
			Season:    ep.SeasonNum,
			Playable:  true,
			Watched:   isWatched,
			Thumbnail: thumb,
			Overview:  overview,
			Rating:    jfData.Rating,
			AirDate:   jfData.AirDate,
			Duration:  jfData.Duration,
		})
	}
	return display
}
