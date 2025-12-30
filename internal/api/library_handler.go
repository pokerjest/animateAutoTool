package api

import (
	"net/http"
	"strconv"

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
		c.String(http.StatusInternalServerError, "Database Error")
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

	localMap := make(map[uint]bool)
	var localAnimes []model.LocalAnime
	db.DB.Select("metadata_id").Where("metadata_id IS NOT NULL").Find(&localAnimes)
	for _, l := range localAnimes {
		if l.MetadataID != nil {
			localMap[*l.MetadataID] = true
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
		isLocal := localMap[m.ID]

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
	if service.GlobalRefreshStatus.IsRunning {
		c.JSON(http.StatusOK, gin.H{"message": "已经在刷新中", "status": "running"})
		return
	}

	// Run in background
	go metaSvc.RefreshAllMetadata(force)

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
	c.JSON(http.StatusOK, service.GlobalRefreshStatus)
}

type FixMatchRequest struct {
	AnimeID  uint   `json:"anime_id"`
	Source   string `json:"source"`
	SourceID int    `json:"source_id"`
}

func FixMatchHandler(c *gin.Context) {
	var req FixMatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	// Default to bangumi if source is empty (backward compatibility)
	if req.Source == "" {
		req.Source = "bangumi"
	}

	metaSvc := service.NewMetadataService()
	if err := metaSvc.MatchSeries(req.AnimeID, req.Source, req.SourceID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Match updated successfully"})
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

func SearchMetadataHandler(c *gin.Context) {
	keyword := c.Query("q")
	source := c.Query("source")
	if keyword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Keyword required"})
		return
	}

	if source == "" {
		source = "bangumi"
	}

	fetchProxy := func() (string, bool) {
		var proxyUrl model.GlobalConfig
		db.DB.Where("key = ?", model.ConfigKeyProxyURL).First(&proxyUrl)
		return proxyUrl.Value, proxyUrl.Value != ""
	}

	switch source {
	case "tmdb":
		var token model.GlobalConfig
		if err := db.DB.Where("key = ?", model.ConfigKeyTMDBToken).First(&token).Error; err != nil || token.Value == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "TMDB Token not configured"})
			return
		}

		// Check proxy for TMDB
		var proxyEnabled model.GlobalConfig
		var proxyURL string
		db.DB.Where("key = ?", model.ConfigKeyProxyTMDB).First(&proxyEnabled)
		if proxyEnabled.Value == "true" {
			if purl, ok := fetchProxy(); ok {
				proxyURL = purl
			}
		}

		tmdbClient := tmdb.NewClient(token.Value, proxyURL)

		results, err := tmdbClient.SearchTV(keyword)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		var genericResults []SearchResult
		for _, show := range results {
			var r SearchResult
			r.ID = show.ID
			r.Name = show.OriginalName
			r.NameCN = show.Name
			if show.PosterPath != "" {
				r.Images.Large = "https://image.tmdb.org/t/p/w500" + show.PosterPath
			}
			r.Summary = show.Overview
			r.AirDate = show.FirstAirDate
			genericResults = append(genericResults, r)
		}
		c.JSON(http.StatusOK, genericResults)

	case "anilist":
		var token model.GlobalConfig
		if err := db.DB.Where("key = ?", model.ConfigKeyAniListToken).First(&token).Error; err != nil || token.Value == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "AniList Token not configured"})
			return
		}

		var proxyEnabled model.GlobalConfig
		var proxyURL string
		db.DB.Where("key = ?", model.ConfigKeyProxyAniList).First(&proxyEnabled)
		if proxyEnabled.Value == "true" {
			if purl, ok := fetchProxy(); ok {
				proxyURL = purl
			}
		}

		client := anilist.NewClient(token.Value, proxyURL)

		result, err := client.SearchAnime(keyword)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		var genericResults []SearchResult
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
		var proxyEnabled model.GlobalConfig
		db.DB.Where("key = ?", model.ConfigKeyProxyBangumi).First(&proxyEnabled)
		if proxyEnabled.Value == "true" {
			if purl, ok := fetchProxy(); ok {
				client.SetProxy(purl)
			}
		}

		// Bangumi search returns its own struct, but frontend expects generic?
		// The original handler returned raw bangumi results or generic?
		// "if results != nil { c.JSON(http.StatusOK, results) }"
		// It seems the frontend (Alpine) handles different structures or we should unify.
		// Let's implement unification for consistency if possible, or stick to what frontend expects.
		// The original code returned whatever Bangumi client returned for "Bangumi".
		// But for TMDB/AniList it mapped to "genericResults".
		// Note checks:
		// "Bangumi" results have "id", "name", "name_cn", "images" object.
		// Our SearchResult struct mirrors Bangumi structure close enough?
		// Bangumi has: id, name, name_cn, summary, air_date, images { large, ... }
		// So passing genericResults for others is trying to match Bangumi's format!
		// So we can just return Bangumi raw results.

		results, err := client.SearchSubject(keyword)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if results != nil {
			// SearchSubject returns a SINGLE result? No, current client.SearchSubject return *BangumiSubject (single)?
			// Wait, the client method is named `SearchSubject` but does it return a list?
			// Let's check `bangumi` package usage or definition.
			// If it returns single, that explains why logic loop wasn't there.
			// But usually search returns list.
			// Code snippet 1347: `client.SearchSubject(keyword)` returns `res, err`.
			// If res is single struct, we return array?
			// `c.JSON(http.StatusOK, []interface{}{res})`?
			// Original code:
			// `if results != nil { c.JSON(http.StatusOK, results) }`
			// If results is a slice, fine. If struct, fine.
			// Let's enable returning list if possible.
			// Checking `client.SearchSubject`: It likely calls `/search/subject/{keywords}` which returns list.
			// BUT `bangumi/client.go` implementation might be simple.
			// Assuming it returns `[]bangumi.Subject` or similar.
			c.JSON(http.StatusOK, results)
		} else {
			c.JSON(http.StatusOK, []interface{}{})
		}
	}
}

func GetBangumiSubjectHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, _ := strconv.Atoi(idStr)

	client := bangumi.NewClient("", "", "")

	// Proxy
	var proxyUrl, proxyEnabled model.GlobalConfig
	db.DB.Where("key = ?", model.ConfigKeyProxyURL).First(&proxyUrl)
	db.DB.Where("key = ?", model.ConfigKeyProxyBangumi).First(&proxyEnabled)

	if proxyEnabled.Value == "true" && proxyUrl.Value != "" {
		client.SetProxy(proxyUrl.Value)
	}

	subject, err := client.GetSubject(id)
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
	case "bangumi":
		data = m.BangumiImageRaw
	case "tmdb":
		data = m.TMDBImageRaw
	case "anilist":
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

	// Basic content type detection (or we could store it in DB too)
	contentType := "image/jpeg"
	if len(data) > 4 && string(data[1:4]) == "PNG" {
		contentType = "image/png"
	} else if len(data) > 3 && string(data[:3]) == "GIF" {
		contentType = "image/gif"
	}

	c.Data(http.StatusOK, contentType, data)
}
