package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	mediaprovider "github.com/pokerjest/animateAutoTool/internal/media"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

type mediaLibraryResponse struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	CollectionType string `json:"collection_type"`
	ItemCount      int    `json:"item_count"`
	Selected       bool   `json:"selected"`
}

type mediaItemResponse struct {
	ID                string   `json:"id"`
	Provider          string   `json:"provider"`
	Name              string   `json:"name"`
	Type              string   `json:"type"`
	Overview          string   `json:"overview"`
	ProductionYear    int      `json:"production_year"`
	PremiereDate      string   `json:"premiere_date"`
	CommunityRating   float64  `json:"community_rating"`
	RunTimeTicks      int64    `json:"runtime_ticks"`
	IndexNumber       int      `json:"episode"`
	ParentIndexNumber int      `json:"season"`
	ParentID          string   `json:"parent_id"`
	SeriesID          string   `json:"series_id"`
	SeriesName        string   `json:"series_name"`
	ChildCount        int      `json:"child_count"`
	Genres            []string `json:"genres,omitempty"`
	PosterURL         string   `json:"poster_url"`
	ThumbnailURL      string   `json:"thumbnail_url"`
	Played            bool     `json:"played"`
	Favorite          bool     `json:"favorite"`
	ResumeTicks       int64    `json:"resume_ticks"`
	ProgressPercent   float64  `json:"progress_percent"`
}

type mediaPlaybackResponse struct {
	Provider         string              `json:"provider"`
	ItemID           string              `json:"item_id"`
	StreamURL        string              `json:"stream_url"`
	DirectStreamURL  string              `json:"direct_stream_url"`
	NetBirdStreamURL string              `json:"netbird_stream_url"`
	ResumeTicks      int64               `json:"resume_ticks"`
	RuntimeTicks     int64               `json:"runtime_ticks"`
	Played           bool                `json:"played"`
	Favorite         bool                `json:"favorite"`
	Media            JellyfinMediaInfo   `json:"media"`
	Diagnostic       *PlaybackDiagnostic `json:"diagnostic,omitempty"`
}

type mediaProgressInput struct {
	Event         string `json:"event"`
	Ticks         int64  `json:"ticks"`
	DurationTicks int64  `json:"duration_ticks"`
}

type mediaStateInput struct {
	Played   *bool `json:"played"`
	Favorite *bool `json:"favorite"`
}

func resolveMediaProvider(name string) (mediaprovider.MediaProvider, error) {
	if !strings.EqualFold(strings.TrimSpace(name), "jellyfin") {
		return nil, fmt.Errorf("不支持的媒体提供商：%s", name)
	}
	client, err := resolveJellyfinPlaybackClient()
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("jellyfin has no available user")
	}
	directURL, _ := normalizeJellyfinBaseURL(configValue(model.ConfigKeyJellyfinDirectUrl))
	return mediaprovider.NewJellyfinProvider(client, directURL, configValue(model.ConfigKeyJellyfinApiKey)), nil
}

func mediaImageURL(provider, itemID string) string {
	return "/api/v1/media/providers/" + url.PathEscape(provider) + "/items/" + url.PathEscape(itemID) + "/image"
}

func mediaItemDTO(provider string, item mediaprovider.Item) mediaItemResponse {
	image := mediaImageURL(provider, item.ID)
	return mediaItemResponse{
		ID: item.ID, Provider: provider, Name: item.Name, Type: item.Type,
		Overview: item.Overview, ProductionYear: item.ProductionYear,
		PremiereDate: item.PremiereDate, CommunityRating: item.CommunityRating,
		RunTimeTicks: item.RuntimeTicks, IndexNumber: item.IndexNumber,
		ParentIndexNumber: item.ParentIndexNumber, ParentID: item.ParentID,
		SeriesID: item.SeriesID, SeriesName: item.SeriesName, ChildCount: item.ChildCount,
		Genres: item.Genres, PosterURL: image, ThumbnailURL: image,
		Played: item.UserState.Played, Favorite: item.UserState.Favorite,
		ResumeTicks: item.UserState.ResumeTicks, ProgressPercent: item.UserState.ProgressPercent,
	}
}

