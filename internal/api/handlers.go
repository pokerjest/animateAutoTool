package api

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
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

	if sub.FilterRule == "" {
		sub.FilterRule = "1080p,简体" // Default
	}

	sub.IsActive = true
	if err := db.DB.Create(&sub).Error; err != nil {
		c.String(http.StatusInternalServerError, "Failed to create: "+err.Error())
		return
	}

	c.Header("HX-Redirect", "/subscriptions")
	c.Status(http.StatusOK)
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
	id := c.Param("id")
	db.DB.Delete(&model.Subscription{}, id)
	c.Status(http.StatusOK)
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
