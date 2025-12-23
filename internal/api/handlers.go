package api

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"gorm.io/gorm"
)

type DashboardData struct {
	SkipLayout     bool
	ActiveSubs     int64
	TodayDownloads int64
}

type SubscriptionsData struct {
	SkipLayout    bool
	Subscriptions []model.Subscription
}

func isHTMX(c *gin.Context) bool {
	return c.GetHeader("HX-Request") == "true"
}

// Helper to reliably fetch QB config without GORM scope issues
func fetchQBConfig() (string, string, string) {
	var configs []model.GlobalConfig
	// Fetch all to avoid scope pollution from sequential First() calls
	if err := db.DB.Find(&configs).Error; err != nil {
		log.Printf("Error fetching configs: %v", err)
		return "http://localhost:8080", "", ""
	}

	cfgMap := make(map[string]string)
	for _, c := range configs {
		cfgMap[c.Key] = c.Value
	}

	url := cfgMap[model.ConfigKeyQBUrl]
	if url == "" {
		url = "http://localhost:8080"
	}
	return url, cfgMap[model.ConfigKeyQBUsername], cfgMap[model.ConfigKeyQBPassword]
}

// === Dashboard ===

func DashboardHandler(c *gin.Context) {
	skip := isHTMX(c)

	var activeSubs int64
	db.DB.Model(&model.Subscription{}).Where("is_active = ?", true).Count(&activeSubs)

	var totalDownloads int64
	db.DB.Model(&model.DownloadLog{}).Count(&totalDownloads)

	data := DashboardData{
		SkipLayout:     skip,
		ActiveSubs:     activeSubs,
		TodayDownloads: totalDownloads,
	}

	c.HTML(http.StatusOK, "index.html", data)
}

// === Subscriptions ===

func SubscriptionsHandler(c *gin.Context) {
	skip := isHTMX(c)
	var subs []model.Subscription
	if err := db.DB.Find(&subs).Error; err != nil {
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
		qbUrl, qbUser, qbPass := fetchQBConfig()
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
	qbUrl, qbUser, qbPass := fetchQBConfig()

	// Init Downloader
	qbt := downloader.NewQBittorrentClient(qbUrl)
	if err := qbt.Login(qbUser, qbPass); err != nil {
		log.Printf("RunSubscription: QB Login failed: %v", err)
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, `<script>alert("QBittorrent 连接失败: `+err.Error()+`")</script>`)
		return
	}

	// Init Manager and Run
	// Init Manager and Run
	mgr := service.NewSubscriptionManager(qbt)
	// Let's do Sync for now as it shouldn't be too long for RSS parse.
	mgr.ProcessSubscription(&sub)

	c.Header("Content-Type", "text/html")
	// Return a toast notification using OOB swap or just a script?
	// Since we swapped "none", we can just return a script or empty.
	// But to show feedback, let's use a little script or alert.
	// But to show feedback, let's use a little script or alert.
	// c.String(http.StatusOK, `<script>alert("已触发订阅检查: `+sub.Title+`")</script>`)
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

	if err := c.ShouldBind(&sub); err != nil {
		c.String(http.StatusBadRequest, "Invalid Data")
		return
	}

	// Update specific fields avoids overwriting others if not present in form
	// But `ShouldBind` maps form to struct.
	// We should be careful about zero values if form is partial.
	// Here the form contains all editable fields.

	if err := db.DB.Save(&sub).Error; err != nil {
		c.String(http.StatusInternalServerError, "Error saving: "+err.Error())
		return
	}

	time.Sleep(1 * time.Second) // Smooth transition

	c.HTML(http.StatusOK, "subscription_card.html", sub)
}

// === Settings ===

func SettingsHandler(c *gin.Context) {
	skip := isHTMX(c)

	var configs []model.GlobalConfig
	if err := db.DB.Find(&configs).Error; err != nil {
		log.Printf("Error fetching configs: %v", err)
	}

	// Debug Log
	log.Printf("DEBUG: Fetched %d configs", len(configs))
	for _, cfg := range configs {
		log.Printf("DEBUG: Config %s => %s", cfg.Key, cfg.Value)
	}

	configMap := make(map[string]string)
	for _, cfg := range configs {
		configMap[cfg.Key] = cfg.Value
	}

	if _, ok := configMap[model.ConfigKeyQBUrl]; !ok {
		configMap[model.ConfigKeyQBUrl] = "http://localhost:8080"
	}

	c.HTML(http.StatusOK, "settings.html", gin.H{
		"SkipLayout": skip,
		"Config":     configMap,
	})
}

func UpdateSettingsHandler(c *gin.Context) {
	keys := []string{
		model.ConfigKeyQBUrl,
		model.ConfigKeyQBUsername,
		model.ConfigKeyQBPassword,
		model.ConfigKeyBaseDir,
	}

	for _, key := range keys {
		val := c.PostForm(key)
		log.Printf("DEBUG: UpdateSettings - Key: %s, Val: '%s'", key, val)

		// Manual Upsert to ensure it works
		var count int64
		db.DB.Model(&model.GlobalConfig{}).Where("key = ?", key).Count(&count)

		if count == 0 {
			// Create
			if err := db.DB.Create(&model.GlobalConfig{Key: key, Value: val}).Error; err != nil {
				log.Printf("ERROR creating config %s: %v", key, err)
			}
		} else {
			// Update
			// Note: Use map to update to allow empty string updates?
			// Update("value", val)
			if err := db.DB.Model(&model.GlobalConfig{}).Where("key = ?", key).Update("value", val).Error; err != nil {
				log.Printf("ERROR updating config %s: %v", key, err)
			}
		}
	}

	// Add artificial delay for smooth UI transition
	time.Sleep(1 * time.Second)

	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, `<div class="text-emerald-600 font-medium flex items-center gap-2 animate-fade-in-up">✅ 配置保存成功</div>`)
}

func TestConnectionHandler(c *gin.Context) {
	url := c.PostForm("qb_url")
	username := c.PostForm("qb_username")
	password := c.PostForm("qb_password")

	if url == "" {
		c.String(http.StatusBadRequest, `<span class="text-red-500 font-bold">❌ URL 不能为空</span>`)
		return
	}

	log.Printf("DEBUG: TestConnectionHandler called with URL: %s", url)

	client := downloader.NewQBittorrentClient(url)
	c.Header("Content-Type", "text/html")
	if err := client.Login(username, password); err != nil {
		log.Printf("DEBUG: Login failed: %v", err)
		c.String(http.StatusOK, `<div class="text-red-600 bg-red-50 px-3 py-1.5 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200 shadow-sm animate-pulse">❌ 连接失败: `+err.Error()+`</div>`)
		return
	}

	version, err := client.GetVersion()
	if err != nil {
		log.Printf("DEBUG: GetVersion failed: %v", err)
		c.String(http.StatusOK, `<div class="text-amber-600 bg-amber-50 px-3 py-1.5 rounded-lg text-sm font-medium flex items-center gap-2 border border-amber-200 shadow-sm">⚠️ 登录成功但获取版本失败: `+err.Error()+`</div>`)
		return
	}

	log.Printf("DEBUG: Connection successful, version: %s", version)
	c.String(http.StatusOK, `<div class="text-emerald-600 bg-emerald-50 px-3 py-1.5 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200 shadow-sm transition-all duration-300 transform scale-100">✅ 连接成功! (`+version+`)</div>`)
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
