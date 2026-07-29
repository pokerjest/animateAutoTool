package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/anilist"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/pokerjest/animateAutoTool/internal/tmdb"
)

// LibraryItem wraps AnimeMetadata with additional status flags
type LibraryItem struct {
	model.AnimeMetadata
	IsSubscribed bool `json:"is_subscribed"`
	IsLocal      bool `json:"is_local"`
	LocalAnimeID uint `json:"local_anime_id"` // 0 if not local
}

func GetLibraryHandler(c *gin.Context) {
	query := c.Query("q")
	var metadata []model.AnimeMetadata

	dbCtx := db.DB.Model(&model.AnimeMetadata{})

	if query != "" {
		// Basic search across titles
		likeQuery := "%" + query + "%"
		dbCtx = dbCtx.Where("title LIKE ? OR title_cn LIKE ? OR title_en LIKE ? OR title_jp LIKE ?", likeQuery, likeQuery, likeQuery, likeQuery)
	}

	year := c.Query("year")
	if year != "" && year != "all" {
		dbCtx = dbCtx.Where("air_date LIKE ?", year+"%")
	}

	// Order by updated_at desc usually makes sense to see new stuff
	if err := dbCtx.Order("updated_at desc").Find(&metadata).Error; err != nil {
		htmlServerError(c, "读取媒体库数据", err)
		return
	}

	// Fetch status maps
	subMap := make(map[uint]bool)
	var subscriptions []model.Subscription
	db.DB.Select("metadata_id").Where("metadata_id IS NOT NULL").Find(&subscriptions)
	for _, s := range subscriptions {
		if s.MetadataID != nil {
			subMap[*s.MetadataID] = true
		}
	}

	localMap := make(map[uint]uint) // MetadataID -> LocalAnimeID
	var localAnimes []model.LocalAnime
	db.DB.Select("id, metadata_id").Where("metadata_id IS NOT NULL").Find(&localAnimes)
	for _, l := range localAnimes {
		if l.MetadataID != nil {
			localMap[*l.MetadataID] = l.ID
		}
	}

	// Construct items
	var items []LibraryItem
	seenBangumiIDs := make(map[int]bool)
	seenTitles := make(map[string]bool)

	statusFilter := c.Query("status")

	for _, m := range metadata {
		// Deduplication Strategy:
		if m.BangumiID > 0 {
			if seenBangumiIDs[m.BangumiID] {
				continue
			}
			seenBangumiIDs[m.BangumiID] = true
		}
		if seenTitles[m.Title] {
			continue
		}
		seenTitles[m.Title] = true

		isSub := subMap[m.ID]
		localID := localMap[m.ID]
		isLocal := localID > 0

		// Apply Status Filter
		if statusFilter == "subscribed" && !isSub {
			continue
		}
		if statusFilter == "local" && !isLocal {
			continue
		}

		items = append(items, LibraryItem{
			AnimeMetadata: m,
			IsSubscribed:  isSub,
			IsLocal:       isLocal,
			LocalAnimeID:  localID,
		})
	}

	c.HTML(http.StatusOK, "library.html", gin.H{
		"Metadata":   items,
		"SearchTerm": query,
		"Year":       year,
		"Status":     c.Query("status"),
		"SkipLayout": IsHTMX(c),
	})
}

// RefreshLibraryMetadataHandler triggers a background global refresh
func RefreshLibraryMetadataHandler(c *gin.Context) {
	force := c.Query("force") == ValueTrue
	metaSvc := service.NewMetadataService()
	if !metaSvc.StartRefreshAllMetadata(force) {
		c.JSON(http.StatusOK, gin.H{"message": "已经在刷新中", "status": "running"})
		return
	}

	msg := "已开始后台增量刷新元数据"
	if force {
		msg = "已开始后台全量强制刷新所有元数据"
	}

	c.JSON(http.StatusOK, gin.H{
		"message": msg,
		"status":  "started",
	})
}

