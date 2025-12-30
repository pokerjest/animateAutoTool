package api

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/jellyfin"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

type PlayInfoResponse struct {
	StreamURL       string `json:"stream_url"`
	DirectStreamURL string `json:"direct_stream_url"`
	ResumeTicks     int64  `json:"resume_ticks"`
	PosterURL       string `json:"poster_url"`
	Title           string `json:"title"`
	EpisodeTitle    string `json:"episode_title"`
}

// GetPlayInfoHandler resolves a local episode to a Jellyfin stream URL
func GetPlayInfoHandler(c *gin.Context) {
	idStr := c.Param("id")
	epID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Episode ID"})
		return
	}

	// 1. Fetch Local Data
	var ep model.LocalEpisode
	if err := db.DB.First(&ep, epID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Episode not found"})
		return
	}

	var anime model.LocalAnime
	if err := db.DB.Preload("Metadata").First(&anime, ep.LocalAnimeID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Anime not found"})
		return
	}

	if anime.Metadata == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No metadata linked to this anime"})
		return
	}

	// 2. Refresh Jellyfin Config
	var urlCfg, keyCfg model.GlobalConfig
	db.DB.Where("key = ?", model.ConfigKeyJellyfinUrl).First(&urlCfg)
	db.DB.Where("key = ?", model.ConfigKeyJellyfinApiKey).First(&keyCfg)

	if urlCfg.Value == "" || keyCfg.Value == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Jellyfin not configured"})
		return
	}

	client := jellyfin.NewClient(urlCfg.Value, keyCfg.Value)

	// 3. Authenticate to get userId (needed for user data/resume)
	// We use the admin user for now or the configured one.
	// Since we set up "admin", let's assume we can Auth as admin.
	// OR: We store the AccessToken globally? client.go doesn't persist it well yet.
	// Let's Authenticate on the fly for now (overhead matches typical usage).
	// Ideally we cache this token.
	// HACK: Use "admin"/"admin" or try to reuse stored token if we had one?
	// We don't have stored user credentials easily accessible here besides checking config?
	// SetupWizard creates admin/admin. Let's try that.
	// TODO: Store proper user creds for playback.

	// For now, let's look for "JellyfinAccessToken" config if exists? No.
	// Let's try to Auth with "admin"/"admin" as fallback or check if apiKey is enough for GetItems?
	// ApiKey gives admin access usually.

	// But GetItems needs a UserID to get "UserData" (Played status).
	// We need to fetch *some* user.
	// Let's fetch Public Users? Or just use the first user found in system?
	// client.Authenticate helps but needs pwd.

	// Hack: Get All Users with ApiKey, pick the first one (usually admin).
	// Since we don't have `GetUsers` in client, let's mock authentication or assume user knew what they did.
	// Actually, `NewClient` just takes API Key.
	// We need `UserID` for UserData.
	// Let's Assume the setup process saved the UserID? Not yet.
	// Let's add `GetUsers` to client to find a valid user ID.

	// Workaround: We'll skip UserData resume for MVP if we can't find a user,
	// BUT the plan says "Sync". We MUST have a user.
	// Let's blindly try Authenticating "admin"/"admin" (default) or "admin"/random if we generated it.
	// Wait, we have `SetupWizard` which generates random password. We didn't save it to DB?
	// We did `CreateUser` with `JellyfinDefaultPassword`? No code for that visible.

	// Let's assume the user has configured the tool effectively.
	// We will query `/Users` endpoint (requires admin key) to get the first user ID.

	users, err := client.GetUsers()
	if err != nil || len(users) == 0 {
		log.Printf("Jellyfin User Fetch Failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not find any Jellyfin user"})
		return
	}
	client.UserID = users[0].Id // Use the first user found

	// 4. Resolve Series ID
	var seriesId string = anime.JellyfinSeriesID

	if seriesId == "" {
		// Priority: Bangumi ID -> TMDB ID
		if anime.Metadata.BangumiID != 0 {
			sid, err := client.GetItemByProviderID("bangumi", strconv.Itoa(anime.Metadata.BangumiID))
			if err == nil {
				seriesId = sid
			} else {
				// Try "Bangumi" (capitalized?)
				sid, err = client.GetItemByProviderID("Bangumi", strconv.Itoa(anime.Metadata.BangumiID))
				if err == nil {
					seriesId = sid
				}
			}
		}

		if seriesId == "" && anime.Metadata.TMDBID != 0 {
			sid, err := client.GetItemByProviderID("tmdb", strconv.Itoa(anime.Metadata.TMDBID))
			if err == nil {
				seriesId = sid
			}
		}

		if seriesId != "" {
			// Cache it
			anime.JellyfinSeriesID = seriesId
			db.DB.Save(&anime)
		}
	}

	if seriesId == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Could not match Series in Jellyfin"})
		return
	}

	// 5. Resolve Episode ID
	var epId string = ep.JellyfinItemID
	var resume int64 = 0

	// Always fetch from Jellyfin to get latest resume ticks, even if we have ID
	// But if we have ID, we can get UserData directly?
	// GetEpisodeFromSeries
	// ... (logic to fetch UserData and resume ticks)
	if epId != "" {
		log.Printf("[DEBUG] PlayInfo: Found cached ItemID %s", epId)
		info, err := client.GetItemInfo(epId)
		if err == nil {
			if userData, ok := info["UserData"].(map[string]interface{}); ok {
				if ticks, ok := userData["PlaybackPositionTicks"].(float64); ok {
					resume = int64(ticks)
				}
			}
		} else {
			// Cache might be invalid?
			log.Printf("[DEBUG] PlayInfo: Cache invalid for %s, refetching...", epId)
			epId = "" // Fallback to resolve again
		}
	}

	if epId == "" {
		log.Printf("[DEBUG] PlayInfo: Resolving Episode ID via Series %s S%dE%d", seriesId, ep.SeasonNum, ep.EpisodeNum)
		id, ticks, err := client.GetEpisodeFromSeries(seriesId, ep.SeasonNum, ep.EpisodeNum)
		if err != nil {
			log.Printf("[DEBUG] PlayInfo: Failed to resolve episode: %v", err)
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Episode S%dE%d not found in Jellyfin", ep.SeasonNum, ep.EpisodeNum)})
			return
		}
		epId = id
		resume = ticks

		// Cache it
		log.Printf("[DEBUG] PlayInfo: Resolved and Cached ItemID %s", epId)
		ep.JellyfinItemID = epId
		db.DB.Save(&ep)
	}

	// 6. Generate Stream URL

	// 6. Generate Stream URL (PROXIED via our backend to avoid CORS/Network issues)
	proxyUrl := fmt.Sprintf("/api/jellyfin/stream/%d", ep.ID)
	// Direct URL for fallback
	directUrl := client.GetStreamURL(epId)

	c.JSON(http.StatusOK, PlayInfoResponse{
		StreamURL:       proxyUrl,
		DirectStreamURL: directUrl,
		ResumeTicks:     resume,
		PosterURL:       anime.Metadata.Image, // Use local image
		Title:           anime.Metadata.Title,
		EpisodeTitle:    fmt.Sprintf("S%dE%d - %s", ep.SeasonNum, ep.EpisodeNum, ep.Title),
	})
}

