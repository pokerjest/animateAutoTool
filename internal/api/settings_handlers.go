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
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/jellyfin"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
	"github.com/pokerjest/animateAutoTool/internal/updater"
)

const (
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

func persistGlobalConfig(key, val string) {
	var count int64
	db.DB.Model(&model.GlobalConfig{}).Where("key = ?", key).Count(&count)
	if count == 0 {
		db.DB.Create(&model.GlobalConfig{Key: key, Value: val})
		return
	}

	db.DB.Model(&model.GlobalConfig{}).Where("key = ?", key).Update("value", val)
}

func normalizedQBValues(mode, url, username, password string) map[string]string {
	mode = qbutil.NormalizeMode(mode)
	url = strings.TrimSpace(url)
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)

	if mode == qbutil.ModeManaged {
		mode = qbutil.ModeManaged
		url = ""
		username = ""
		password = ""
	}

	return map[string]string{
		model.ConfigKeyQBMode:     mode,
		model.ConfigKeyQBUrl:      url,
		model.ConfigKeyQBUsername: username,
		model.ConfigKeyQBPassword: password,
	}
}

func normalizedQBFormValues(c *gin.Context) map[string]string {
	return normalizedQBValues(
		c.PostForm(model.ConfigKeyQBMode),
		c.PostForm(model.ConfigKeyQBUrl),
		c.PostForm(model.ConfigKeyQBUsername),
		c.PostForm(model.ConfigKeyQBPassword),
	)
}

func qbConfigFromForm(c *gin.Context) qbutil.Config {
	values := normalizedQBFormValues(c)
	cfg := qbutil.Config{
		Mode:     values[model.ConfigKeyQBMode],
		URL:      values[model.ConfigKeyQBUrl],
		Username: values[model.ConfigKeyQBUsername],
		Password: values[model.ConfigKeyQBPassword],
	}

	if qbutil.UsesManagedInstance(cfg) {
		cfg.Mode = qbutil.ModeManaged
		cfg.URL = qbutil.DefaultURL
		cfg.Username = ""
		cfg.Password = ""
	}

	return cfg
}

