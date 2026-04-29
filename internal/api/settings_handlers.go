package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"gorm.io/gorm"
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

func persistGlobalConfig(key, val string) error {
	if db.DB == nil {
		return gorm.ErrInvalidDB
	}
	return store.NewConfigStore(db.DB).Set(key, val)
}

func persistGlobalConfigs(values map[string]string) error {
	if db.DB == nil {
		return gorm.ErrInvalidDB
	}
	return store.NewConfigStore(db.DB).SetMany(values)
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

func SettingsHandler(c *gin.Context) {
	skip := IsHTMX(c)
	configMap, jellyfinServerId, stats := loadSettingsViewData()

	c.HTML(http.StatusOK, "settings.html", gin.H{
		"SkipLayout":       skip,
		"Config":           configMap,
		"JellyfinServerID": jellyfinServerId,
		"Stats":            stats,
	})
}

func UpdateSettingsHandler(c *gin.Context) {
	scope, _ := c.GetPostForm("settings_scope")
	if err := persistSettingsScope(c, scope); err != nil {
		c.Header("Content-Type", "text/html")
		c.String(http.StatusInternalServerError, renderSettingsSaveError(fmt.Sprintf("保存配置失败: %v", err)))
		return
	}
	var warnings []string
	warnings = append(warnings, maybeAutoAuthJellyfin(c)...)
	warnings = append(warnings, maybeAutoSyncPikPak(c)...)
	successMsg := buildSettingsSaveResponse(c, scope, warnings)

	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, successMsg)
}

// BangumiSaveHandler saves Bangumi settings and returns success
func BangumiSaveHandler(c *gin.Context) {
	if err := persistBangumiSettings(c); err != nil {
		c.String(http.StatusInternalServerError, renderSettingsSaveError(fmt.Sprintf("保存 Bangumi 配置失败: %v", err)))
		return
	}
	c.String(http.StatusOK, renderBangumiSaveSuccess())
}