// ReportProgressHandler receives progress updates from frontend and forwards to Jellyfin
func ReportProgressHandler(c *gin.Context) {
	var body struct {
		EpisodeID uint   `json:"episode_id"` // Local Episode ID
		Event     string `json:"event"`      // "timeupdate", "pause", "timeupdate", "ended"
		Ticks     int64  `json:"ticks"`      // Current position in ticks
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	// 1. Fetch Episode Linkage
	var ep model.LocalEpisode
	if err := db.DB.First(&ep, body.EpisodeID).Error; err != nil {
		log.Printf("[DEBUG] PlayInfo: Episode %d not found in DB", body.EpisodeID)
		c.JSON(http.StatusNotFound, gin.H{"error": "Episode not found"})
		return
	}

	// Need Metadata to find Series ID
	var anime model.LocalAnime
	if err := db.DB.Preload("Metadata").First(&anime, ep.LocalAnimeID).Error; err != nil {
		log.Printf("[DEBUG] PlayInfo: Anime for episode %d not found", body.EpisodeID)
		c.Status(http.StatusNotFound)
		return
	}

	log.Printf("[DEBUG] PlayInfo: Requesting playback for Ep %d (Order: %d, Path: %s)", ep.ID, ep.EpisodeNum, ep.Path)

	// 2. Setup Jellyfin Client
	var urlCfg, keyCfg model.GlobalConfig
	db.DB.Where("key = ?", model.ConfigKeyJellyfinUrl).First(&urlCfg)
	db.DB.Where("key = ?", model.ConfigKeyJellyfinApiKey).First(&keyCfg)
	if urlCfg.Value == "" || keyCfg.Value == "" {
		c.Status(http.StatusServiceUnavailable) // Not configured
		return
	}
	client := jellyfin.NewClient(urlCfg.Value, keyCfg.Value)

	users, _ := client.GetUsers()
	if len(users) > 0 {
		client.UserID = users[0].Id
	} else {
		c.Status(http.StatusInternalServerError)
		return
	}

	// 3. Resolve Jellyfin Item ID (Cache optimized)
	var seriesId string = anime.JellyfinSeriesID

	if seriesId == "" {
		if anime.Metadata.BangumiID != 0 {
			seriesId, _ = client.GetItemByProviderID("bangumi", strconv.Itoa(anime.Metadata.BangumiID))
			if seriesId == "" {
				seriesId, _ = client.GetItemByProviderID("Bangumi", strconv.Itoa(anime.Metadata.BangumiID))
			}
		} else if anime.Metadata.TMDBID != 0 {
			seriesId, _ = client.GetItemByProviderID("tmdb", strconv.Itoa(anime.Metadata.TMDBID))
		}

		if seriesId != "" {
			anime.JellyfinSeriesID = seriesId
			db.DB.Save(&anime)
		}
	}

	if seriesId == "" {
		c.Status(http.StatusNotFound)
		return
	}

	itemId := ep.JellyfinItemID
	if itemId == "" {
		id, _, err := client.GetEpisodeFromSeries(seriesId, ep.SeasonNum, ep.EpisodeNum)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		itemId = id
		ep.JellyfinItemID = itemId
		db.DB.Save(&ep)
	}

	// 4. Act on Event
	switch body.Event {
	case "ended":
		if err := client.MarkPlayed(itemId); err != nil {
			log.Printf("Jellyfin MarkPlayed failed: %v", err)
		}

		// Sync to Bangumi (Async)
		if anime.Metadata.BangumiID != 0 {
			go func(bgmID int, epNum int) {
				var token string
				db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyBangumiAccessToken).Select("value").Scan(&token)
				if token != "" {
					bgmClient := bangumi.NewClient("", "", "")

					// Apply Proxy Logic
					var proxyUrl, proxyEnabled string
					db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyProxyURL).Select("value").Scan(&proxyUrl)
					db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyProxyBangumi).Select("value").Scan(&proxyEnabled)

					if proxyEnabled == ValueTrue && proxyUrl != "" {
						bgmClient.SetProxy(proxyUrl)
					}

					if err := bgmClient.UpdateWatchedEpisodes(token, bgmID, epNum); err != nil {
						log.Printf("Failed to sync progress to Bangumi: %v", err)
					} else {
						log.Printf("Synced progress to Bangumi: Subject %d Ep %d", bgmID, epNum)
					}
				}
			}(anime.Metadata.BangumiID, ep.EpisodeNum)
		}
	case "pause", "destroy":
		if err := client.UpdateProgress(itemId, body.Ticks); err != nil {
			log.Printf("Jellyfin UpdateProgress failed: %v", err)
		}
	case "timeupdate":
		// Only update every X seconds/calls?
		// Frontend should debounce this.
		if err := client.UpdateProgress(itemId, body.Ticks); err != nil {
			// Verbose logging might be too much here, maybe only on debug?
			log.Printf("Jellyfin UpdateProgress failed: %v", err)
		}
	}

	c.Status(http.StatusOK)
}