func renderQBStatusMessage(cfg qbutil.Config) string {
	if qbutil.ManagedBinaryMissing(cfg, config.BinDir()) {
		return `<div class="text-amber-600 bg-amber-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-amber-200">Managed qBittorrent is selected, but the local binary is not installed.</div>`
	}

	if qbutil.MissingExternalURL(cfg) {
		return `<div class="text-amber-600 bg-amber-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-amber-200">External qBittorrent mode is enabled, but the WebUI URL is still empty.</div>`
	}

	client := downloader.NewQBittorrentClient(cfg.URL)
	if err := client.Login(cfg.Username, cfg.Password); err != nil {
		return fmt.Sprintf(`<div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">QB connection failed: %v</div>`, err)
	}

	if ver, err := client.GetVersion(); err == nil {
		return fmt.Sprintf(`<div class="text-emerald-600 bg-emerald-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200">QB connected (version: %s)</div>`, ver)
	}

	return `<div class="text-amber-600 bg-amber-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-amber-200">QB login succeeded but version lookup failed</div>`
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

	qbCfg := qbutil.LoadConfig()
	configMap[model.ConfigKeyQBMode] = qbCfg.Mode
	if qbutil.UsesManagedInstance(qbCfg) {
		configMap[model.ConfigKeyQBUrl] = ""
		configMap[model.ConfigKeyQBUsername] = ""
		configMap[model.ConfigKeyQBPassword] = ""
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

	// Fetch Backup Stats for the new Backup tab
	stats := getDBStats(db.DB, db.CurrentDBPath)

	c.HTML(http.StatusOK, "settings.html", gin.H{
		"SkipLayout":       skip,
		"Config":           configMap,
		"JellyfinServerID": jellyfinServerId,
		"Stats":            stats,
	})
}

func UpdateSettingsHandler(c *gin.Context) {
	// 1. Save All Configs
	// 1. Determine Scope
	scope, _ := c.GetPostForm("settings_scope")

	// Define all possible keys and their scopes
	// This map is optional if we purely rely on keysToProcess, but helpful for validation

	// Define all possible keys and their scopes
	// This map is optional if we purely rely on keysToProcess, but helpful for validation

	var keysToProcess []string
	checkboxesToProcess := []string{}

	switch scope {
	case "download":
		keysToProcess = []string{
			model.ConfigKeyQBMode,
			model.ConfigKeyQBUrl,
			model.ConfigKeyQBUsername,
			model.ConfigKeyQBPassword,
			model.ConfigKeyBaseDir,
		}
	case "data-sources":
		keysToProcess = []string{
			model.ConfigKeyBangumiRefreshToken,
			model.ConfigKeyBangumiAccessToken,
			model.ConfigKeyBangumiAppID,     // Added
			model.ConfigKeyBangumiAppSecret, // Added
			model.ConfigKeyTMDBToken,
			model.ConfigKeyAniListToken,
		}
	case "network":
		keysToProcess = []string{
			model.ConfigKeyProxyURL,
			model.ConfigKeyRepoUpdateIntervalMinutes,
			model.ConfigKeyRepoUpdateOwner,
			model.ConfigKeyRepoUpdateName,
		}
		checkboxesToProcess = []string{
			model.ConfigKeyProxyBangumi,
			model.ConfigKeyProxyTMDB,
			model.ConfigKeyProxyAniList,
			model.ConfigKeyProxyJellyfin,
			model.ConfigKeyRepoUpdateEnabled,
			model.ConfigKeyRepoAutoPullEnabled,
			model.ConfigKeyRepoRequireChecksum,
		}
	case "media":
		keysToProcess = []string{
			model.ConfigKeyJellyfinUrl,
			model.ConfigKeyJellyfinApiKey,
			model.ConfigKeyJellyfinUsername,
			model.ConfigKeyJellyfinPassword,
		}
		checkboxesToProcess = []string{
			// model.ConfigKeyProxyJellyfin is in network tab now
		}
	case "pikpak":
		keysToProcess = []string{
			model.ConfigKeyPikPakUsername,
			model.ConfigKeyPikPakPassword,
			model.ConfigKeyPikPakRefreshToken,
		}
	default:
		// Fallback: Legacy Global Save (Process ALL)
		keysToProcess = []string{
			model.ConfigKeyQBMode,
			model.ConfigKeyQBUrl,
			model.ConfigKeyQBUsername,
			model.ConfigKeyQBPassword,
			model.ConfigKeyBaseDir,
			model.ConfigKeyBangumiRefreshToken,
			model.ConfigKeyBangumiAccessToken,
			model.ConfigKeyTMDBToken,
			model.ConfigKeyAniListToken,
			model.ConfigKeyProxyURL,
			model.ConfigKeyProxyBangumi,
			model.ConfigKeyProxyTMDB,
			model.ConfigKeyProxyAniList,
			model.ConfigKeyRepoUpdateEnabled,
			model.ConfigKeyRepoAutoPullEnabled,
			model.ConfigKeyRepoUpdateIntervalMinutes,
			model.ConfigKeyRepoUpdateOwner,
			model.ConfigKeyRepoUpdateName,
			model.ConfigKeyRepoRequireChecksum,
			model.ConfigKeyJellyfinUrl,
			model.ConfigKeyJellyfinApiKey,
			model.ConfigKeyJellyfinUsername,
			model.ConfigKeyJellyfinPassword,
			model.ConfigKeyProxyJellyfin,
			model.ConfigKeyPikPakUsername,
			model.ConfigKeyPikPakPassword,
			model.ConfigKeyPikPakRefreshToken,
		}
		checkboxesToProcess = []string{
			model.ConfigKeyProxyBangumi,
			model.ConfigKeyProxyTMDB,
			model.ConfigKeyProxyAniList,
			model.ConfigKeyProxyJellyfin,
			model.ConfigKeyRepoUpdateEnabled,
			model.ConfigKeyRepoAutoPullEnabled,
			model.ConfigKeyRepoRequireChecksum,
		}
	}

	qbOverrides := map[string]string{}
	if scope == "download" {
		qbOverrides = normalizedQBFormValues(c)
	} else if _, hasQBMode := c.GetPostForm(model.ConfigKeyQBMode); hasQBMode {
		qbOverrides = normalizedQBFormValues(c)
	}

	// 1.1 Process Standard Keys
	for _, key := range keysToProcess {
		if val, ok := qbOverrides[key]; ok {
			persistGlobalConfig(key, val)
			continue
		}

		val, exists := c.GetPostForm(key)
		if !exists {
			continue // Should not happen if form is correct, but safe to skip
		}

		persistGlobalConfig(key, val)
	}

	// 1.2 Process Checkboxes (Scope-aware)
	// Only set to "false" if the checkbox is EXPECTED in this scope but missing in form
	for _, key := range checkboxesToProcess {
		if _, exists := c.GetPostForm(key); !exists {
			// It was in the scope but not in the post body -> User unchecked it
			persistGlobalConfig(key, "false")
		} else {
			// It exists -> User checked it (value="true")
			// The loop above (keysToProcess) handles string values, but checkboxes need specific handling if they are mixed?
			// Actually, standard HTML forms send value="true" if checked, and nothing if unchecked.
			// So for checked items, we need to save "true".

			// Let's rely on standard logic:
			persistGlobalConfig(key, "true")
		}
	}

	// 1.6 JELLYFIN AUTO-AUTH LOGIC
	jfUrl := c.PostForm(model.ConfigKeyJellyfinUrl)
	jfUser := c.PostForm(model.ConfigKeyJellyfinUsername)
	jfPass := c.PostForm(model.ConfigKeyJellyfinPassword)

	go func(u, user, pass string) {
		if u != "" {
			// Check if we have a valid key already?
			// Policy: If user provided credentials => Try to Refresh/Get Key.
			// Re-authenticating is cheap enough.

			if user == "" || pass == "" {
				return
			}

			// Try to authenticate
			client := jellyfin.NewClient(u, "")
			authResp, err := client.Authenticate(user, pass)
			if err == nil && authResp.AccessToken != "" {
				// Success! Save Key.
				log.Printf("Jellyfin Auto-Auth Successful for user: %s", user)

				var count int64
				db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyJellyfinApiKey).Count(&count)
				if count == 0 {
					db.DB.Create(&model.GlobalConfig{Key: model.ConfigKeyJellyfinApiKey, Value: authResp.AccessToken})
				} else {
					db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyJellyfinApiKey).Update("value", authResp.AccessToken)
				}

				// Invalidate Cache
				statusCache.Delete("jellyfin")
			} else {
				log.Printf("Jellyfin Auto-Auth Failed: %v", err)
				// Don't clear existing key, maybe it's valid?
			}
		}
	}(jfUrl, jfUser, jfPass)

	// 1.7 PIKPAK AUTO-SYNC LOGIC
	ppUser := c.PostForm(model.ConfigKeyPikPakUsername)
	ppPass := c.PostForm(model.ConfigKeyPikPakPassword)
	ppRefreshToken := c.PostForm(model.ConfigKeyPikPakRefreshToken)
	ppCaptchaToken := c.PostForm(model.ConfigKeyPikPakCaptchaToken)

	if ppUser != "" && ppPass != "" {
		go func(u, p, r, c string) {
			if err := alist.AddPikPakStorage(u, p, r, c); err != nil {
				log.Printf("Failed to sync PikPak storage in background: %v", err)
			} else {
				log.Printf("PikPak storage synced successfully in background for user: %s", u)
			}
		}(ppUser, ppPass, ppRefreshToken, ppCaptchaToken)
	}

	// 2. Add artificial delay for smooth UI transition
	time.Sleep(500 * time.Millisecond)

	// 3. Refresh QB status using normalized mode-aware config
	statusCache.Delete("qb")
	qbStatusHtml := fmt.Sprintf(`<div id="qb-status" hx-swap-oob="innerHTML">%s</div>`, renderQBStatusMessage(qbConfigFromForm(c)))
	// 4. Trigger Bangumi Refresh OOB
	bangumiStatusOOB := `<div hx-swap-oob="true" id="bangumi-refresh-trigger" hx-get="/api/bangumi/profile" hx-target="#bangumi-status" hx-trigger="load" class="hidden"></div>`

	// 5. Trigger TMDB Refresh OOB
	tmdbStatusOOB := RenderTMDBStatusOOB()

	// 6. Trigger AniList Refresh OOB
	anilistStatusOOB := RenderAniListStatusOOB()

	// 7. Trigger Jellyfin Refresh OOB
	jellyfinStatusOOB := RenderJellyfinStatusOOB()

	// 8. Return Success Message + OOB Status Updates
	successMsg := fmt.Sprintf(`
		<div class="text-emerald-600 font-bold flex items-center gap-2 animate-pulse">✅ 所有配置已保存</div>
		%s
		%s
		%s
		%s
		%s
	`, qbStatusHtml, bangumiStatusOOB, tmdbStatusOOB, anilistStatusOOB, jellyfinStatusOOB)

	// If saving data sources, refresh their statuses immediately
	if scope == "data-sources" {
		successMsg += RenderBangumiStatusOOB()
		successMsg += RenderTMDBStatusOOB()
		successMsg += RenderAniListStatusOOB()
	}
	if scope == "network" {
		go updater.CheckNow("settings-save")
		successMsg += `<div hx-swap-oob="true" id="repo-update-refresh-trigger" hx-get="/api/settings/repo-update-status" hx-target="#repo-update-container" hx-trigger="load" class="hidden"></div>`
	}

	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, successMsg)
}