func configuredJellyfinLibraryIDs() []string {
	raw := strings.TrimSpace(configValue(model.ConfigKeyJellyfinLibraryIDs))
	if raw == "" {
		return nil
	}
	var values []string
	if json.Unmarshal([]byte(raw), &values) == nil {
		result := make([]string, 0, len(values))
		for _, value := range values {
			if value = strings.TrimSpace(value); value != "" {
				result = append(result, value)
			}
		}
		return result
	}
	return splitNonEmpty(raw)
}

func configuredLibrarySet() map[string]bool {
	values := configuredJellyfinLibraryIDs()
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func V1MediaProvidersHandler(c *gin.Context) {
	client, err := resolveMediaProvider("jellyfin")
	connected := err == nil && client != nil
	detail := ""
	capabilities := mediaprovider.ProviderCapabilities{Libraries: true, Search: true, Episodes: true, Progress: true, Favorites: true, Images: true}
	if err != nil {
		detail = err.Error()
	} else {
		capabilities = client.Capabilities()
	}
	v1Data(c, http.StatusOK, gin.H{"providers": []gin.H{{
		"id": "jellyfin", "name": "Jellyfin", "configured": connected || configValue(model.ConfigKeyJellyfinUrl) != "",
		"connected": connected, "detail": detail,
		"capabilities": capabilities,
	}}})
}

func V1MediaLibrariesHandler(c *gin.Context) {
	client, err := resolveMediaProvider(c.Param("provider"))
	if err != nil {
		v1Error(c, http.StatusServiceUnavailable, "media_provider_unavailable", "媒体提供商暂时不可用")
		return
	}
	libraries, err := client.ListLibraries(c.Request.Context())
	if err != nil {
		v1Error(c, http.StatusBadGateway, "media_libraries_failed", "读取媒体库失败")
		return
	}
	selected := configuredLibrarySet()
	items := make([]mediaLibraryResponse, 0, len(libraries))
	for _, library := range libraries {
		items = append(items, mediaLibraryResponse{
			ID: library.ID, Name: library.Name, CollectionType: library.CollectionType,
			ItemCount: library.ItemCount, Selected: len(selected) == 0 || selected[library.ID],
		})
	}
	v1Data(c, http.StatusOK, gin.H{"items": items})
}

func V1MediaItemsHandler(c *gin.Context) {
	provider := strings.TrimSpace(c.Param("provider"))
	client, err := resolveMediaProvider(provider)
	if err != nil {
		v1Error(c, http.StatusServiceUnavailable, "media_provider_unavailable", "媒体提供商暂时不可用")
		return
	}
	section := strings.TrimSpace(c.Query("section"))
	if section == "continue" {
		page, err := client.ListContinueWatching(c.Request.Context(), 48)
		if err != nil {
			v1Error(c, http.StatusBadGateway, "media_items_failed", "读取继续观看失败")
			return
		}
		respondMediaPage(c, provider, page.Items, page.Total, 1, len(page.Items))
		return
	}
	if section == "favorites" {
		page, err := client.ListFavorites(c.Request.Context(), 48)
		if err != nil {
			v1Error(c, http.StatusBadGateway, "media_items_failed", "读取收藏失败")
			return
		}
		respondMediaPage(c, provider, page.Items, page.Total, 1, len(page.Items))
		return
	}

	pageNumber, pageSize := v1Pagination(c, 48)
	search := strings.TrimSpace(c.Query("q"))
	libraryID := strings.TrimSpace(c.Query("library_id"))
	parentID := strings.TrimSpace(c.Query("parent_id"))
	sortBy := strings.TrimSpace(c.Query("sort_by"))
	if sortBy == "" {
		sortBy = "SortName"
	}
	sortOrder := strings.TrimSpace(c.Query("sort_order"))
	if sortOrder == "" {
		sortOrder = "Ascending"
	}
	libraryIDs := configuredJellyfinLibraryIDs()
	if libraryID != "" && libraryID != "all" {
		libraryIDs = []string{libraryID}
	}
	if parentID != "" {
		libraryIDs = []string{parentID}
	}
	if len(libraryIDs) == 0 {
		libraryIDs = []string{""}
	}
	if len(libraryIDs) == 1 {
		types := []string{"Series", "Movie"}
		if parentID != "" {
			types = []string{"Season", "Movie", "Episode", "Series"}
		}
		page, listErr := client.ListItems(c.Request.Context(), mediaprovider.Query{
			ParentID: libraryIDs[0], SearchTerm: search, IncludeItemTypes: types,
			SortBy: sortBy, SortOrder: sortOrder, Recursive: parentID == "",
			StartIndex: (pageNumber - 1) * pageSize, Limit: pageSize,
		})
		if listErr != nil {
			v1Error(c, http.StatusBadGateway, "media_items_failed", "读取媒体项目失败")
			return
		}
		respondMediaPage(c, provider, page.Items, page.Total, pageNumber, pageSize)
		return
	}
	all := make([]mediaprovider.Item, 0)
	total := 0
	fetchLimit := min(pageNumber*pageSize, 200)
	for _, id := range libraryIDs {
		types := []string{"Series", "Movie"}
		if parentID != "" {
			types = []string{"Season", "Movie", "Episode", "Series"}
		}
		page, listErr := client.ListItems(c.Request.Context(), mediaprovider.Query{
			ParentID: id, SearchTerm: search, IncludeItemTypes: types,
			SortBy: sortBy, SortOrder: sortOrder, Recursive: parentID == "",
			StartIndex: 0, Limit: fetchLimit,
		})
		if listErr != nil {
			v1Error(c, http.StatusBadGateway, "media_items_failed", "读取媒体项目失败")
			return
		}
		all = append(all, page.Items...)
		total += page.Total
	}
	start := min((pageNumber-1)*pageSize, len(all))
	end := min(start+pageSize, len(all))
	respondMediaPage(c, provider, all[start:end], total, pageNumber, pageSize)
}

func V1MediaItemHandler(c *gin.Context) {
	provider := strings.TrimSpace(c.Param("provider"))
	client, err := resolveMediaProvider(provider)
	if err != nil {
		v1Error(c, http.StatusServiceUnavailable, "media_provider_unavailable", "媒体提供商暂时不可用")
		return
	}
	item, err := client.GetItem(c.Request.Context(), c.Param("id"))
	if err != nil {
		v1Error(c, http.StatusNotFound, "media_item_not_found", "未找到媒体项目")
		return
	}
	v1Data(c, http.StatusOK, mediaItemDTO(provider, *item))
}

func V1MediaChildrenHandler(c *gin.Context) {
	provider := strings.TrimSpace(c.Param("provider"))
	client, err := resolveMediaProvider(provider)
	if err != nil {
		v1Error(c, http.StatusServiceUnavailable, "media_provider_unavailable", "媒体提供商暂时不可用")
		return
	}
	includeTypes := []string{"Season"}
	if strings.EqualFold(c.Query("type"), "episode") {
		includeTypes = []string{"Episode"}
	}
	page, err := client.ListChildren(c.Request.Context(), c.Param("id"), includeTypes)
	if err != nil {
		v1Error(c, http.StatusBadGateway, "media_children_failed", "读取剧集列表失败")
		return
	}
	respondMediaPage(c, provider, page.Items, page.Total, 1, len(page.Items))
}

func V1MediaContinueHandler(c *gin.Context) {
	provider := strings.TrimSpace(c.Param("provider"))
	client, err := resolveMediaProvider(provider)
	if err != nil {
		v1Error(c, http.StatusServiceUnavailable, "media_provider_unavailable", "媒体提供商暂时不可用")
		return
	}
	_, pageSize := v1Pagination(c, 48)
	page, err := client.ListContinueWatching(c.Request.Context(), pageSize)
	if err != nil {
		v1Error(c, http.StatusBadGateway, "media_items_failed", "读取继续观看失败")
		return
	}
	respondMediaPage(c, provider, page.Items, page.Total, 1, len(page.Items))
}

func V1MediaImageHandler(c *gin.Context) {
	client, err := resolveMediaProvider(c.Param("provider"))
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	data, err := client.GetImage(c.Request.Context(), c.Param("id"))
	if err != nil || len(data) == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "private, max-age=300")
	c.Data(http.StatusOK, http.DetectContentType(data), data)
}

