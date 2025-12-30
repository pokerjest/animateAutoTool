package api

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"gorm.io/gorm"
)

type SubscriptionsData struct {
	SkipLayout    bool
	Subscriptions []model.Subscription
}

func SubscriptionsHandler(c *gin.Context) {
	skip := IsHTMX(c)
	var subs []model.Subscription
	if err := db.DB.Preload("Metadata").Find(&subs).Error; err != nil {
		log.Printf("Error fetching subscriptions: %v", err)
	}

	// Populate DownloadedCount
	for i := range subs {
		var count int64
		db.DB.Model(&model.DownloadLog{}).Where("subscription_id = ?", subs[i].ID).Count(&count)
		subs[i].DownloadedCount = count
	}

	data := SubscriptionsData{
		SkipLayout:    skip,
		Subscriptions: subs,
	}
	c.HTML(http.StatusOK, "subscriptions.html", data)
}

func CreateSubscriptionHandler(c *gin.Context) {
	var sub model.Subscription
	if err := c.ShouldBind(&sub); err != nil {
		c.String(http.StatusBadRequest, "Invalid Data: %v", err)
		return
	}

	if err := createSubscriptionInternal(&sub); err != nil {
		if err.Error() == "exists" {
			c.String(http.StatusConflict, "Subscription with this RSS URL already exists")
		} else {
			c.String(http.StatusInternalServerError, err.Error())
		}
		return
	}

	// User requested "silky smooth" transition, wait 1s
	time.Sleep(1 * time.Second)

	c.Header("HX-Redirect", "/subscriptions")
	c.Status(http.StatusOK)
}

func createSubscriptionInternal(sub *model.Subscription) error {
	// Try to extract BangumiID from RSS URL
	if sub.RSSUrl != "" {
		if u, err := url.Parse(sub.RSSUrl); err == nil {
			q := u.Query()
			if bidStr := q.Get("bangumiId"); bidStr != "" {
				if bid, err := strconv.Atoi(bidStr); err == nil {
					if sub.Metadata == nil {
						sub.Metadata = &model.AnimeMetadata{}
					}
					sub.Metadata.BangumiID = bid
				}
			}
		}
	}

	// Auto-fill FilterRule from SubtitleGroup if FilterRule is empty
	if sub.FilterRule == "" && sub.SubtitleGroup != "" {
		sub.FilterRule = sub.SubtitleGroup
	}

	// Enrich Metadata (Bangumi & TMDB)
	metaSvc := service.NewMetadataService()
	metaSvc.EnrichMetadata(sub.Metadata, sub.Title)

	sub.IsActive = true

	// Check if already exists (including soft-deleted)
	var existing model.Subscription
	if err := db.DB.Unscoped().Where("rss_url = ?", sub.RSSUrl).First(&existing).Error; err == nil {
		// Found existing
		if existing.DeletedAt.Valid {
			// Restore it
			existing.DeletedAt = gorm.DeletedAt{} // Restore
			existing.Title = sub.Title
			existing.FilterRule = sub.FilterRule
			existing.ExcludeRule = sub.ExcludeRule
			existing.IsActive = true
			if err := db.DB.Save(&existing).Error; err != nil {
				return fmt.Errorf("failed to restore: %v", err)
			}
			*sub = existing // Update caller's pointer
		} else {
			return fmt.Errorf("exists")
		}
	} else {
		// New creation
		if err := db.DB.Create(sub).Error; err != nil {
			return fmt.Errorf("failed to create: %v", err)
		}
	}

	// Trigger run asynchronously
	go func() {
		log.Printf("DEBUG: Async ProcessSubscription started for %s", sub.Title)
		qbUrl, qbUser, qbPass := FetchQBConfig()
		qbt := downloader.NewQBittorrentClient(qbUrl)
		if err := qbt.Login(qbUser, qbPass); err != nil {
			log.Printf("ERROR: Async QB Login failed: %v", err)
			return
		}
		mgr := service.NewSubscriptionManager(qbt)
		mgr.ProcessSubscription(sub)
	}()

	return nil
}

