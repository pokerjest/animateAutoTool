package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/jellyfin"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

type PlayInfoResponse struct {
	StreamURL       string              `json:"stream_url"`
	DirectStreamURL string              `json:"direct_stream_url"`
	ResumeTicks     int64               `json:"resume_ticks"`
	PosterURL       string              `json:"poster_url"`
	Title           string              `json:"title"`
	EpisodeTitle    string              `json:"episode_title"`
	Diagnostic      *PlaybackDiagnostic `json:"diagnostic,omitempty"`
}

type PlaybackDiagnostic struct {
	Code             string `json:"code"`
	Summary          string `json:"summary"`
	Hint             string `json:"hint"`
	Detail           string `json:"detail,omitempty"`
	CanUseDirectLink bool   `json:"can_use_direct_link"`
	PrimaryAction    string `json:"primary_action,omitempty"`
	PrimaryTarget    string `json:"primary_target,omitempty"`
}

func playbackError(c *gin.Context, status int, msg string, diagnostic *PlaybackDiagnostic) {
	c.JSON(status, gin.H{
		"error":      msg,
		"diagnostic": diagnostic,
	})
}

func jellyfinConfigDiagnostic(detail string) *PlaybackDiagnostic {
	return &PlaybackDiagnostic{
		Code:          "jellyfin_not_configured",
		Summary:       "Jellyfin 还没有完成配置",
		Hint:          "请先在设置页填写 Jellyfin 地址和 API Key，再回来播放。",
		Detail:        detail,
		PrimaryAction: "打开设置页",
		PrimaryTarget: "/settings",
	}
}

func jellyfinUserDiagnostic(err error) *PlaybackDiagnostic {
	switch {
	case jellyfin.HasStatus(err, http.StatusUnauthorized, http.StatusForbidden):
		return &PlaybackDiagnostic{
			Code:          "jellyfin_auth_failed",
			Summary:       "Jellyfin API Key 无效，或当前账号没有读取媒体库的权限",
			Hint:          "请在设置页重新登录 Jellyfin 或更新 API Key，然后再试一次。",
			Detail:        err.Error(),
			PrimaryAction: "检查 Jellyfin 设置",
			PrimaryTarget: "/settings",
		}
	case err != nil:
		return &PlaybackDiagnostic{
			Code:          "jellyfin_unreachable",
			Summary:       "当前无法连接到 Jellyfin 服务器",
			Hint:          "请检查 Jellyfin 地址是否正确、服务是否已启动，以及反向代理是否可达。",
			Detail:        err.Error(),
			PrimaryAction: "检查 Jellyfin 设置",
			PrimaryTarget: "/settings",
		}
	default:
		return &PlaybackDiagnostic{
			Code:          "jellyfin_no_users",
			Summary:       "Jellyfin 里没有可用于读取播放进度的用户",
			Hint:          "请确认 Jellyfin 已完成初始化，并至少存在一个可登录用户。",
			PrimaryAction: "打开设置页",
			PrimaryTarget: "/settings",
		}
	}
}

func seriesNotFoundDiagnostic(anime model.LocalAnime) *PlaybackDiagnostic {
	detail := anime.Title
	if anime.Metadata != nil {
		switch {
		case anime.Metadata.BangumiID != 0:
			detail = fmt.Sprintf("%s · Bangumi ID %d", anime.Title, anime.Metadata.BangumiID)
		case anime.Metadata.TMDBID != 0:
			detail = fmt.Sprintf("%s · TMDB ID %d", anime.Title, anime.Metadata.TMDBID)
		}
	}

	return &PlaybackDiagnostic{
		Code:          "jellyfin_series_not_found",
		Summary:       "Jellyfin 里还没有找到这部番剧",
		Hint:          "通常是媒体库还没扫描到，或元数据 ID 和 Jellyfin 中的条目对不上。可以先在 Jellyfin 里刷新资料库，再回到本地库页重试刮削或修正匹配。",
		Detail:        detail,
		PrimaryAction: "打开本地库详情",
		PrimaryTarget: fmt.Sprintf("/local-anime?highlight=%d&open=1", anime.ID),
	}
}

func episodeNotFoundDiagnostic(anime model.LocalAnime, ep model.LocalEpisode) *PlaybackDiagnostic {
	return &PlaybackDiagnostic{
		Code:          "jellyfin_episode_not_found",
		Summary:       fmt.Sprintf("Jellyfin 里还没有找到 S%dE%d", ep.SeasonNum, ep.EpisodeNum),
		Hint:          "这通常表示 Jellyfin 还没扫到这一集，或剧集号和文件解析结果不一致。可以先刷新 Jellyfin 资料库，再检查本地文件命名。",
		Detail:        ep.Path,
		PrimaryAction: "检查本地番剧详情",
		PrimaryTarget: fmt.Sprintf("/local-anime?highlight=%d&open=1&focus_episode=%d", anime.ID, ep.ID),
	}
}

func proxyPlaybackDiagnostic(detail string) *PlaybackDiagnostic {
	return &PlaybackDiagnostic{
		Code:             "jellyfin_proxy_failed",
		Summary:          "Jellyfin 代理流播放失败",
		Hint:             "可以先尝试右上角的直连播放；如果仍然失败，请检查 Jellyfin 地址、反向代理和媒体是否已入库。",
		Detail:           detail,
		CanUseDirectLink: true,
		PrimaryAction:    "检查本地番剧详情",
		PrimaryTarget:    "/local-anime",
	}
}