func V1MediaPlayHandler(c *gin.Context) {
	provider := strings.TrimSpace(c.Param("provider"))
	itemID := strings.TrimSpace(c.Param("id"))
	client, err := resolveMediaProvider(provider)
	if err != nil {
		v1Error(c, http.StatusServiceUnavailable, "media_provider_unavailable", "媒体提供商暂时不可用")
		return
	}
	playback, err := client.GetPlayback(c.Request.Context(), itemID)
	if err != nil {
		v1Error(c, http.StatusNotFound, "media_item_not_found", "未找到媒体项目")
		return
	}
	proxyURL := "/api/v1/media/providers/" + url.PathEscape(provider) + "/items/" + url.PathEscape(itemID) + "/stream"
	netBirdURL := ""
	if userID, userErr := currentSessionUserID(c); userErr == nil {
		if token, tokenErr := signMediaNetBirdStreamToken(provider, itemID, userID, time.Now().Add(netBirdStreamTokenTTL)); tokenErr == nil {
			if base, baseErr := normalizeNetBirdProxyBaseURL(configValue(model.ConfigKeyNetBirdProxyURL)); baseErr == nil && base != "" {
				netBirdURL = fmt.Sprintf("%s/api/v1/netbird/media/%s/stream/%s?token=%s", base, url.PathEscape(provider), url.PathEscape(itemID), url.QueryEscape(token))
			}
		}
	}
	v1Data(c, http.StatusOK, mediaPlaybackResponse{
		Provider: provider, ItemID: itemID, StreamURL: proxyURL,
		DirectStreamURL: playback.DirectStreamURL, NetBirdStreamURL: netBirdURL,
		ResumeTicks: playback.ResumeTicks, RuntimeTicks: playback.RuntimeTicks,
		Played: playback.Played, Favorite: playback.Favorite,
		Media: JellyfinMediaInfo{},
	})
}