func CreateBatchSubscriptionHandler(c *gin.Context) {
	var subs []model.Subscription
	if err := c.ShouldBindJSON(&subs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON format"})
		return
	}

	added := 0
	failed := 0
	for i := range subs {
		if err := createSubscriptionInternal(&subs[i]); err != nil {
			log.Printf("Batch add failed for %s: %v", subs[i].Title, err)
			failed++
		} else {
			added++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Batch process completed",
		"added":   added,
		"failed":  failed,
	})
}

// BatchPreviewHandler 并发地为批量添加提供预览
func BatchPreviewHandler(c *gin.Context) {
	var subs []model.Subscription
	if err := c.ShouldBindJSON(&subs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Data"})
		return
	}

	type PreviewResult struct {
		Title    string           `json:"title"`
		RSSUrl   string           `json:"rss_url"`
		Episodes []parser.Episode `json:"episodes,omitempty"`
		Error    string           `json:"error,omitempty"`
	}

	results := make([]PreviewResult, len(subs))
	var wg sync.WaitGroup

	for i, sub := range subs {
		wg.Add(1)
		go func(i int, s model.Subscription) {
			defer wg.Done()
			res := PreviewResult{Title: s.Title, RSSUrl: s.RSSUrl}

			if s.RSSUrl == "" {
				res.Error = "缺少 RSS 链接"
			} else {
				p := parser.NewMikanParser()
				eps, err := p.Parse(s.RSSUrl)
				if err != nil {
					res.Error = err.Error()
				} else {
					// 只取前 5 集预览
					if len(eps) > 5 {
						eps = eps[:5]
					}
					res.Episodes = eps
				}
			}
			results[i] = res
		}(i, sub)
	}

	wg.Wait()
	c.JSON(http.StatusOK, results)
}

func ToggleSubscriptionHandler(c *gin.Context) {
	id := c.Param("id")
	var sub model.Subscription
	if err := db.DB.First(&sub, id).Error; err != nil {
		c.String(http.StatusNotFound, "Not Found")
		return
	}

	sub.IsActive = !sub.IsActive
	db.DB.Save(&sub)

	time.Sleep(1 * time.Second) // Smooth transition

	btnClass := "bg-green-500 hover:bg-green-600"
	btnText := "Running"
	if !sub.IsActive {
		btnClass = "bg-gray-400 hover:bg-gray-500"
		btnText = "Paused"
	}

	// ID for indicator
	toggleID := "toggle-" + strconv.Itoa(int(sub.ID))
	html := `<button id="` + toggleID + `" hx-post="/api/subscriptions/` + strconv.Itoa(int(sub.ID)) + `/toggle" hx-swap="outerHTML" hx-indicator="#` + toggleID + `" class="px-3 py-1 rounded text-white text-xs transition-colors ` + btnClass + ` flex items-center gap-1 disabled:opacity-50">` +
		`<span class="htmx-indicator hidden animate-spin">⌛</span>` +
		`<span class="htmx-request:hidden">` + btnText + `</span>` +
		`</button>`
	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, html)
}

func DeleteSubscriptionHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.String(http.StatusBadRequest, "无效的 ID")
		return
	}

	log.Printf("DEBUG: Deleting subscription ID: %d", id)

	// Start transaction for cascading delete
	tx := db.DB.Begin()

	// 1. Delete associated logs (Hard Delete)
	if err := tx.Unscoped().Where("subscription_id = ?", id).Delete(&model.DownloadLog{}).Error; err != nil {
		tx.Rollback()
		log.Printf("ERROR: Delete logs failed for subID %d: %v", id, err)
		c.String(http.StatusInternalServerError, "删除关联日志失败: "+err.Error())
		return
	}

	// 2. Delete subscription (Hard Delete)
	if err := tx.Unscoped().Delete(&model.Subscription{}, id).Error; err != nil {
		tx.Rollback()
		log.Printf("ERROR: Delete sub failed for ID %d: %v", id, err)
		c.String(http.StatusInternalServerError, "删除订阅失败: "+err.Error())
		return
	}

	if err := tx.Commit().Error; err != nil {
		log.Printf("ERROR: Commit failed: %v", err)
		c.String(http.StatusInternalServerError, "事务提交失败")
		return
	}

	time.Sleep(200 * time.Millisecond) // Slight delay for UI feel
	c.Status(http.StatusOK)
}