// RefreshItemMetadataHandler refreshes a single anime metadata
func RefreshItemMetadataHandler(c *gin.Context) {
	idStr := c.Param("id")
	idUint64, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "无效的ID参数"})
		return
	}
	id := uint(idUint64)

	metaSvc := service.NewMetadataService()
	if err := metaSvc.RefreshSingleMetadata(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "刷新失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "刷新成功", "status": "ok"})
}

// GetRefreshStatusHandler returns the current global refresh status
func GetRefreshStatusHandler(c *gin.Context) {
	c.JSON(http.StatusOK, service.GlobalRefreshStatus.Snapshot())
}

type FixMatchRequest struct {
	ID       uint   `json:"id"`   // Can be AnimeID or MetadataID depending on Type
	Type     string `json:"type"` // "local" or "metadata"
	Source   string `json:"source"`
	SourceID int    `json:"source_id"`
	Matches  *struct {
		BangumiID int `json:"bangumi_id"`
		TMDBID    int `json:"tmdb_id"`
		AniListID int `json:"anilist_id"`
	} `json:"matches,omitempty"`
	// Backwards compatibility
	AnimeID uint `json:"anime_id"`
}

func FixMatchHandler(c *gin.Context) {
	var req FixMatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonBadRequest(c, "修正匹配请求格式不正确")
		return
	}

	// Default to bangumi if source is empty (backward compatibility)
	if req.Source == "" {
		req.Source = SourceBangumi
	}

	metaSvc := service.NewMetadataService()

	// Logic for Local Anime (legacy or explicit)
	if req.Type == "local" || (req.Type == "" && req.AnimeID > 0) {
		id := req.ID
		if req.AnimeID > 0 {
			id = req.AnimeID
		}
		var err error
		if req.Matches != nil {
			err = metaSvc.MatchSeriesSources(id, req.Matches.BangumiID, req.Matches.TMDBID, req.Matches.AniListID)
		} else {
			err = metaSvc.MatchSeries(id, req.Source, req.SourceID)
		}
		if err != nil {
			jsonServerError(c, "修正本地番剧匹配", err)
			return
		}
	} else {
		// Metadata only fix
		var err error
		if req.Matches != nil {
			err = metaSvc.MatchMetadataSources(req.ID, req.Matches.BangumiID, req.Matches.TMDBID, req.Matches.AniListID)
		} else {
			err = metaSvc.MatchMetadata(req.ID, req.Source, req.SourceID)
		}
		if err != nil {
			jsonServerError(c, "修正元数据匹配", err)
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "匹配关系已更新"})
}

type SearchResult struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	NameCN string `json:"name_cn"`
	Images struct {
		Large  string `json:"large"`
		Common string `json:"common"`
		Medium string `json:"medium"`
		Small  string `json:"small"`
		Grid   string `json:"grid"`
	} `json:"images"`
	Summary string `json:"summary"`
	AirDate string `json:"air_date"`
}

// SearchMetadataMatchHandler performs a deterministic cross-provider lookup.
// It returns grouped candidates while keeping the legacy /metadata/search
// endpoint unchanged for older clients.
func SearchMetadataMatchHandler(c *gin.Context) {
	keyword := c.Query("q")
	source := c.DefaultQuery("source", SourceBangumi)
	sourceID, _ := strconv.Atoi(c.Query("source_id"))
	if keyword == "" {
		if localID, err := strconv.ParseUint(c.Query("local_anime_id"), 10, 32); err == nil && localID > 0 {
			var anime model.LocalAnime
			if db.DB.First(&anime, uint(localID)).Error == nil {
				keyword = anime.Title
			}
		}
	}
	result, err := searchMetadataMatchCandidates(c.Request.Context(), metadataSearchOptions{
		Query: keyword, Source: source, SourceID: sourceID,
	})
	if err != nil {
		if strings.Contains(err.Error(), "不支持的元数据来源") || strings.Contains(err.Error(), "关键词不能为空") {
			jsonBadRequest(c, "搜索三源元数据: "+err.Error())
		} else {
			jsonServerError(c, "搜索三源元数据", err)
		}
		return
	}
	c.JSON(http.StatusOK, result)
}