// ProxyVideoHandler proxies the video stream from Jellyfin to the client
func ProxyVideoHandler(c *gin.Context) {
	idStr := c.Param("id")
	epID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	// 1. Fetch Episode
	var ep model.LocalEpisode
	if err := db.DB.First(&ep, epID).Error; err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	if ep.JellyfinItemID == "" {
		c.Status(http.StatusNotFound) // Should have been resolved by PlayInfo
		return
	}

	// 2. Setup Jellyfin Client (Need URL and Key)
	var urlCfg, keyCfg model.GlobalConfig
	db.DB.Where("key = ?", model.ConfigKeyJellyfinUrl).First(&urlCfg)
	db.DB.Where("key = ?", model.ConfigKeyJellyfinApiKey).First(&keyCfg)
	if urlCfg.Value == "" || keyCfg.Value == "" {
		c.Status(http.StatusServiceUnavailable)
		return
	}

	// 3. Construct Reverse Proxy
	target, err := url.Parse(urlCfg.Value)
	if err != nil {
		log.Printf("[Proxy] Invalid Jellyfin URL: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = 100 * time.Millisecond // Optimize for streaming

	// Define the director to rewrite the request
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)

		// Rewrite Path: /Videos/{ItemId}/stream
		req.URL.Path = fmt.Sprintf("/Videos/%s/stream", ep.JellyfinItemID)

		// Set Query Params
		q := req.URL.Query()
		q.Set("static", "true")
		q.Set("api_key", keyCfg.Value)
		req.URL.RawQuery = q.Encode()

		// Update Host Header to match target
		req.Host = target.Host

		// Clear headers that might confuse Jellyfin
		req.Header.Del("Cookie")
		req.Header.Del("Authorization")
		req.Header.Del("Referer")
		req.Header.Del("Origin")
	}

	// Error Handler to suppress client disconnect noise
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if err != nil && err != http.ErrAbortHandler {
			log.Printf("[Proxy] Error proxying video: %v", err)
			// Only write status if headers haven't been written
			w.WriteHeader(http.StatusBadGateway)
		}
	}

	// Safe ServeHTTP to catch http.ErrAbortHandler if propagated as panic
	defer func() {
		if err := recover(); err != nil {
			if err != http.ErrAbortHandler {
				// Re-panic if it's not the abort handler
				panic(err)
			}
			// Ignore AbortHandler panic
		}
	}()

	proxy.ServeHTTP(c.Writer, c.Request)
}
