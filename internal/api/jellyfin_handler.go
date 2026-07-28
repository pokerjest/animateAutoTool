package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/httpx"
	"github.com/pokerjest/animateAutoTool/internal/jellyfin"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/store"
)

type PlayInfoResponse struct {
	StreamURL        string              `json:"stream_url"`
	DirectStreamURL  string              `json:"direct_stream_url"`
	NetBirdStreamURL string              `json:"netbird_stream_url"`
	ResumeTicks      int64               `json:"resume_ticks"`
	RuntimeTicks     int64               `json:"runtime_ticks"`
	Played           bool                `json:"played"`
	EpisodeFavorite  bool                `json:"episode_favorite"`
	SeriesFavorite   bool                `json:"series_favorite"`
	Media            JellyfinMediaInfo   `json:"media"`
	PosterURL        string              `json:"poster_url"`
	Title            string              `json:"title"`
	EpisodeTitle     string              `json:"episode_title"`
	Diagnostic       *PlaybackDiagnostic `json:"diagnostic,omitempty"`
}

type JellyfinMediaInfo struct {
	Container     string `json:"container"`
	Size          int64  `json:"size"`
	Bitrate       int64  `json:"bitrate"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	VideoCodec    string `json:"video_codec"`
	AudioCodec    string `json:"audio_codec"`
	AudioChannels int    `json:"audio_channels"`
	SubtitleCount int    `json:"subtitle_count"`
}

type jellyfinUserStateInput struct {
	Played   *bool `json:"played"`
	Favorite *bool `json:"favorite"`
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

func proxyPlaybackDiagnostic(detail string, canUseDirectLink bool) *PlaybackDiagnostic {
	return &PlaybackDiagnostic{
		Code:             "jellyfin_proxy_failed",
		Summary:          "Jellyfin 代理流播放失败",
		Hint:             "请检查 Jellyfin 地址、Tailscale 连接、反向代理和媒体是否已入库。",
		Detail:           detail,
		CanUseDirectLink: canUseDirectLink,
		PrimaryAction:    "检查本地番剧详情",
		PrimaryTarget:    "/local-anime",
	}
}

func normalizeJellyfinBaseURL(value string) (string, error) {
	return normalizePlaybackBaseURL(value, "jellyfin 浏览器直连地址")
}

func normalizeNetBirdProxyBaseURL(value string) (string, error) {
	return normalizePlaybackBaseURL(value, "netbird 代理地址")
}

func normalizePlaybackBaseURL(value, fieldName string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" || (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
		return "", fmt.Errorf("%s必须是完整的 http:// 或 https:// 地址", fieldName)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%s不能包含账号、查询参数或片段", fieldName)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return strings.TrimRight(parsed.String(), "/"), nil
}

func netBirdStreamURL(c *gin.Context, episodeID uint) string {
	baseURL, err := normalizeNetBirdProxyBaseURL(configValue(model.ConfigKeyNetBirdProxyURL))
	if err != nil || baseURL == "" {
		if err != nil {
			log.Printf("[WARN] ignoring invalid NetBird proxy URL: %v", err)
		}
		return ""
	}
	userID, err := currentSessionUserID(c)
	if err != nil {
		return ""
	}
	token, err := signNetBirdStreamToken(episodeID, userID, time.Now().Add(netBirdStreamTokenTTL))
	if err != nil {
		log.Printf("[WARN] unable to create NetBird stream token: %v", err)
		return ""
	}
	return fmt.Sprintf("%s/api/v1/netbird/jellyfin/stream/%d?token=%s", baseURL, episodeID, url.QueryEscape(token))
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
	urlValue := configValue(model.ConfigKeyJellyfinUrl)
	apiKey := configValue(model.ConfigKeyJellyfinApiKey)
	if urlValue == "" || apiKey == "" {
		return nil, errors.New("missing jellyfin url or api key")
	}

	client := newConfiguredJellyfinClient(urlValue, apiKey)
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
		if err := localAnimeStore().SaveAnime(anime); err != nil {
			log.Printf("WARN: cache Jellyfin series id for anime %d failed: %v", anime.ID, err)
		}
	}

	return seriesID
}

func ensureJellyfinEpisodeID(client *jellyfin.Client, anime *model.LocalAnime, ep *model.LocalEpisode) (string, error) {
	if client == nil || anime == nil || ep == nil {
		return "", errors.New("missing jellyfin episode context")
	}
	if strings.TrimSpace(ep.JellyfinItemID) != "" {
		return ep.JellyfinItemID, nil
	}
	seriesID := resolveSeriesIDForPlayback(client, anime)
	if seriesID == "" {
		return "", errors.New("jellyfin series not found")
	}
	itemID, _, err := client.GetEpisodeFromSeries(seriesID, ep.SeasonNum, ep.EpisodeNum)
	if err != nil {
		return "", err
	}
	ep.JellyfinItemID = itemID
	if err := localAnimeStore().SaveEpisode(ep); err != nil {
		log.Printf("WARN: cache Jellyfin item id for episode %d failed: %v", ep.ID, err)
	}
	return itemID, nil
}

func jellyfinMediaInfo(details *jellyfin.ItemDetails) JellyfinMediaInfo {
	if details == nil || len(details.MediaSources) == 0 {
		return JellyfinMediaInfo{}
	}
	source := details.MediaSources[0]
	result := JellyfinMediaInfo{Container: source.Container, Size: source.Size, Bitrate: source.Bitrate}
	for _, stream := range source.MediaStreams {
		switch strings.ToLower(stream.Type) {
		case "video":
			if result.VideoCodec == "" {
				result.VideoCodec = stream.Codec
				result.Width = stream.Width
				result.Height = stream.Height
				if result.Bitrate == 0 {
					result.Bitrate = stream.BitRate
				}
			}
		case "audio":
			if result.AudioCodec == "" {
				result.AudioCodec = stream.Codec
				result.AudioChannels = stream.Channels
			}
		case "subtitle":
			result.SubtitleCount++
		}
	}
	return result
}

// GetPlayInfoHandler resolves a local episode to a Jellyfin stream URL
func GetPlayInfoHandler(c *gin.Context) {
	idStr := c.Param("id")
	epID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		jsonBadRequest(c, "剧集 ID 无效")
		return
	}

	// 1. Fetch Local Data
	var ep model.LocalEpisode
	if err := db.DB.First(&ep, epID).Error; err != nil {
		jsonNotFound(c, "未找到对应的剧集")
		return
	}

	var anime model.LocalAnime
	if err := db.DB.Preload("Metadata").First(&anime, ep.LocalAnimeID).Error; err != nil {
		jsonNotFound(c, "未找到对应的番剧")
		return
	}

	if anime.Metadata == nil {
		playbackError(c, http.StatusBadRequest, "这部番剧还没有关联元数据", missingMetadataDiagnostic(anime))
		return
	}

	// 2. Refresh Jellyfin Config
	urlValue := configValue(model.ConfigKeyJellyfinUrl)
	apiKey := configValue(model.ConfigKeyJellyfinApiKey)
	if urlValue == "" || apiKey == "" {
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
	var details *jellyfin.ItemDetails
	seriesFavorite := false
	if seriesDetails, detailsErr := client.GetItemDetails(seriesId); detailsErr == nil {
		seriesFavorite = seriesDetails.UserData.IsFavorite
	}

	// Always fetch from Jellyfin to get latest resume ticks, even if we have ID
	// But if we have ID, we can get UserData directly?
	// GetEpisodeFromSeries
	// ... (logic to fetch UserData and resume ticks)
	if epId != "" {
		log.Printf("[DEBUG] PlayInfo: Found cached ItemID %s", epId)
		info, err := client.GetItemDetails(epId)
		if err == nil {
			details = info
			resume = info.UserData.PlaybackPositionTicks
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
		if err := localAnimeStore().SaveEpisode(&ep); err != nil {
			log.Printf("WARN: cache Jellyfin item id for episode %d failed: %v", ep.ID, err)
		}
		if info, detailsErr := client.GetItemDetails(epId); detailsErr == nil {
			details = info
			resume = info.UserData.PlaybackPositionTicks
		}
	}

	// 6. Generate Stream URL

	// 6. Generate Stream URL (PROXIED via our backend to avoid CORS/Network issues)
	proxyUrl := fmt.Sprintf("/api/v1/jellyfin/stream/%d", ep.ID)
	// Direct URL is advertised separately because the address reachable by the
	// browser (for example a Tailscale MagicDNS name) can differ from the URL
	// used by AnimateTool on the server.
	directUrl := ""
	if directBase, directErr := normalizeJellyfinBaseURL(configValue(model.ConfigKeyJellyfinDirectUrl)); directErr == nil && directBase != "" {
		directUrl = jellyfin.NewClient(directBase, apiKey).GetStreamURL(epId)
	} else if directErr != nil {
		log.Printf("[WARN] ignoring invalid Jellyfin direct URL: %v", directErr)
	}
	netBirdURL := netBirdStreamURL(c, ep.ID)

	if _, err := os.Stat(ep.Path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			playbackError(c, http.StatusNotFound, "对应的视频文件已经不在本地目录里", localMediaMissingDiagnostic(anime, ep))
			return
		}
		log.Printf("[WARN] unable to stat episode file %s: %v", ep.Path, err)
	}
	runtimeTicks := int64(0)
	played := false
	episodeFavorite := false
	if details != nil {
		runtimeTicks = details.RunTimeTicks
		played = details.UserData.Played
		episodeFavorite = details.UserData.IsFavorite
	}
	// AnimateTool's per-user record takes precedence over the configured
	// Jellyfin user's shared resume point. Jellyfin remains the fallback for
	// users upgrading from versions without local playback history.
	if userID, userErr := currentSessionUserID(c); userErr == nil {
		if history, historyErr := store.NewPlaybackHistoryStore(db.DB).Find(userID, ep.ID); historyErr == nil {
			if history.Completed {
				resume = 0
			} else {
				resume = history.PositionTicks
			}
			if history.DurationTicks > 0 {
				runtimeTicks = history.DurationTicks
			}
		}
	}

	c.JSON(http.StatusOK, PlayInfoResponse{
		StreamURL:        proxyUrl,
		DirectStreamURL:  directUrl,
		NetBirdStreamURL: netBirdURL,
		ResumeTicks:      resume,
		RuntimeTicks:     runtimeTicks,
		Played:           played,
		EpisodeFavorite:  episodeFavorite,
		SeriesFavorite:   seriesFavorite,
		Media:            jellyfinMediaInfo(details),
		PosterURL:        anime.Metadata.Image, // Use local image
		Title:            anime.Metadata.Title,
		EpisodeTitle:     fmt.Sprintf("S%dE%d - %s", ep.SeasonNum, ep.EpisodeNum, ep.Title),
		Diagnostic:       proxyPlaybackDiagnostic("NetBird 或 Jellyfin 直连失败时，播放器会自动切换到 AnimateTool 代理流。", directUrl != "" || netBirdURL != ""),
	})
}

// UpdateJellyfinEpisodeStateHandler updates watched/favorite state for a local
// episode without exposing Jellyfin item identifiers to the browser.
func UpdateJellyfinEpisodeStateHandler(c *gin.Context) {
	var input jellyfinUserStateInput
	if err := c.ShouldBindJSON(&input); err != nil || (input.Played == nil && input.Favorite == nil) {
		v1Error(c, http.StatusBadRequest, "invalid_jellyfin_state", "至少需要提交已观看或收藏状态")
		return
	}
	var ep model.LocalEpisode
	if err := db.DB.First(&ep, c.Param("id")).Error; err != nil {
		v1Error(c, http.StatusNotFound, "episode_not_found", "未找到对应的剧集")
		return
	}
	var anime model.LocalAnime
	if err := db.DB.Preload("Metadata").First(&anime, ep.LocalAnimeID).Error; err != nil {
		v1Error(c, http.StatusNotFound, "anime_not_found", "未找到对应的番剧")
		return
	}
	client, err := resolveJellyfinPlaybackClient()
	if err != nil || client == nil {
		v1Error(c, http.StatusServiceUnavailable, "jellyfin_unavailable", "Jellyfin 暂时不可用")
		return
	}
	itemID, err := ensureJellyfinEpisodeID(client, &anime, &ep)
	if err != nil {
		v1Error(c, http.StatusNotFound, "jellyfin_episode_not_found", "Jellyfin 中未找到对应剧集")
		return
	}
	if input.Played != nil {
		if *input.Played {
			err = client.MarkPlayed(itemID)
		} else {
			err = client.UnmarkPlayed(itemID)
		}
		if err != nil {
			v1Error(c, http.StatusBadGateway, "jellyfin_state_failed", "更新 Jellyfin 观看状态失败")
			return
		}
	}
	if input.Favorite != nil {
		if err := client.SetFavorite(itemID, *input.Favorite); err != nil {
			v1Error(c, http.StatusBadGateway, "jellyfin_favorite_failed", "更新 Jellyfin 收藏状态失败")
			return
		}
	}
	details, _ := client.GetItemDetails(itemID)
	played := input.Played != nil && *input.Played
	favorite := input.Favorite != nil && *input.Favorite
	if details != nil {
		played = details.UserData.Played
		favorite = details.UserData.IsFavorite
	}
	v1Data(c, http.StatusOK, gin.H{"played": played, "favorite": favorite})
}

// UpdateJellyfinSeriesStateHandler updates the favorite state for a local
// anime's Jellyfin series.
func UpdateJellyfinSeriesStateHandler(c *gin.Context) {
	var input jellyfinUserStateInput
	if err := c.ShouldBindJSON(&input); err != nil || input.Favorite == nil {
		v1Error(c, http.StatusBadRequest, "invalid_jellyfin_state", "需要提交收藏状态")
		return
	}
	anime, err := localAnimeStore().GetWithMetadata(c.Param("id"))
	if err != nil {
		v1Error(c, http.StatusNotFound, "anime_not_found", "未找到对应的本地番剧")
		return
	}
	client, err := resolveJellyfinPlaybackClient()
	if err != nil || client == nil {
		v1Error(c, http.StatusServiceUnavailable, "jellyfin_unavailable", "Jellyfin 暂时不可用")
		return
	}
	seriesID := resolveSeriesIDForPlayback(client, anime)
	if seriesID == "" {
		v1Error(c, http.StatusNotFound, "jellyfin_series_not_found", "Jellyfin 中未找到对应番剧")
		return
	}
	if err := client.SetFavorite(seriesID, *input.Favorite); err != nil {
		v1Error(c, http.StatusBadGateway, "jellyfin_favorite_failed", "更新 Jellyfin 收藏状态失败")
		return
	}
	v1Data(c, http.StatusOK, gin.H{"favorite": *input.Favorite})
}

// ProxyVideoHandler proxies the video stream from Jellyfin to the client
func ProxyVideoHandler(c *gin.Context) {
	idStr := c.Param("id")
	epID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	proxyVideoForEpisode(c, uint(epID))
}

func proxyVideoForEpisode(c *gin.Context, episodeID uint) {
	// 1. Fetch Episode
	var ep model.LocalEpisode
	if err := db.DB.First(&ep, episodeID).Error; err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	if ep.JellyfinItemID == "" {
		c.Status(http.StatusNotFound) // Should have been resolved by PlayInfo
		return
	}
	proxyVideoForJellyfinItem(c, ep.JellyfinItemID)
}

func proxyVideoForJellyfinItem(c *gin.Context, itemID string) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		c.Status(http.StatusNotFound)
		return
	}

	// 2. Setup Jellyfin Client (Need URL and Key)
	urlValue := configValue(model.ConfigKeyJellyfinUrl)
	apiKey := configValue(model.ConfigKeyJellyfinApiKey)
	if urlValue == "" || apiKey == "" {
		c.Status(http.StatusServiceUnavailable)
		return
	}

	// 3. Construct Reverse Proxy
	target, err := url.Parse(urlValue)
	if err != nil {
		log.Printf("[Proxy] Invalid Jellyfin URL: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = httpx.NewHTTPClientWithProxy(0, configuredProxyURL(model.ConfigKeyProxyJellyfin)).Transport
	proxy.FlushInterval = 100 * time.Millisecond // Optimize for streaming

	// Define the director to rewrite the request
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)

		// Rewrite Path: /Videos/{ItemId}/stream
		req.URL.Path = fmt.Sprintf("/Videos/%s/stream", itemID)

		// Set Query Params
		q := req.URL.Query()
		q.Del("token")
		q.Set("static", "true")
		q.Set("api_key", apiKey)
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
		if err == nil || isExpectedVideoProxyCancellation(r, err) {
			return
		}
		log.Printf("[Proxy] Error proxying video: %v", err)
		// Only write status if headers haven't been written
		w.WriteHeader(http.StatusBadGateway)
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

func isExpectedVideoProxyCancellation(r *http.Request, err error) bool {
	if errors.Is(err, http.ErrAbortHandler) || errors.Is(err, context.Canceled) {
		return true
	}
	return r != nil && errors.Is(r.Context().Err(), context.Canceled)
}