func bangumiMetadataSearchResults(results []bangumi.SearchResult) []SearchResult {
	items := make([]SearchResult, 0, len(results))
	for _, result := range results {
		var item SearchResult
		item.ID = result.ID
		item.Name = result.Name
		item.NameCN = result.NameCN
		item.Images.Large = result.Images.Large
		item.Images.Common = result.Images.Common
		item.Images.Medium = result.Images.Medium
		item.Images.Small = result.Images.Small
		item.Images.Grid = result.Images.Grid
		items = append(items, item)
	}
	return items
}

func SearchMetadataHandler(c *gin.Context) {
	keyword := c.Query("q")
	source := c.Query("source")
	if keyword == "" {
		jsonBadRequest(c, "请输入搜索关键词")
		return
	}

	if source == "" {
		source = SourceBangumi
	}

	switch source {
	case SourceTMDB:
		token := configValue(model.ConfigKeyTMDBToken)
		if token == "" {
			jsonBadRequest(c, "还没有配置 TMDB Token")
			return
		}

		tmdbClient := tmdb.NewClient(token, configuredProxyURL(model.ConfigKeyProxyTMDB))

		results, err := tmdbClient.SearchTVContext(c.Request.Context(), keyword)
		if err != nil {
			jsonServerError(c, "搜索 TMDB 元数据", err)
			return
		}

		genericResults := make([]SearchResult, 0, len(results))
		for _, show := range results {
			var r SearchResult
			r.ID = show.ID
			r.Name = show.OriginalName
			r.NameCN = show.Name
			if show.PosterPath != "" {
				r.Images.Large = show.PosterPath
			}
			r.Summary = show.Overview
			r.AirDate = show.FirstAirDate
			genericResults = append(genericResults, r)
		}
		c.JSON(http.StatusOK, genericResults)

	case SourceAniList:
		token := configValue(model.ConfigKeyAniListToken)
		if token == "" {
			jsonBadRequest(c, "还没有配置 AniList Token")
			return
		}

		client := anilist.NewClient(token, configuredProxyURL(model.ConfigKeyProxyAniList))

		result, err := client.SearchAnimeContext(c.Request.Context(), keyword)
		if err != nil {
			jsonServerError(c, "搜索 AniList 元数据", err)
			return
		}

		genericResults := make([]SearchResult, 0, 1)
		if result != nil {
			var r SearchResult
			r.ID = result.ID
			r.Name = result.Title.Native
			r.NameCN = result.Title.Romaji // Fallback
			if result.Title.English != "" {
				r.NameCN = result.Title.English
			}
			if result.CoverImage.ExtraLarge != "" {
				r.Images.Large = result.CoverImage.ExtraLarge
			} else {
				r.Images.Large = result.CoverImage.Large
			}
			r.Summary = result.Description
			// StartDate not available in client struct
			genericResults = append(genericResults, r)
		}
		c.JSON(http.StatusOK, genericResults)

	default: // Bangumi
		client := bangumi.NewClient("", "", "")
		applyProxyToBangumiClient(client)

		results, err := client.SearchSubjectsContext(c.Request.Context(), keyword)
		if err != nil {
			jsonServerError(c, "搜索 Bangumi 元数据", err)
			return
		}
		c.JSON(http.StatusOK, bangumiMetadataSearchResults(results))
	}
}

func GetBangumiSubjectHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, _ := strconv.Atoi(idStr)

	client := bangumi.NewClient("", "", "")
	applyProxyToBangumiClient(client)

	subject, err := client.GetSubjectContext(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, subject)
}