// QBSaveAndTestHandler saves QB settings and then tests connection
func QBSaveAndTestHandler(c *gin.Context) {
	qbValues := normalizedQBFormValues(c)
	persistGlobalConfig(model.ConfigKeyQBMode, qbValues[model.ConfigKeyQBMode])
	persistGlobalConfig(model.ConfigKeyQBUrl, qbValues[model.ConfigKeyQBUrl])
	persistGlobalConfig(model.ConfigKeyQBUsername, qbValues[model.ConfigKeyQBUsername])
	persistGlobalConfig(model.ConfigKeyQBPassword, qbValues[model.ConfigKeyQBPassword])
	persistGlobalConfig(model.ConfigKeyBaseDir, strings.TrimSpace(c.PostForm(model.ConfigKeyBaseDir)))

	statusCache.Delete("qb")
	c.String(http.StatusOK, `<div class="space-y-2"><div class="text-emerald-600 bg-emerald-50 px-3 py-1.5 rounded-lg text-sm font-medium border border-emerald-200 shadow-sm">QB settings saved.</div>`+renderQBStatusMessage(qbConfigFromForm(c))+`</div>`)
}

// PikPakSyncHandler synchronizes PikPak settings to AList
func PikPakSyncHandler(c *gin.Context) {
	username := c.PostForm("pikpak_username")
	password := c.PostForm("pikpak_password")
	refreshToken := c.PostForm("pikpak_refresh_token")
	captchaToken := c.PostForm("pikpak_captcha_token")

	if username == "" || password == "" {
		c.String(http.StatusOK, `<span class="text-red-500">❌ 请先填写 PikPak 用户名和密码</span>`)
		return
	}

	// 1. SAVE to DB first (So it persists on refresh)
	pikpakKeys := map[string]string{
		model.ConfigKeyPikPakUsername:     username,
		model.ConfigKeyPikPakPassword:     password,
		model.ConfigKeyPikPakRefreshToken: refreshToken,
	}

	for key, val := range pikpakKeys {
		var count int64
		db.DB.Model(&model.GlobalConfig{}).Where("key = ?", key).Count(&count)
		if count == 0 {
			db.DB.Create(&model.GlobalConfig{Key: key, Value: val})
		} else {
			db.DB.Model(&model.GlobalConfig{}).Where("key = ?", key).Update("value", val)
		}
	}

	// 2. Sync to Alist
	err := alist.AddPikPakStorage(username, password, refreshToken, captchaToken)
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
	time.Sleep(500 * time.Millisecond) // Artificial delay for UX
	status, err := alist.GetPikPakStatus()
	html := ""

	// Simulate Jellyfin rendering style
	if err != nil {
		html = fmt.Sprintf(`<div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ Error: %s</div>`, err.Error())
	} else if status == "work" || status == "WORK" {
		html = `<div class="text-emerald-600 bg-emerald-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200">✅ 运行正常</div>`
	} else if status == "未配置" {
		html = `<div class="text-sm text-gray-500 flex items-center gap-2"><span>🔴 未配置</span><span class="text-xs text-gray-400">(请填写账号密码并点击保存连接)</span></div>`
	} else {
		// Other non-work status
		html = fmt.Sprintf(`<div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ 状态: %s</div>`, status)
	}

	// Return plain content
	c.String(http.StatusOK, html)
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
	cfg := qbConfigFromForm(c)
	c.Header("Content-Type", "text/html")

	if qbutil.ManagedBinaryMissing(cfg, config.BinDir()) {
		c.String(http.StatusOK, `<div class="text-amber-600 bg-amber-50 px-3 py-1.5 rounded-lg text-sm font-medium flex items-center gap-2 border border-amber-200 shadow-sm">Managed qBittorrent is selected, but the local binary is not installed.</div>`)
		return
	}
	if qbutil.MissingExternalURL(cfg) {
		c.String(http.StatusBadRequest, `<div class="text-red-600 bg-red-50 px-3 py-1.5 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200 shadow-sm">External qBittorrent mode requires a WebUI URL.</div>`)
		return
	}

	c.String(http.StatusOK, renderQBStatusMessage(cfg))
}