func RunSubscriptionHandler(c *gin.Context) {
	id := c.Param("id")
	var sub model.Subscription
	if err := db.DB.First(&sub, id).Error; err != nil {
		c.String(http.StatusNotFound, "Not Found")
		return
	}

	// Fetch QB Config
	qbUrl, qbUser, qbPass := FetchQBConfig()

	// Init Downloader
	qbt := downloader.NewQBittorrentClient(qbUrl)
	if err := qbt.Login(qbUser, qbPass); err != nil {
		log.Printf("RunSubscription: QB Login failed: %v", err)
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, `<script>alert("QBittorrent 连接失败: `+err.Error()+`")</script>`)
		return
	}

	// Init Manager and Run
	mgr := service.NewSubscriptionManager(qbt)
	// Let's do Sync for now as it shouldn't be too long for RSS parse.
	mgr.ProcessSubscription(&sub)

	c.Header("Content-Type", "text/html")
	time.Sleep(1 * time.Second) // Smooth transition
	c.Status(http.StatusOK)
}

func UpdateSubscriptionHandler(c *gin.Context) {
	id := c.Param("id")
	var sub model.Subscription
	if err := db.DB.First(&sub, id).Error; err != nil {
		c.String(http.StatusNotFound, "Not Found")
		return
	}

	var input struct {
		Title       string `form:"Title" binding:"required"`
		RSSUrl      string `form:"RSSUrl" binding:"required"`
		FilterRule  string `form:"FilterRule"`
		ExcludeRule string `form:"ExcludeRule"`
	}

	if err := c.ShouldBind(&input); err != nil {
		c.String(http.StatusBadRequest, "Invalid Data")
		return
	}

	// Check if Title changed, reset metadata ID search if so
	if sub.Title != input.Title {
		sub.Title = input.Title
		// Re-clean title just in case
		// Reset ID and try to find again immediately
		if sub.Metadata != nil {
			sub.Metadata.BangumiID = 0
			bgmClient := bangumi.NewClient("", "", "")
			if res, err := bgmClient.SearchSubject(sub.Title); err == nil && res != nil {
				sub.Metadata.BangumiID = res.ID
			}
		}
	}

	sub.RSSUrl = input.RSSUrl
	sub.FilterRule = input.FilterRule
	sub.ExcludeRule = input.ExcludeRule

	if err := db.DB.Save(&sub).Error; err != nil {
		c.String(http.StatusInternalServerError, "Error saving: "+err.Error())
		return
	}

	// Sleep for smooth UI feel
	time.Sleep(500 * time.Millisecond)

	c.HTML(http.StatusOK, "subscription_card.html", sub)
}

func RefreshSubscriptionMetadataHandler(c *gin.Context) {
	id := c.Param("id")
	var sub model.Subscription
	if err := db.DB.First(&sub, id).Error; err != nil {
		c.String(http.StatusNotFound, "Not Found")
		return
	}

	// Enrich Metadata using shared service
	metaSvc := service.NewMetadataService()
	metaSvc.EnrichMetadata(sub.Metadata, sub.Title)

	if err := db.DB.Save(&sub).Error; err != nil {
		c.String(http.StatusInternalServerError, "Failed to save: "+err.Error())
		return
	}

	// Sleep for smooth UI feel
	time.Sleep(500 * time.Millisecond)

	// Populate DownloadedCount for the card view
	var count int64
	db.DB.Model(&model.DownloadLog{}).Where("subscription_id = ?", sub.ID).Count(&count)
	sub.DownloadedCount = count

	c.HTML(http.StatusOK, "subscription_card.html", sub)
}

