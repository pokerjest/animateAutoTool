package api

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
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

type DashboardData struct {
	SkipLayout     bool
	ActiveSubs     int64
	TodayDownloads int64
	QBConnected    bool
	QBVersion      string
	BangumiLogin   bool
	TMDBConnected  bool
	WatchingList   []bangumi.UserCollectionItem
	CompletedList  []bangumi.UserCollectionItem
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

	// Check QB Status
	qbUrl, qbUser, qbPass := fetchQBConfig()
	var qbConnected bool
	var qbVersion string
	if qbUrl != "" {
		qbt := downloader.NewQBittorrentClient(qbUrl)
		if err := qbt.Login(qbUser, qbPass); err == nil {
			if ver, err := qbt.GetVersion(); err == nil {
				qbConnected = true
				qbVersion = ver
			}
		}
	}

	// Check Bangumi Status & Fetch Data
	// Check Bangumi Status & Fetch Data
	var bangumiLogin bool
	var watchingList []bangumi.UserCollectionItem
	var completedList []bangumi.UserCollectionItem
	var tokenConfig model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyBangumiAccessToken).First(&tokenConfig).Error; err == nil && tokenConfig.Value != "" {
		bangumiLogin = true

		// Fetch Collection
		// Note: Ideally we should cache this or do it async to not block dashboard load
		// But for now, let's do it synchronously with a short timeout context or just rely on Resty timeout
		client := bangumi.NewClient("", "", "")
		user, err := client.GetCurrentUser(tokenConfig.Value)
		if err == nil {
			// Use 'me' or username. Let's use user.Username

			// 1. Fetch Watching (Type 3)
			watching, err1 := client.GetUserCollection(tokenConfig.Value, user.Username, 3, 12, 0)
			if err1 != nil {
				log.Printf("Error fetching watching collection: %v", err1)
			} else {
				watchingList = watching
			}

			// 2. Fetch Completed (Type 2) - Limit 12 (Recent)
			completed, err2 := client.GetUserCollection(tokenConfig.Value, user.Username, 2, 12, 0)
			if err2 != nil {
				log.Printf("Error fetching completed collection: %v", err2)
			} else {
				completedList = completed
			}
		} else {
			log.Printf("Error fetching user profile: %v", err)
		}
	}

	// Check TMDB Status (Simple check if configured)
	var tmdbConnected bool
	var tmdbConfig model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyTMDBToken).First(&tmdbConfig).Error; err == nil && tmdbConfig.Value != "" {
		tmdbConnected = true
	}

	data := DashboardData{
		SkipLayout:     skip,
		ActiveSubs:     activeSubs,
		TodayDownloads: totalDownloads,
		QBConnected:    qbConnected,
		QBVersion:      qbVersion,
		BangumiLogin:   bangumiLogin,
		TMDBConnected:  tmdbConnected,
		WatchingList:   watchingList,
		CompletedList:  completedList,
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
	// Try to extract BangumiID from RSS URL
	if sub.RSSUrl != "" {
		if u, err := url.Parse(sub.RSSUrl); err == nil {
			q := u.Query()
			if bidStr := q.Get("bangumiId"); bidStr != "" {
				if bid, err := strconv.Atoi(bidStr); err == nil {
					sub.BangumiID = bid
				}
			}
		}
	}

	// Fallback: Search by title if no ID found
	if sub.BangumiID == 0 && sub.Title != "" {
		bgmClient := bangumi.NewClient("", "", "")
		if res, err := bgmClient.SearchSubject(sub.Title); err == nil && res != nil {
			sub.BangumiID = res.ID
		}
	}

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

	// Check if Title changed, reset BangumiID if so
	if sub.Title != input.Title {
		sub.Title = input.Title
		// Re-clean title just in case
		// Reset ID and try to find again immediately
		sub.BangumiID = 0
		bgmClient := bangumi.NewClient("", "", "")
		if res, err := bgmClient.SearchSubject(sub.Title); err == nil && res != nil {
			sub.BangumiID = res.ID
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

	// Re-search Bangumi
	bgmClient := bangumi.NewClient("", "", "")
	if res, err := bgmClient.SearchSubject(sub.Title); err == nil && res != nil {
		sub.BangumiID = res.ID
		if err := db.DB.Save(&sub).Error; err != nil {
			c.String(http.StatusInternalServerError, "Failed to save: "+err.Error())
			return
		}
	} else {
		// Optional: clear ID if not found? Or keep old?
		// For now, let's keep old if search fails, but maybe log it.
		// If user explicitly asks to refresh, maybe they expect a fix.
		// But if network fails, we shouldn't wipe it.
		// Let's only update if we found a valid ID.
		log.Printf("RefreshMetadata: No match found for %s", sub.Title)
	}

	// Sleep for smooth UI feel
	time.Sleep(500 * time.Millisecond)

	// Populate DownloadedCount for the card view
	var count int64
	db.DB.Model(&model.DownloadLog{}).Where("subscription_id = ?", sub.ID).Count(&count)
	sub.DownloadedCount = count

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
	// 1. Save All Configs
	keys := []string{
		model.ConfigKeyQBUrl,
		model.ConfigKeyQBUsername,
		model.ConfigKeyQBPassword,
		model.ConfigKeyBaseDir,
		model.ConfigKeyBangumiAccessToken,
		model.ConfigKeyTMDBToken,
		model.ConfigKeyProxyURL,
		model.ConfigKeyProxyBangumi,
		model.ConfigKeyProxyTMDB,
	}

	for _, key := range keys {
		val, exists := c.GetPostForm(key)
		if !exists {
			continue
		}

		log.Printf("DEBUG: UpdateSettings - Key: %s, Val: '%s'", key, val)

		// Manual Upsert
		var count int64
		db.DB.Model(&model.GlobalConfig{}).Where("key = ?", key).Count(&count)

		if count == 0 {
			db.DB.Create(&model.GlobalConfig{Key: key, Value: val})
		} else {
			db.DB.Model(&model.GlobalConfig{}).Where("key = ?", key).Update("value", val)
		}
	}

	// 2. Add artificial delay for smooth UI transition
	time.Sleep(500 * time.Millisecond)

	// 3. Test QB Connection
	url := c.PostForm("qb_url")
	username := c.PostForm("qb_username")
	password := c.PostForm("qb_password")

	qbStatusHtml := ""
	if url != "" {
		client := downloader.NewQBittorrentClient(url)
		if err := client.Login(username, password); err != nil {
			qbStatusHtml = fmt.Sprintf(`<div id="qb-status" hx-swap-oob="innerHTML"><div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ QB 连接失败: %v</div></div>`, err)
		} else {
			if ver, err := client.GetVersion(); err == nil {
				qbStatusHtml = fmt.Sprintf(`<div id="qb-status" hx-swap-oob="innerHTML"><div class="text-emerald-600 bg-emerald-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200">✅ QB 已连接 (版本: %s)</div></div>`, ver)
			} else {
				qbStatusHtml = `<div id="qb-status" hx-swap-oob="innerHTML"><div class="text-amber-600 bg-amber-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-amber-200">⚠️ QB 登录成功但版本未知</div></div>`
			}
		}
	} else {
		qbStatusHtml = `<div id="qb-status" hx-swap-oob="innerHTML"><div class="text-sm text-gray-400 flex items-center gap-2"><span>⚪ 未配置 QB URL</span></div></div>`
	}

	// 4. Trigger Bangumi Refresh OOB
	bangumiStatusOOB := RenderBangumiStatusOOB()

	// 5. Trigger TMDB Refresh OOB
	tmdbStatusOOB := RenderTMDBStatusOOB()

	c.Header("Content-Type", "text/html")
	// Main response for #save-result + OOB blocks
	c.String(http.StatusOK, fmt.Sprintf(`
		<div class="text-emerald-600 font-bold flex items-center gap-2 animate-pulse">✅ 所有配置已保存</div>
		%s
		%s
		%s
	`, qbStatusHtml, bangumiStatusOOB, tmdbStatusOOB))
}

// QBSaveAndTestHandler saves QB settings and then tests connection
func QBSaveAndTestHandler(c *gin.Context) {
	// 1. Save Config
	qbKeys := []string{
		model.ConfigKeyQBUrl,
		model.ConfigKeyQBUsername,
		model.ConfigKeyQBPassword,
		model.ConfigKeyBaseDir,
	}
	// Reusing logic from UpdateSettings, simplified
	for _, key := range qbKeys {
		val, exists := c.GetPostForm(key)
		if !exists {
			continue
		}

		var count int64
		db.DB.Model(&model.GlobalConfig{}).Where("key = ?", key).Count(&count)
		if count == 0 {
			db.DB.Create(&model.GlobalConfig{Key: key, Value: val})
		} else {
			db.DB.Model(&model.GlobalConfig{}).Where("key = ?", key).Update("value", val)
		}
	}

	// 2. Test Connection
	url := c.PostForm("qb_url")
	username := c.PostForm("qb_username")
	password := c.PostForm("qb_password")

	if url == "" {
		c.String(http.StatusOK, `<div class="text-amber-500 font-medium flex items-center gap-2">⚠️ 保存成功，但 URL 为空，无法测试连接</div>`)
		return
	}

	client := downloader.NewQBittorrentClient(url)
	if err := client.Login(username, password); err != nil {
		log.Printf("DEBUG: Login failed: %v", err)
		c.String(http.StatusOK, `<div class="text-red-600 bg-red-50 px-3 py-1.5 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200 shadow-sm animate-pulse">💾 保存成功 | ❌ 连接失败: `+err.Error()+`</div>`)
		return
	}

	version, err := client.GetVersion()
	statusMsg := ""
	if err != nil {
		statusMsg = fmt.Sprintf("⚠️ 保存成功 | 登录成功但获取版本失败: %v", err)
	} else {
		statusMsg = fmt.Sprintf("✅ 保存成功 | 🔌 连接正常 (%s)", version)
	}

	c.String(http.StatusOK, fmt.Sprintf(`<div class="text-emerald-600 bg-emerald-50 px-3 py-1.5 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200 shadow-sm transition-all duration-300 transform scale-100">%s</div>`, statusMsg))
}

// BangumiSaveHandler saves Bangumi settings and returns success
func BangumiSaveHandler(c *gin.Context) {
	keys := []string{
		model.ConfigKeyBangumiAppID,
		model.ConfigKeyBangumiAppSecret,
	}

	for _, key := range keys {
		val, exists := c.GetPostForm(key)
		if !exists {
			continue
		}

		var count int64
		db.DB.Model(&model.GlobalConfig{}).Where("key = ?", key).Count(&count)
		if count == 0 {
			db.DB.Create(&model.GlobalConfig{Key: key, Value: val})
		} else {
			db.DB.Model(&model.GlobalConfig{}).Where("key = ?", key).Update("value", val)
		}
	}

	// Trigger Profile Refresh on the client using HTMX OOB or by having the client listen to event.
	// We'll return the success message AND a trigger header (if simple) or script?
	// Easiest is to return success message, and rely on `BangumiProfileHandler` being re-triggered.
	// Since we are returning HTML to a target div (#bangumi-save-result), we can't easily swap #bangumi-status unless we use OOB.
	// Let's use OOB swap to refresh the profile status simultaneously.

	// Construct success message
	successHtml := `<div class="text-emerald-600 font-medium flex items-center gap-2 animate-fade-in-up">✅ 保存成功</div>`

	// OOB Swap for status div: trigger a reload by swapping a script or replacement?
	// HTMX doesn't have "Reload this other element" OOB easily without replacing it.
	// However, if we replace #bangumi-status with itself but with `hx-trigger="load"`, it will reload.
	// Or we can just include the updated profile content via OOB if we call the handler logic?
	// Let's just return a script to trigger the reload of #bangumi-status.
	triggerScript := `<div hx-swap-oob="true" id="bangumi-refresh-trigger" hx-get="/api/bangumi/profile" hx-target="#bangumi-status" hx-trigger="load" class="hidden"></div>`

	c.String(http.StatusOK, successHtml+triggerScript)
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

// GetQBStatusHandler tests connection using stored config
func GetQBStatusHandler(c *gin.Context) {
	var configs []model.GlobalConfig
	// Fetch only QB related configs
	db.DB.Where("key IN ?", []string{model.ConfigKeyQBUrl, model.ConfigKeyQBUsername, model.ConfigKeyQBPassword}).Find(&configs)

	configMap := make(map[string]string)
	for _, cfg := range configs {
		configMap[cfg.Key] = cfg.Value
	}

	url := configMap[model.ConfigKeyQBUrl]
	username := configMap[model.ConfigKeyQBUsername]
	password := configMap[model.ConfigKeyQBPassword]

	if url == "" {
		c.String(http.StatusOK, `<div class="text-sm text-gray-400 flex items-center gap-2"><span>⚪ 未配置 QB URL</span></div>`)
		return
	}

	client := downloader.NewQBittorrentClient(url)
	if err := client.Login(username, password); err != nil {
		c.String(http.StatusOK, fmt.Sprintf(`<div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ QB 连接失败: %v</div>`, err))
		return
	}

	if ver, err := client.GetVersion(); err == nil {
		c.String(http.StatusOK, fmt.Sprintf(`<div class="text-emerald-600 bg-emerald-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200">✅ QB 已连接 (版本: %s)</div>`, ver))
	} else {
		c.String(http.StatusOK, `<div class="text-amber-600 bg-amber-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-amber-200">⚠️ QB 登录成功但版本未知</div>`)
	}
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
	if err := db.DB.Find(&subs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch subscriptions"})
		return
	}

	updatedCount := 0
	bgmClient := bangumi.NewClient("", "", "") // Anonymous client

	// Compile regex for cleaning title: remove leading [...]
	re := regexp.MustCompile(`^\[.*?\]\s*`)

	for _, sub := range subs {
		// Only try to update if BangumiID is missing
		if sub.BangumiID == 0 {
			queryTitle := sub.Title
			cleaned := false

			// Try cleaning title if it looks like [Subgroup] Title
			if re.MatchString(queryTitle) {
				cleanTitle := re.ReplaceAllString(queryTitle, "")
				if cleanTitle != "" {
					queryTitle = cleanTitle
					cleaned = true
				}
			}

			// Search by title (cleaned or original)
			log.Printf("DEBUG: Refresh - Searching Bangumi for: '%s'", queryTitle)
			if res, err := bgmClient.SearchSubject(queryTitle); err == nil && res != nil {
				log.Printf("DEBUG: Refresh - Found ID: %d", res.ID)
				sub.BangumiID = res.ID
				// If we successfully matched with a cleaned title, update the title in DB too
				if cleaned {
					sub.Title = queryTitle
				}
				if err := db.DB.Save(&sub).Error; err == nil {
					updatedCount++
				}
			}
			// Be nice to the API
			time.Sleep(500 * time.Millisecond)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("刷新完成，更新了 %d 个订阅的元数据", updatedCount),
		"updated": updatedCount,
		"total":   len(subs),
	})
}

func GetBangumiSubjectHandler(c *gin.Context) {
	idStr := c.Param("id")
	if idStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id format"})
		return
	}

	// Just use anonymous client for public data
	client := bangumi.NewClient("", "", "")
	subject, err := client.GetSubject(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to fetch subject: %v", err)})
		return
	}

	c.JSON(http.StatusOK, subject)
}
func GetTMDBStatusHandler(c *gin.Context) {
	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, RenderTMDBStatusOOBNoSwap())
}

// RenderTMDBStatusOOB returns the OOB swap HTML for TMDB status
func RenderTMDBStatusOOB() string {
	content := RenderTMDBStatusOOBNoSwap()
	// Wrap with OOB swap attribute
	return strings.Replace(content, `id="tmdb-status"`, `id="tmdb-status" hx-swap-oob="innerHTML"`, 1)
}

// RenderTMDBStatusOOBNoSwap returns the inner HTML for TMDB status (for direct GET)
func RenderTMDBStatusOOBNoSwap() string {
	var config model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyTMDBToken).First(&config).Error; err != nil || config.Value == "" {
		return `<div id="tmdb-status"><div class="text-sm text-gray-500 flex items-center gap-2"><span>🔴 未连接</span><span class="text-xs text-gray-400">(请先输入 Token 并保存)</span></div></div>`
	}

	token := config.Value
	req, _ := http.NewRequest("GET", "https://api.themoviedb.org/3/account", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	// Check Proxy
	var proxyConfig model.GlobalConfig
	var proxyEnabled model.GlobalConfig
	var transport *http.Transport

	db.DB.Where("key = ?", model.ConfigKeyProxyTMDB).First(&proxyEnabled)

	if proxyEnabled.Value == "true" {
		if err := db.DB.Where("key = ?", model.ConfigKeyProxyURL).First(&proxyConfig).Error; err == nil && proxyConfig.Value != "" {
			if proxyUrl, err := url.Parse(proxyConfig.Value); err == nil {
				transport = &http.Transport{Proxy: http.ProxyURL(proxyUrl)}
			}
		}
	}

	client := &http.Client{
		Timeout:   10 * time.Second, // Increase timeout for proxy
		Transport: transport,
	}
	resp, err := client.Do(req)

	if err != nil {
		log.Printf("TMDB Request Error: %v", err)
		return fmt.Sprintf(`<div id="tmdb-status"><div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ 连接失败: %v</div></div>`, err)
	}
	defer resp.Body.Close()

	log.Printf("TMDB Response Status: %d", resp.StatusCode)
	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("TMDB Response Body: %s", string(bodyBytes))
	}

	if resp.StatusCode == 200 {
		return `<div id="tmdb-status"><div class="text-emerald-600 bg-emerald-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200">✅ 已连接 TMDB</div></div>`
	} else if resp.StatusCode == 401 {
		return `<div id="tmdb-status"><div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ 认证失败 (Token 无效)</div></div>`
	}

	return fmt.Sprintf(`<div id="tmdb-status"><div class="text-amber-600 bg-amber-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-amber-200">⚠️ 连接异常 (HTTP %d)</div></div>`, resp.StatusCode)
}