func MediaStreamHandler(c *gin.Context) {
	if !strings.EqualFold(c.Param("provider"), "jellyfin") {
		c.Status(http.StatusNotFound)
		return
	}
	proxyVideoForJellyfinItem(c, c.Param("id"))
}

func V1MediaProgressHandler(c *gin.Context) {
	if !strings.EqualFold(c.Param("provider"), "jellyfin") {
		v1Error(c, http.StatusNotFound, "media_provider_not_found", "未找到媒体提供商")
		return
	}
	var input mediaProgressInput
	if err := c.ShouldBindJSON(&input); err != nil || input.Ticks < 0 || input.DurationTicks < 0 {
		v1Error(c, http.StatusBadRequest, "invalid_media_progress", "播放进度格式不正确")
		return
	}
	client, err := resolveMediaProvider(c.Param("provider"))
	if err != nil {
		v1Error(c, http.StatusServiceUnavailable, "media_provider_unavailable", "媒体提供商暂时不可用")
		return
	}
	err = client.UpdateProgress(c.Request.Context(), c.Param("id"), mediaprovider.ProgressInput{
		Event: input.Event, Ticks: input.Ticks, DurationTicks: input.DurationTicks,
	})
	if err != nil {
		v1Error(c, http.StatusBadGateway, "media_progress_failed", "同步播放进度失败")
		return
	}
	v1Data(c, http.StatusOK, gin.H{"ok": true})
}

func V1MediaStateHandler(c *gin.Context) {
	if !strings.EqualFold(c.Param("provider"), "jellyfin") {
		v1Error(c, http.StatusNotFound, "media_provider_not_found", "未找到媒体提供商")
		return
	}
	var input mediaStateInput
	if err := c.ShouldBindJSON(&input); err != nil || (input.Played == nil && input.Favorite == nil) {
		v1Error(c, http.StatusBadRequest, "invalid_media_state", "至少需要提交已看或收藏状态")
		return
	}
	client, err := resolveMediaProvider(c.Param("provider"))
	if err != nil {
		v1Error(c, http.StatusServiceUnavailable, "media_provider_unavailable", "媒体提供商暂时不可用")
		return
	}
	itemID := c.Param("id")
	err = client.UpdateUserState(c.Request.Context(), itemID, mediaprovider.UserStateInput{Played: input.Played, Favorite: input.Favorite})
	if err != nil {
		v1Error(c, http.StatusBadGateway, "media_state_failed", "同步媒体状态失败")
		return
	}
	item, _ := client.GetItem(c.Request.Context(), itemID)
	played, favorite := false, false
	if item != nil {
		played, favorite = item.UserState.Played, item.UserState.Favorite
	}
	v1Data(c, http.StatusOK, gin.H{"played": played, "favorite": favorite})
}

func respondMediaPage(c *gin.Context, provider string, items []mediaprovider.Item, total, page, pageSize int) {
	result := make([]mediaItemResponse, 0, len(items))
	for _, item := range items {
		result = append(result, mediaItemDTO(provider, item))
	}
	v1Page(c, result, page, pageSize, int64(total))
}

func splitNonEmpty(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == ' ' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}
