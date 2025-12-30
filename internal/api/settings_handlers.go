package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/alist"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/jellyfin"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

const (
	DefaultServerURL     = "http://localhost:8080"
	StatusNotConnected   = "未连接"
	StatusConnected      = "已连接"
	StatusConnectedHTML  = `<span class="text-emerald-600 font-bold flex items-center gap-1"><span class="w-2 h-2 rounded-full bg-emerald-500"></span> ` + StatusConnected + `</span>`
	StatusConnectionFail = "连接失败"
	ErrTokenMissing      = "Token missing"
	StyleDashboard       = "dashboard"
	SourceBangumi        = "bangumi"
	SourceAniList        = "anilist"
	SourceTMDB           = "tmdb"
	ValueTrue            = "true"
)

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

func SettingsHandler(c *gin.Context) {
	skip := IsHTMX(c)

	var configs []model.GlobalConfig
	if err := db.DB.Find(&configs).Error; err != nil {
		log.Printf("Error fetching configs: %v", err)
	}

	configMap := make(map[string]string)
	for _, cfg := range configs {
		configMap[cfg.Key] = cfg.Value
	}

	// Fetch Jellyfin Server ID for qualified links
	jellyfinServerId := ""
	if url, ok := configMap[model.ConfigKeyJellyfinUrl]; ok && url != "" {
		if apiKey, ok := configMap[model.ConfigKeyJellyfinApiKey]; ok && apiKey != "" {
			client := jellyfin.NewClient(url, apiKey)
			if info, err := client.GetPublicInfo(); err == nil {
				jellyfinServerId = info.Id
			} else {
				log.Printf("ERROR: Failed to fetch Jellyfin Server ID in Settings: %v", err)
			}
		}
	}

	c.HTML(http.StatusOK, "settings.html", gin.H{
		"SkipLayout":       skip,
		"Config":           configMap,
		"JellyfinServerID": jellyfinServerId,
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
	bangumiStatusOOB := `<div hx-swap-oob="true" id="bangumi-refresh-trigger" hx-get="/api/bangumi/profile" hx-target="#bangumi-status" hx-trigger="load" class="hidden"></div>`

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

	// Construct success message
	successHtml := `<div class="text-emerald-600 font-medium flex items-center gap-2 animate-fade-in-up">✅ 保存成功</div>`
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

	c.String(http.StatusOK, `<div class="text-emerald-600 bg-emerald-50 px-3 py-1.5 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200 shadow-sm transition-all duration-300 transform scale-100">✅ 连接成功! (`+version+`)</div>`)
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
	if style == StyleDashboard {
		if connected {
			return StatusConnectedHTML
		}
		errText := StatusNotConnected
		if strings.Contains(errStr, "Token") {
			errText = "未连接 (Token无效)"
		} else if errStr != "" {
			errText = StatusConnectionFail
		}
		return fmt.Sprintf(`<span class="text-red-500 font-bold flex items-center gap-1" title="%s"><span class="w-2 h-2 rounded-full bg-red-500"></span> %s</span>`, errStr, errText)
	}

	// Default Settings Page Style (The Pill)
	if connected {
		return `<div id="tmdb-status"><div class="text-emerald-600 bg-emerald-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200">✅ 已连接 TMDB</div></div>`
	}

	// Error cases
	if errStr == ErrTokenMissing {
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
		return false, ErrTokenMissing
	}

	token := config.Value

	// Cache Check
	// We also need proxy config for hash
	var proxyEnabled model.GlobalConfig
	var proxyConfig model.GlobalConfig
	db.DB.Where("key = ?", model.ConfigKeyProxyTMDB).First(&proxyEnabled)
	if proxyEnabled.Value == ValueTrue {
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
	if proxyEnabled.Value == ValueTrue && proxyConfig.Value != "" {
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

	if errStr == ErrTokenMissing {
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
		return false, "", ErrTokenMissing
	}

	token := strings.TrimSpace(config.Value)
	// Remove "Bearer " prefix if present (case-insensitive)
	lowerToken := strings.ToLower(token)
	if strings.HasPrefix(lowerToken, "bearer ") {
		token = strings.TrimSpace(token[7:])
	}

	if token == "" {
		return false, "", ErrTokenMissing
	}

	// Cache Check
	var proxyConfig model.GlobalConfig
	var proxyEnabled model.GlobalConfig
	db.DB.Where("key = ?", model.ConfigKeyProxyAniList).First(&proxyEnabled)
	if proxyEnabled.Value == ValueTrue {
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
	if proxyEnabled.Value == ValueTrue {
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
	if proxyEnabled.Value == ValueTrue {
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
	if proxyEnabled.Value == ValueTrue {
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