// GetPosterHandler handles image requests from the database
func GetPosterHandler(c *gin.Context) {
	id := c.Param("id")
	source := c.Query("source") // source can be 'active', 'bangumi', 'tmdb', 'anilist'

	var m model.AnimeMetadata
	if err := db.DB.First(&m, id).Error; err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	var data []byte
	switch source {
	case SourceBangumi:
		data = m.BangumiImageRaw
	case SourceTMDB:
		data = m.TMDBImageRaw
	case SourceAniList:
		data = m.AniListImageRaw
	default:
		// Default to current active source or first available
		if m.Title == m.BangumiTitle && len(m.BangumiImageRaw) > 0 {
			data = m.BangumiImageRaw
		} else if m.Title == m.TMDBTitle && len(m.TMDBImageRaw) > 0 {
			data = m.TMDBImageRaw
		} else if m.Title == m.AniListTitle && len(m.AniListImageRaw) > 0 {
			data = m.AniListImageRaw
		} else {
			// fallback to whatever is not empty
			if len(m.BangumiImageRaw) > 0 {
				data = m.BangumiImageRaw
			} else if len(m.TMDBImageRaw) > 0 {
				data = m.TMDBImageRaw
			} else if len(m.AniListImageRaw) > 0 {
				data = m.AniListImageRaw
			}
		}
	}

	if len(data) == 0 {
		c.Status(http.StatusNotFound)
		return
	}

	servePosterImage(c, data)
}

// ProxyTMDBImageHandler proxies TMDB images through the server
func ProxyTMDBImageHandler(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.Status(http.StatusBadRequest)
		return
	}

	// Fetch token from DB
	var token model.GlobalConfig
	db.DB.Where("key = ?", model.ConfigKeyTMDBToken).First(&token)

	tmdbClient := tmdb.NewClient(token.Value, configuredProxyURL(model.ConfigKeyProxyTMDB))
	resp, err := tmdbClient.ProxyImage(path)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	if resp.IsError() {
		c.Status(resp.StatusCode())
		return
	}

	c.Data(http.StatusOK, resp.Header().Get("Content-Type"), resp.Body())
}

func randomBackgroundURL() (string, error) {
	// Query for a random metadata record that has some image data
	// Note: RANDOM() is SQLite specific, usually ORDER BY RANDOM()
	// GORM raw sql is easiest here or Find with Random order

	var m model.AnimeMetadata
	// Prioritize items with Bangumi/TMDB images which are likely high quality
	// Use ORDER BY RANDOM() LIMIT 1
	// Check for non-empty blobs.
	result := db.DB.Where("length(bangumi_image_raw) > 0 OR length(tmdb_image_raw) > 0 OR length(ani_list_image_raw) > 0").
		Order("RANDOM()").
		First(&m)

	if result.Error != nil {
		return "", result.Error
	}

	// Determine best source
	source := SourceBangumi
	if len(m.BangumiImageRaw) > 0 {
		source = SourceBangumi
	} else if len(m.TMDBImageRaw) > 0 {
		source = SourceTMDB
	} else if len(m.AniListImageRaw) > 0 {
		source = SourceAniList
	}

	return fmt.Sprintf("/api/v1/posters/%d?source=%s", m.ID, source), nil
}

// GetRandomBackgroundHandler returns a URL to a random anime cover image for
// the legacy HTML UI. New clients use V1RandomBackgroundHandler.
func GetRandomBackgroundHandler(c *gin.Context) {
	backgroundURL, err := randomBackgroundURL()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "暂时没有找到可用封面"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "url": backgroundURL})
}

// V1RandomBackgroundHandler returns a same-origin poster URL that clients can
// resize with the poster endpoint's width query parameter.
func V1RandomBackgroundHandler(c *gin.Context) {
	backgroundURL, err := randomBackgroundURL()
	if err != nil {
		v1Error(c, http.StatusNotFound, "background_not_found", "暂时没有找到可用封面")
		return
	}

	v1Data(c, http.StatusOK, gin.H{"url": backgroundURL})
}
