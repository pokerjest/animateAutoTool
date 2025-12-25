package api

import (
	"fmt"
	"net/http"

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
	svc := service.NewLocalAnimeService()
	count := svc.RefreshAllMetadata()

	// Return a JSON trigger header with the success message
	msg := fmt.Sprintf("后台刷新完成，共更新 %d 条元数据", count)

	// Create the header JSON. Since the value is simple, we can construct it manually or use a struct?
	// Manually is fine for simple strings. Note: JSON strings must be quoted.
	// We'll use a simple approach: Trigger a "showToast" event with the message as detail
	// Or pass it as { "showToast": "message" }

	// Since we need to put a string variable inside JSON, let's use Sprintf properly
	// Using custom event name "library-refresh-done" might be cleaner, but "showToast" is what we used in planning.
	// frontend: @htmx:after-request check for header?
	// actually standard way: HX-Trigger: {"showMessage": "text"}

	// Let's use `encoding/json` to be safe
	// URI Encode the message content ONLY to ensure JSON is valid ASCII
	// Front-end will decodeURIComponent
	// Use JSON body for maximum compatibility and robustness
	// This bypasses header exposure issues and event dispatching complexity
	c.JSON(http.StatusOK, gin.H{
		"message": msg,
		"count":   count,
	})
}