// GetQBStatusHandler tests connection using stored config with caching
func GetQBStatusHandler(c *gin.Context) {
	time.Sleep(500 * time.Millisecond)
	qbCfg := qbutil.LoadConfig()

	if qbutil.ManagedBinaryMissing(qbCfg, config.BinDir()) {
		c.String(http.StatusOK, `<div class="text-amber-600 bg-amber-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-amber-200">Managed qBittorrent is selected, but the local binary is not installed.</div>`)
		return
	}
	if qbutil.MissingExternalURL(qbCfg) {
		c.String(http.StatusOK, `<div class="text-amber-600 bg-amber-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-amber-200">External qBittorrent mode is enabled, but the WebUI URL is still empty.</div>`)
		return
	}

	hash := getCacheHash(qbCfg.Mode, qbCfg.URL, qbCfg.Username, qbCfg.Password)
	if val, ok := statusCache.Load("qb"); ok {
		stat := val.(cachedStatus)
		if stat.ConfigHash == hash && time.Now().Before(stat.Expiry) {
			if stat.Success {
				c.String(http.StatusOK, fmt.Sprintf(`<div class="text-emerald-600 bg-emerald-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200">QB connected (cached) %s</div>`, stat.Msg))
			} else {
				c.String(http.StatusOK, fmt.Sprintf(`<div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">QB connection failed (cached): %s</div>`, stat.Msg))
			}
			return
		}
	}

	client := downloader.NewQBittorrentClient(qbCfg.URL)
	if err := client.Login(qbCfg.Username, qbCfg.Password); err != nil {
		statusCache.Store("qb", cachedStatus{Success: false, Msg: err.Error(), ConfigHash: hash, Expiry: time.Now().Add(1 * time.Minute)})
		c.String(http.StatusOK, fmt.Sprintf(`<div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">QB connection failed: %v</div>`, err))
		return
	}

	if ver, err := client.GetVersion(); err == nil {
		statusCache.Store("qb", cachedStatus{Success: true, Msg: fmt.Sprintf("(version: %s)", ver), ConfigHash: hash, Expiry: time.Now().Add(5 * time.Minute)})
		c.String(http.StatusOK, fmt.Sprintf(`<div class="text-emerald-600 bg-emerald-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200">QB connected (version: %s)</div>`, ver))
		return
	}

	statusCache.Store("qb", cachedStatus{Success: false, Msg: "Version unknown", ConfigHash: hash, Expiry: time.Now().Add(1 * time.Minute)})
	c.String(http.StatusOK, `<div class="text-amber-600 bg-amber-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-amber-200">QB login succeeded but version lookup failed</div>`)
}
func GetTMDBStatusHandler(c *gin.Context) {
	time.Sleep(500 * time.Millisecond) // Artificial delay for UX
	style := c.Query("style")
	c.Header("Content-Type", "text/html")
	html := RenderTMDBStatus(style)
	// Strip wrapper if it exists (Cleanup old logic)
	if strings.Contains(html, `<div id="tmdb-status">`) {
		html = strings.Replace(html, `<div id="tmdb-status">`, "", 1)
		html = strings.TrimSuffix(html, "</div>")
	}
	// Return plain content
	c.String(http.StatusOK, html)
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
	defer safeio.Close(resp.Body)

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
	time.Sleep(500 * time.Millisecond) // Artificial delay for UX
	style := c.Query("style")
	c.Header("Content-Type", "text/html")
	html := RenderAniListStatus(style)
	// Strip wrapper if it exists
	if strings.Contains(html, `<div id="anilist-status">`) {
		html = strings.Replace(html, `<div id="anilist-status">`, "", 1)
		html = strings.TrimSuffix(html, "</div>")
	}
	// Return plain content
	c.String(http.StatusOK, html)
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
	defer safeio.Close(resp.Body)

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
	time.Sleep(500 * time.Millisecond) // Artificial delay for UX
	style := c.Query("style")
	c.Header("Content-Type", "text/html")
	html := RenderJellyfinStatus(style)
	// Strip wrapper if it exists
	if strings.Contains(html, `<div id="jellyfin-status">`) {
		html = strings.Replace(html, `<div id="jellyfin-status">`, "", 1)
		html = strings.TrimSuffix(html, "</div>")
	}
	// Return plain content
	c.String(http.StatusOK, html)
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
		log.Printf("DEBUG: Jellyfin connection check failed: Config missing (hasURL=%t, hasKey=%t)", urlCfg.Value != "", keyCfg.Value != "")
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
	defer safeio.Close(resp.Body)

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

// JellyfinLoginHandler attempts to log in to Jellyfin and returns the API Key
func JellyfinLoginHandler(c *gin.Context) {
	url := c.PostForm("jellyfin_url")
	username := c.PostForm("jellyfin_username")
	password := c.PostForm("jellyfin_password")

	if url == "" || username == "" {
		c.String(http.StatusOK, `<div id="jellyfin-login-status" class="text-red-500 text-sm mt-2">❌ 需要填写 URL 和 用户名</div>`)
		return
	}

	client := jellyfin.NewClient(url, "")
	resp, err := client.Authenticate(username, password)
	if err != nil {
		c.String(http.StatusOK, fmt.Sprintf(`<div id="jellyfin-login-status" class="text-red-500 text-sm mt-2">❌ 登录失败: %s</div>`, err.Error()))
		return
	}

	successMsg := `<div id="jellyfin-login-status" class="text-emerald-600 font-bold text-sm mt-2 flex items-center gap-2">✅ 获取成功! <span class="text-xs font-normal text-gray-500">(Token已自动填入)</span></div>`
	// Ensure this ID matches settings.html. I will verify settings.html next.
	updateInput := fmt.Sprintf(`<input id="jellyfin_api_key_input" name="jellyfin_api_key" value="%s" type="text"
                                    class="w-full px-5 py-3 rounded-2xl bg-white/50 border border-gray-200 focus:bg-white focus:border-orange-300 focus:ring-4 focus:ring-orange-500/10 outline-none transition-all font-medium font-mono text-sm shadow-inner group-hover:border-orange-200"
                                    placeholder="输入 API Key 或使用下方登录获取" hx-swap-oob="true">`, resp.AccessToken)

	c.String(http.StatusOK, successMsg+updateInput)
}
