package api

import (
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"bytes"
	"encoding/hex"
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/alist"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"gorm.io/gorm"
)

type DashboardData struct {
	SkipLayout        bool
	ActiveSubs        int64
	TodayDownloads    int64
	QBConnected       bool
	QBVersion         string
	BangumiLogin      bool
	TMDBConnected     bool
	JellyfinConnected bool
	WatchingList      []bangumi.UserCollectionItem
	CompletedList     []bangumi.UserCollectionItem
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

	// Check Jellyfin Status (Simple check if configured)
	var jellyfinConnected bool
	var jellyfinConfig model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyJellyfinUrl).First(&jellyfinConfig).Error; err == nil && jellyfinConfig.Value != "" {
		jellyfinConnected = true
	}

	data := DashboardData{
		SkipLayout:        skip,
		ActiveSubs:        activeSubs,
		TodayDownloads:    totalDownloads,
		QBConnected:       qbConnected,
		QBVersion:         qbVersion,
		BangumiLogin:      bangumiLogin,
		TMDBConnected:     tmdbConnected,
		JellyfinConnected: jellyfinConnected,
		WatchingList:      watchingList,
		CompletedList:     completedList,
	}

	c.HTML(http.StatusOK, "index.html", data)
}

// === Subscriptions ===

func SubscriptionsHandler(c *gin.Context) {
	skip := isHTMX(c)
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
	svc := service.NewLocalAnimeService()
	svc.EnrichSubscriptionMetadata(sub)

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
	svc := service.NewLocalAnimeService()
	svc.EnrichSubscriptionMetadata(&sub)

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
	case "tmdb":
		if m.TMDBID != 0 {
			m.Title = m.TMDBTitle
			m.Image = m.TMDBImage
			m.Summary = m.TMDBSummary
		}
	case "bangumi":
		if m.BangumiID != 0 {
			m.Title = m.BangumiTitle
			m.Image = m.BangumiImage
			m.Summary = m.BangumiSummary
		}
	case "anilist":
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
	svc := service.NewLocalAnimeService()
	svc.SyncMetadataToModels(m)

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
		model.ConfigKeyBangumiRefreshToken,
		model.ConfigKeyTMDBToken,
		model.ConfigKeyAniListToken,
		model.ConfigKeyProxyURL,
		model.ConfigKeyProxyBangumi,
		model.ConfigKeyProxyTMDB,
		model.ConfigKeyProxyAniList,
		model.ConfigKeyJellyfinUrl,
		model.ConfigKeyJellyfinApiKey,
		model.ConfigKeyJellyfinApiKey,
		model.ConfigKeyProxyJellyfin,
		model.ConfigKeyPikPakUsername,
		model.ConfigKeyPikPakPassword,
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

	// 1.5 Handle Checkboxes (Unchecked ones won't be in form)
	checkboxes := []string{
		model.ConfigKeyProxyBangumi,
		model.ConfigKeyProxyTMDB,
		model.ConfigKeyProxyAniList,
		model.ConfigKeyProxyJellyfin,
	}
	for _, key := range checkboxes {
		if _, exists := c.GetPostForm(key); !exists {
			db.DB.Model(&model.GlobalConfig{}).Where("key = ?", key).Update("value", "false")
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

	// 6. Trigger AniList Refresh OOB
	anilistStatusOOB := RenderAniListStatusOOB()

	// 7. Trigger Jellyfin Refresh OOB
	jellyfinStatusOOB := RenderJellyfinStatusOOB()

	c.Header("Content-Type", "text/html")
	// Main response for #save-result + OOB blocks
	c.String(http.StatusOK, fmt.Sprintf(`
		<div class="text-emerald-600 font-bold flex items-center gap-2 animate-pulse">✅ 所有配置已保存</div>
		%s
		%s
		%s
		%s
		%s
		%s
	`, qbStatusHtml, bangumiStatusOOB, tmdbStatusOOB, anilistStatusOOB, jellyfinStatusOOB))
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

// PikPakSyncHandler synchronizes PikPak settings to AList
func PikPakSyncHandler(c *gin.Context) {
	username := c.PostForm("pikpak_username")
	password := c.PostForm("pikpak_password")
	refreshToken := c.PostForm("pikpak_refresh_token")

	if username == "" || password == "" {
		c.String(http.StatusOK, `<span class="text-red-500">❌ 请先填写 PikPak 用户名和密码</span>`)
		return
	}

	err := alist.AddPikPakStorage(username, password, refreshToken)
	if err != nil {
		log.Printf("Failed to sync PikPak to AList: %v", err)

		// Check for specific PikPak verification error
		errStr := err.Error()
		if strings.Contains(errStr, "need verify") || strings.Contains(errStr, "Click Here") {
			// Extract the link if possible, or just print the raw HTML error which already contains an <a> tag
			// Clean up the error prefix "failed to create storage: ..."
			if idx := strings.Index(errStr, "need verify"); idx != -1 {
				errStr = errStr[idx:]
			}
			c.String(http.StatusOK, fmt.Sprintf(`<div class="bg-yellow-50 border border-yellow-200 rounded-lg p-3 text-sm text-yellow-800 flex flex-col gap-2">
                <div class="font-bold flex items-center gap-2">⚠️ 需要验证</div>
                <div>由于 PikPak 安全策略，首次登录可能需要验证。</div>
                <div class="text-blue-600 underline">%s</div>
                <div class="text-xs text-yellow-600 mt-1">验证完成后，请再次点击“保存并连接”。</div>
             </div>`, errStr))
			return
		}

		c.String(http.StatusOK, fmt.Sprintf(`<span class="text-red-500">❌ 同步失败: %s</span>`, err.Error()))
		return
	}

	c.Header("HX-Trigger", "pikpak-synced")
	c.String(http.StatusOK, `<span class="text-emerald-600">✅ PikPak 已成功挂载到 AList (/PikPak)</span>`)
}

func GetPikPakStatusHandler(c *gin.Context) {
	status, err := alist.GetPikPakStatus()

	// Simulate Jellyfin rendering style
	if err != nil {
		c.String(http.StatusOK, fmt.Sprintf(`<div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ Error: %s</div>`, err.Error()))
		return
	}

	if status == "work" || status == "WORK" {
		c.String(http.StatusOK, `<div class="text-emerald-600 bg-emerald-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200">✅ 运行正常</div>`)
		return
	} else if status == "未配置" {
		// Initial state or not configured
		c.String(http.StatusOK, `<div class="text-sm text-gray-500 flex items-center gap-2"><span>🔴 未配置</span><span class="text-xs text-gray-400">(请填写账号密码并点击保存连接)</span></div>`)
		return
	}

	// Other non-work status
	c.String(http.StatusOK, fmt.Sprintf(`<div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ 状态: %s</div>`, status))
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

// Status Caching
var statusCache sync.Map

type cachedStatus struct {
	Success    bool
	Msg        string
	Msg2       string // Extra info
	ConfigHash string
	Expiry     time.Time
}

func getCacheHash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// GetQBStatusHandler tests connection using stored config with caching
func GetQBStatusHandler(c *gin.Context) {
	var configs []model.GlobalConfig
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

	// Calculate Hash
	hash := getCacheHash(url, username, password)

	// Check Cache
	if val, ok := statusCache.Load("qb"); ok {
		stat := val.(cachedStatus)
		if stat.ConfigHash == hash && time.Now().Before(stat.Expiry) {
			if stat.Success {
				c.String(http.StatusOK, fmt.Sprintf(`<div class="text-emerald-600 bg-emerald-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200">✅ QB 已连接 (缓存) %s</div>`, stat.Msg))
			} else {
				c.String(http.StatusOK, fmt.Sprintf(`<div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ QB 连接失败 (缓存): %s</div>`, stat.Msg))
			}
			return
		}
	}

	client := downloader.NewQBittorrentClient(url)
	if err := client.Login(username, password); err != nil {
		// Cache Failure
		statusCache.Store("qb", cachedStatus{
			Success:    false,
			Msg:        err.Error(),
			ConfigHash: hash,
			Expiry:     time.Now().Add(1 * time.Minute), // Short cache for errors
		})
		c.String(http.StatusOK, fmt.Sprintf(`<div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ QB 连接失败: %v</div>`, err))
		return
	}

	if ver, err := client.GetVersion(); err == nil {
		// Cache Success
		statusCache.Store("qb", cachedStatus{
			Success:    true,
			Msg:        fmt.Sprintf("(版本: %s)", ver),
			ConfigHash: hash,
			Expiry:     time.Now().Add(5 * time.Minute),
		})
		c.String(http.StatusOK, fmt.Sprintf(`<div class="text-emerald-600 bg-emerald-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200">✅ QB 已连接 (版本: %s)</div>`, ver))
	} else {
		// Status Unknown?
		statusCache.Store("qb", cachedStatus{
			Success:    false, // Treat as warning/fail for cache?
			Msg:        "Version unknown",
			ConfigHash: hash,
			Expiry:     time.Now().Add(1 * time.Minute),
		})
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
	if err := db.DB.Preload("Metadata").Find(&subs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch subscriptions"})
		return
	}

	updatedCount := 0
	svc := service.NewLocalAnimeService()

	for i := range subs {
		svc.EnrichSubscriptionMetadata(&subs[i])
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
	style := c.Query("style")
	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, RenderTMDBStatus(style))
}

// RenderTMDBStatusOOB returns the OOB swap HTML for TMDB status (Settings page)
func RenderTMDBStatusOOB() string {
	content := RenderTMDBStatus("")
	// Wrap with OOB swap attribute
	return strings.Replace(content, `id="tmdb-status"`, `id="tmdb-status" hx-swap-oob="innerHTML"`, 1)
}

// RenderTMDBStatus returns HTML based on style ("" or "dashboard")
func RenderTMDBStatus(style string) string {
	connected, errStr := CheckTMDBConnection()

	// Dashboard Style
	if style == "dashboard" {
		if connected {
			return `<span class="text-emerald-600 font-bold flex items-center gap-1"><span class="w-2 h-2 rounded-full bg-emerald-500"></span> 已连接</span>`
		}
		errText := "未连接"
		if strings.Contains(errStr, "Token") {
			errText = "未连接 (Token无效)"
		} else if errStr != "" {
			errText = "连接失败"
		}
		return fmt.Sprintf(`<span class="text-red-500 font-bold flex items-center gap-1" title="%s"><span class="w-2 h-2 rounded-full bg-red-500"></span> %s</span>`, errStr, errText)
	}

	// Default Settings Page Style (The Pill)
	if connected {
		return `<div id="tmdb-status"><div class="text-emerald-600 bg-emerald-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200">✅ 已连接 TMDB</div></div>`
	}

	// Error cases
	if errStr == "Token missing" {
		return `<div id="tmdb-status"><div class="text-sm text-gray-500 flex items-center gap-2"><span>🔴 未连接</span><span class="text-xs text-gray-400">(请先输入 Token 并保存)</span></div></div>`
	}
	if strings.Contains(errStr, "Token Invalid") {
		return `<div id="tmdb-status"><div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ 认证失败 (Token 无效)</div></div>`
	}

	return fmt.Sprintf(`<div id="tmdb-status"><div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ 连接失败: %s</div></div>`, errStr)
}

func CheckTMDBConnection() (bool, string) {
	var config model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyTMDBToken).First(&config).Error; err != nil || config.Value == "" {
		return false, "Token missing"
	}

	token := config.Value

	// Cache Check
	// We also need proxy config for hash
	var proxyEnabled model.GlobalConfig
	var proxyConfig model.GlobalConfig
	db.DB.Where("key = ?", model.ConfigKeyProxyTMDB).First(&proxyEnabled)
	if proxyEnabled.Value == "true" {
		db.DB.Where("key = ?", model.ConfigKeyProxyURL).First(&proxyConfig)
	}

	hash := getCacheHash(token, proxyEnabled.Value, proxyConfig.Value)
	if val, ok := statusCache.Load("tmdb"); ok {
		stat := val.(cachedStatus)
		if stat.ConfigHash == hash && time.Now().Before(stat.Expiry) {
			return stat.Success, stat.Msg
		}
	}

	req, err := http.NewRequest("GET", "https://api.themoviedb.org/3/configuration", nil)
	if err != nil {
		return false, fmt.Sprintf("Internal Error: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	// Check Proxy
	// Proxy Setup (Reusing variables from Cache Check)
	var transport *http.Transport
	if proxyEnabled.Value == "true" && proxyConfig.Value != "" {
		if proxyUrl, err := url.Parse(proxyConfig.Value); err == nil {
			transport = &http.Transport{Proxy: http.ProxyURL(proxyUrl)}
		}
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	if transport != nil {
		client.Transport = transport
	}

	resp, err := client.Do(req)
	if err != nil {
		statusCache.Store("tmdb", cachedStatus{
			Success:    false,
			Msg:        err.Error(),
			ConfigHash: hash,
			Expiry:     time.Now().Add(1 * time.Minute),
		})
		return false, err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		statusCache.Store("tmdb", cachedStatus{
			Success:    true,
			Msg:        "",
			ConfigHash: hash,
			Expiry:     time.Now().Add(5 * time.Minute),
		})
		return true, ""
	}

	errMsg := "Token Invalid"
	if resp.StatusCode != 401 {
		errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	statusCache.Store("tmdb", cachedStatus{
		Success:    false,
		Msg:        errMsg,
		ConfigHash: hash,
		Expiry:     time.Now().Add(1 * time.Minute),
	})

	return false, errMsg
}

// Logic for AniList Status
func GetAniListStatusHandler(c *gin.Context) {
	style := c.Query("style")
	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, RenderAniListStatus(style))
}

func RenderAniListStatusOOB() string {
	content := RenderAniListStatus("")
	return strings.Replace(content, `id="anilist-status"`, `id="anilist-status" hx-swap-oob="innerHTML"`, 1)
}

func RenderAniListStatus(style string) string {
	connected, username, errStr := CheckAniListConnection()

	// Dashboard Style
	if style == "dashboard" {
		if connected {
			return `<span class="text-emerald-600 font-bold flex items-center gap-1"><span class="w-2 h-2 rounded-full bg-emerald-500"></span> 已连接</span>`
		}
		errText := "未连接"
		if strings.Contains(errStr, "Token") {
			errText = "未连接 (Token无效)"
		} else if errStr != "" {
			errText = "连接失败"
		}
		return fmt.Sprintf(`<span class="text-red-500 font-bold flex items-center gap-1" title="%s"><span class="w-2 h-2 rounded-full bg-red-500"></span> %s</span>`, errStr, errText)
	}

	if connected {
		return fmt.Sprintf(`<div id="anilist-status"><div class="text-emerald-600 bg-emerald-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200">✅ 已连接 AniList (%s)</div></div>`, username)
	}

	if errStr == "Token missing" {
		return `<div id="anilist-status"><div class="text-sm text-gray-500 flex items-center gap-2"><span>🔴 未连接</span><span class="text-xs text-gray-400">(请先输入 Token 并保存)</span></div></div>`
	}
	if strings.Contains(errStr, "Token Invalid") || strings.Contains(errStr, "401") || strings.Contains(errStr, "400") {
		return fmt.Sprintf(`<div id="anilist-status"><div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ 认证失败 (%s)</div></div>`, errStr)
	}

	return fmt.Sprintf(`<div id="anilist-status"><div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ 连接失败: %s</div></div>`, errStr)
}

type AniListViewerQuery struct {
	Viewer struct {
		Name string `json:"name"`
		Id   int    `json:"id"`
	} `json:"Viewer"`
}

type AniListResponse struct {
	Data   AniListViewerQuery `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func CheckAniListConnection() (bool, string, string) {
	var config model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyAniListToken).First(&config).Error; err != nil || config.Value == "" {
		return false, "", "Token missing"
	}

	token := strings.TrimSpace(config.Value)
	// Remove "Bearer " prefix if present (case-insensitive)
	lowerToken := strings.ToLower(token)
	if strings.HasPrefix(lowerToken, "bearer ") {
		token = strings.TrimSpace(token[7:])
	}

	if token == "" {
		return false, "", "Token missing"
	}

	// Cache Check
	var proxyConfig model.GlobalConfig
	var proxyEnabled model.GlobalConfig
	db.DB.Where("key = ?", model.ConfigKeyProxyAniList).First(&proxyEnabled)
	if proxyEnabled.Value == "true" {
		db.DB.Where("key = ?", model.ConfigKeyProxyURL).First(&proxyConfig)
	}

	hash := getCacheHash(token, proxyEnabled.Value, proxyConfig.Value)
	if val, ok := statusCache.Load("anilist"); ok {
		stat := val.(cachedStatus)
		if stat.ConfigHash == hash && time.Now().Before(stat.Expiry) {
			return stat.Success, stat.Msg2, stat.Msg // Msg2 is username, Msg is error
		}
	}

	query := `{"query": "{ Viewer { name id } }"}`

	req, err := http.NewRequest("POST", "https://graphql.anilist.co", bytes.NewBufferString(query))
	if err != nil {
		return false, "", fmt.Sprintf("Internal Error: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	var transport *http.Transport
	if proxyEnabled.Value == "true" {
		if proxyUrl, err := url.Parse(proxyConfig.Value); err == nil && proxyConfig.Value != "" {
			transport = &http.Transport{Proxy: http.ProxyURL(proxyUrl)}
		}
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	if transport != nil {
		client.Transport = transport
	}

	resp, err := client.Do(req)
	if err != nil {
		statusCache.Store("anilist", cachedStatus{
			Success:    false,
			Msg:        err.Error(),
			Msg2:       "",
			ConfigHash: hash,
			Expiry:     time.Now().Add(1 * time.Minute),
		})
		return false, "", err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		statusCache.Store("anilist", cachedStatus{
			Success:    false,
			Msg:        errMsg,
			Msg2:       "",
			ConfigHash: hash,
			Expiry:     time.Now().Add(1 * time.Minute),
		})
		return false, "", errMsg
	}

	var result AniListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, "", "Decode Error"
	}

	if len(result.Errors) > 0 {
		return false, "", result.Errors[0].Message
	}

	statusCache.Store("anilist", cachedStatus{
		Success:    true,
		Msg:        "",
		Msg2:       result.Data.Viewer.Name,
		ConfigHash: hash,
		Expiry:     time.Now().Add(5 * time.Minute),
	})

	return true, result.Data.Viewer.Name, ""
}

// Logic for Jellyfin Status
func GetJellyfinStatusHandler(c *gin.Context) {
	style := c.Query("style")
	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, RenderJellyfinStatus(style))
}

func RenderJellyfinStatusOOB() string {
	content := RenderJellyfinStatus("")
	return strings.Replace(content, `id="jellyfin-status"`, `id="jellyfin-status" hx-swap-oob="innerHTML"`, 1)
}

func RenderJellyfinStatus(style string) string {
	connected, errStr := CheckJellyfinConnection()

	// Dashboard Style
	if style == "dashboard" {
		if connected {
			return `<span class="text-emerald-600 font-bold flex items-center gap-1"><span class="w-2 h-2 rounded-full bg-emerald-500"></span> 已连接</span>`
		}
		errText := "未连接"
		if errStr != "" {
			errText = "连接失败"
		}
		return fmt.Sprintf(`<span class="text-red-500 font-bold flex items-center gap-1" title="%s"><span class="w-2 h-2 rounded-full bg-red-500"></span> %s</span>`, errStr, errText)
	}

	if connected {
		return `<div id="jellyfin-status"><div class="text-emerald-600 bg-emerald-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200">✅ 已连接 Jellyfin</div></div>`
	}

	if errStr == "Config missing" {
		return `<div id="jellyfin-status"><div class="text-sm text-gray-500 flex items-center gap-2"><span>🔴 未连接</span><span class="text-xs text-gray-400">(请先输入 URL 和 API Key 并保存)</span></div></div>`
	}

	return fmt.Sprintf(`<div id="jellyfin-status"><div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ 连接失败: %s</div></div>`, errStr)
}

func CheckJellyfinConnection() (bool, string) {
	var urlCfg, keyCfg, proxyEnabled model.GlobalConfig
	db.DB.Where("key = ?", model.ConfigKeyJellyfinUrl).First(&urlCfg)
	db.DB.Where("key = ?", model.ConfigKeyJellyfinApiKey).First(&keyCfg)
	db.DB.Where("key = ?", model.ConfigKeyProxyJellyfin).First(&proxyEnabled)

	if urlCfg.Value == "" || keyCfg.Value == "" {
		log.Printf("DEBUG: Jellyfin connection check failed: Config missing (URL: %s, Key: %s)", urlCfg.Value, keyCfg.Value)
		return false, "Config missing"
	}

	// Cache Check
	var proxyConfig model.GlobalConfig
	if proxyEnabled.Value == "true" {
		db.DB.Where("key = ?", model.ConfigKeyProxyURL).First(&proxyConfig)
	}

	hash := getCacheHash(urlCfg.Value, keyCfg.Value, proxyEnabled.Value, proxyConfig.Value)
	if val, ok := statusCache.Load("jellyfin"); ok {
		stat := val.(cachedStatus)
		if stat.ConfigHash == hash && time.Now().Before(stat.Expiry) {
			return stat.Success, stat.Msg
		}
	}

	serverUrl := strings.TrimRight(urlCfg.Value, "/")
	// Jellyfin common info endpoint
	apiUrl := fmt.Sprintf("%s/System/Info", serverUrl)

	log.Printf("DEBUG: Testing Jellyfin connection to: %s", apiUrl)

	req, err := http.NewRequest("GET", apiUrl, nil)
	if err != nil {
		return false, fmt.Sprintf("Internal Error: %v", err)
	}

	req.Header.Set("X-Emby-Token", keyCfg.Value)
	req.Header.Set("Accept", "application/json")

	var transport *http.Transport
	if proxyEnabled.Value == "true" {
		if proxyUrl, err := url.Parse(proxyConfig.Value); err == nil && proxyConfig.Value != "" {
			transport = &http.Transport{Proxy: http.ProxyURL(proxyUrl)}
		}
	}

	client := &http.Client{
		Timeout: 5 * time.Second, // Shorter timeout for status check
	}
	if transport != nil {
		client.Transport = transport
	}

	resp, err := client.Do(req)
	if err != nil {
		statusCache.Store("jellyfin", cachedStatus{
			Success:    false,
			Msg:        err.Error(),
			ConfigHash: hash,
			Expiry:     time.Now().Add(1 * time.Minute),
		})
		log.Printf("DEBUG: Jellyfin connection error: %v", err)
		return false, err.Error()
	}
	defer resp.Body.Close()

	log.Printf("DEBUG: Jellyfin connection response: %d", resp.StatusCode)

	if resp.StatusCode == 200 {
		statusCache.Store("jellyfin", cachedStatus{
			Success:    true,
			Msg:        "",
			ConfigHash: hash,
			Expiry:     time.Now().Add(5 * time.Minute),
		})
		return true, ""
	}

	errMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
	if resp.StatusCode == 401 {
		errMsg = "API Key Invalid"
	}

	statusCache.Store("jellyfin", cachedStatus{
		Success:    false,
		Msg:        errMsg,
		ConfigHash: hash,
		Expiry:     time.Now().Add(1 * time.Minute),
	})

	return false, errMsg
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