// SwitchSubscriptionSourceHandler 切换订阅的数据源并全局同步
func SwitchSubscriptionSourceHandler(c *gin.Context) {
	id := c.Param("id")
	source := c.Query("source")

	var sub model.Subscription
	if err := db.DB.Preload("Metadata").First(&sub, id).Error; err != nil {
		c.String(http.StatusNotFound, "Not Found")
		return
	}

	if sub.Metadata == nil {
		c.String(http.StatusBadRequest, "No metadata associated")
		return
	}

	m := sub.Metadata
	switch source {
	case SourceTMDB:
		if m.TMDBID != 0 {
			m.Title = m.TMDBTitle
			m.Image = m.TMDBImage
			m.Summary = m.TMDBSummary
		}
	case SourceBangumi:
		if m.BangumiID != 0 {
			m.Title = m.BangumiTitle
			m.Image = m.BangumiImage
			m.Summary = m.BangumiSummary
		}
	case SourceAniList:
		if m.AniListID != 0 {
			m.Title = m.AniListTitle
			m.Image = m.AniListImage
			m.Summary = m.AniListSummary
		}
	}

	if err := db.DB.Save(m).Error; err != nil {
		c.String(http.StatusInternalServerError, "Failed to save metadata")
		return
	}

	// Trigger global sync (updates the subscription itself + any local folders)
	metaSvc := service.NewMetadataService()
	metaSvc.SyncMetadataToModels(m)

	c.HTML(http.StatusOK, "subscription_card.html", sub)
}

func SearchAnimeHandler(c *gin.Context) {
	keyword := c.Query("q")
	if keyword == "" {
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, `<div class="p-4 text-center text-gray-500">请输入关键词进行搜索</div>`)
		return
	}

	p := parser.NewMikanParser()
	results, err := p.Search(keyword)
	if err != nil {
		log.Printf("Search error: %v", err)
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, `<div class="p-4 text-center text-red-500">搜索失败: `+err.Error()+`</div>`)
		return
	}

	c.HTML(http.StatusOK, "search_results.html", gin.H{
		"Results": results,
	})
}

func GetSubgroupsHandler(c *gin.Context) {
	bangumiID := c.Query("id")
	if bangumiID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	p := parser.NewMikanParser()
	subgroups, err := p.GetSubgroups(bangumiID)
	if err != nil {
		log.Printf("GetSubgroups error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, subgroups)
}

func PreviewRSSHandler(c *gin.Context) {
	url := c.Query("RSSUrl")
	if url == "" {
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, `<div class="p-4 text-center text-gray-500">请输入有效 RSS 链接</div>`)
		return
	}

	p := parser.NewMikanParser()
	episodes, err := p.Parse(url)
	if err != nil {
		log.Printf("Preview error: %v", err)
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, `<div class="p-4 text-center text-red-500">解析失败: `+err.Error()+`</div>`)
		return
	}

	c.HTML(http.StatusOK, "preview_results.html", gin.H{
		"Episodes": episodes,
	})
}

func GetMikanDashboardHandler(c *gin.Context) {
	year := c.Query("year")
	season := c.Query("season")

	p := parser.NewMikanParser()
	dashboard, err := p.GetDashboard(year, season)
	if err != nil {
		log.Printf("GetMikanDashboard error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, dashboard)
}

func RefreshSubscriptionsHandler(c *gin.Context) {
	var subs []model.Subscription
	if err := db.DB.Preload("Metadata").Find(&subs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch subscriptions"})
		return
	}

	updatedCount := 0
	metaSvc := service.NewMetadataService()

	for i := range subs {
		metaSvc.EnrichMetadata(subs[i].Metadata, subs[i].Title)
		if err := db.DB.Save(&subs[i]).Error; err == nil {
			updatedCount++
		}
		// Be nice to the API
		time.Sleep(200 * time.Millisecond)
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("刷新完成，更新了 %d 个订阅的元数据", updatedCount),
		"updated": updatedCount,
		"total":   len(subs),
	})
}
