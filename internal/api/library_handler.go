package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/service"
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
	for _, m := range metadata {
		items = append(items, LibraryItem{
			AnimeMetadata: m,
			IsSubscribed:  subMap[m.ID],
			IsLocal:       localMap[m.ID],
		})
	}

	c.HTML(http.StatusOK, "library.html", gin.H{
		"Metadata":   items,
		"SearchTerm": query,
		"SkipLayout": isHTMX(c),
	})
}

// RefreshLibraryMetadataHandler triggers a background global refresh
func RefreshLibraryMetadataHandler(c *gin.Context) {
	force := c.Query("force") == ValueTrue
	svc := service.NewLocalAnimeService()
	if service.GlobalRefreshStatus.IsRunning {
		c.JSON(http.StatusOK, gin.H{"message": "已经在刷新中", "status": "running"})
		return
	}

	// Run in background
	go svc.RefreshAllMetadata(force)

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

	svc := service.NewLocalAnimeService()
	if err := svc.RefreshSingleMetadata(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "刷新失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "刷新成功", "status": "ok"})
}

// GetRefreshStatusHandler returns the current global refresh status
func GetRefreshStatusHandler(c *gin.Context) {
	c.JSON(http.StatusOK, service.GlobalRefreshStatus)
}