func missingMetadataDiagnostic(anime model.LocalAnime) *PlaybackDiagnostic {
	return &PlaybackDiagnostic{
		Code:          "missing_metadata",
		Summary:       "当前番剧还没有绑定元数据",
		Hint:          "请先在本地库详情里完成刮削或修正匹配，之后再尝试播放。",
		Detail:        anime.Title,
		PrimaryAction: "打开本地库详情",
		PrimaryTarget: fmt.Sprintf("/local-anime?highlight=%d&open=1", anime.ID),
	}
}

func localMediaMissingDiagnostic(anime model.LocalAnime, ep model.LocalEpisode) *PlaybackDiagnostic {
	return &PlaybackDiagnostic{
		Code:          "local_media_missing",
		Summary:       "对应的视频文件已经不在本地目录里",
		Hint:          "请检查下载目录、移动/重命名记录，或重新扫描本地库后再尝试播放。",
		Detail:        ep.Path,
		PrimaryAction: "打开本地番剧详情",
		PrimaryTarget: fmt.Sprintf("/local-anime?highlight=%d&open=1&focus_episode=%d", anime.ID, ep.ID),
	}
}

func resolveJellyfinPlaybackClient() (*jellyfin.Client, error) {
	var urlCfg, keyCfg model.GlobalConfig
	db.DB.Where("key = ?", model.ConfigKeyJellyfinUrl).First(&urlCfg)
	db.DB.Where("key = ?", model.ConfigKeyJellyfinApiKey).First(&keyCfg)

	if urlCfg.Value == "" || keyCfg.Value == "" {
		return nil, errors.New("missing jellyfin url or api key")
	}

	client := jellyfin.NewClient(urlCfg.Value, keyCfg.Value)
	users, err := client.GetUsers()
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
	}
	client.UserID = users[0].Id
	return client, nil
}

func resolveSeriesIDForPlayback(client *jellyfin.Client, anime *model.LocalAnime) string {
	if anime == nil || anime.Metadata == nil {
		return ""
	}

	seriesID := anime.JellyfinSeriesID
	if seriesID != "" {
		return seriesID
	}

	if anime.Metadata.BangumiID != 0 {
		sid, err := client.GetItemByProviderID("bangumi", strconv.Itoa(anime.Metadata.BangumiID))
		if err == nil {
			seriesID = sid
		} else {
			sid, err = client.GetItemByProviderID("Bangumi", strconv.Itoa(anime.Metadata.BangumiID))
			if err == nil {
				seriesID = sid
			}
		}
	}

	if seriesID == "" && anime.Metadata.TMDBID != 0 {
		sid, err := client.GetItemByProviderID("tmdb", strconv.Itoa(anime.Metadata.TMDBID))
		if err == nil {
			seriesID = sid
		}
	}

	if seriesID != "" {
		anime.JellyfinSeriesID = seriesID
		db.DB.Save(anime)
	}

	return seriesID
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
		playbackError(c, http.StatusBadRequest, "这部番剧还没有关联元数据", missingMetadataDiagnostic(anime))
		return
	}

	// 2. Refresh Jellyfin Config
	var urlCfg model.GlobalConfig
	db.DB.Where("key = ?", model.ConfigKeyJellyfinUrl).First(&urlCfg)
	var keyCfg model.GlobalConfig
	db.DB.Where("key = ?", model.ConfigKeyJellyfinApiKey).First(&keyCfg)
	if urlCfg.Value == "" || keyCfg.Value == "" {
		playbackError(c, http.StatusServiceUnavailable, "Jellyfin 暂时不可用", jellyfinConfigDiagnostic("missing jellyfin config"))
		return
	}

	client, err := resolveJellyfinPlaybackClient()
	if err != nil {
		playbackError(c, http.StatusServiceUnavailable, "Jellyfin 暂时不可用", jellyfinUserDiagnostic(err))
		return
	}
	if client == nil {
		playbackError(c, http.StatusServiceUnavailable, "Jellyfin 暂时不可用", jellyfinUserDiagnostic(nil))
		return
	}

	// 4. Resolve Series ID
	seriesId := resolveSeriesIDForPlayback(client, &anime)
	if seriesId == "" {
		playbackError(c, http.StatusNotFound, "Jellyfin 里还没有找到这部番剧", seriesNotFoundDiagnostic(anime))
		return
	}

	// 5. Resolve Episode ID
	epId := ep.JellyfinItemID
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
			playbackError(c, http.StatusNotFound, fmt.Sprintf("Jellyfin 里没有找到 S%dE%d", ep.SeasonNum, ep.EpisodeNum), episodeNotFoundDiagnostic(anime, ep))
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

	if _, err := os.Stat(ep.Path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			playbackError(c, http.StatusNotFound, "对应的视频文件已经不在本地目录里", localMediaMissingDiagnostic(anime, ep))
			return
		}
		log.Printf("[WARN] unable to stat episode file %s: %v", ep.Path, err)
	}

	c.JSON(http.StatusOK, PlayInfoResponse{
		StreamURL:       proxyUrl,
		DirectStreamURL: directUrl,
		ResumeTicks:     resume,
		PosterURL:       anime.Metadata.Image, // Use local image
		Title:           anime.Metadata.Title,
		EpisodeTitle:    fmt.Sprintf("S%dE%d - %s", ep.SeasonNum, ep.EpisodeNum, ep.Title),
		Diagnostic:      proxyPlaybackDiagnostic("代理流若黑屏或加载失败，可直接打开 Jellyfin 直连链接继续播放。"),
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
	seriesId := anime.JellyfinSeriesID

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
