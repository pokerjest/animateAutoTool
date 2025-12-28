package api

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

func getBangumiConfig() (string, string) {
	var configs []model.GlobalConfig
	db.DB.Where("key IN ?", []string{model.ConfigKeyBangumiAppID, model.ConfigKeyBangumiAppSecret}).Find(&configs)

	mp := make(map[string]string)
	for _, c := range configs {
		mp[c.Key] = c.Value
	}
	return mp[model.ConfigKeyBangumiAppID], mp[model.ConfigKeyBangumiAppSecret]
}

func getBangumiTokens() (string, string) {
	var configs []model.GlobalConfig
	db.DB.Where("key IN ?", []string{model.ConfigKeyBangumiAccessToken, model.ConfigKeyBangumiRefreshToken}).Find(&configs)

	mp := make(map[string]string)
	for _, c := range configs {
		mp[c.Key] = c.Value
	}
	return mp[model.ConfigKeyBangumiAccessToken], mp[model.ConfigKeyBangumiRefreshToken]
}

func saveBangumiTokens(accessToken, refreshToken string) {
	// Upsert Access Token
	var at model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyBangumiAccessToken).First(&at).Error; err != nil {
		db.DB.Create(&model.GlobalConfig{Key: model.ConfigKeyBangumiAccessToken, Value: accessToken})
	} else {
		at.Value = accessToken
		db.DB.Save(&at)
	}

	// Upsert Refresh Token
	var rt model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyBangumiRefreshToken).First(&rt).Error; err != nil {
		db.DB.Create(&model.GlobalConfig{Key: model.ConfigKeyBangumiRefreshToken, Value: refreshToken})
	} else {
		rt.Value = refreshToken
		db.DB.Save(&rt)
	}
}

func applyProxyToBangumiClient(client *bangumi.Client) {
	var config model.GlobalConfig
	var enabledConfig model.GlobalConfig

	// Check if proxy is set
	if err := db.DB.Where("key = ?", model.ConfigKeyProxyURL).First(&config).Error; err != nil || config.Value == "" {
		return
	}

	// Check if enabled for Bangumi (default to false if not set, or true? User says "checkbox", so explicit enable)
	// But traditionally if not set, maybe we want it off.
	// We check if "proxy_bangumi_enabled" == "true"
	if err := db.DB.Where("key = ?", model.ConfigKeyProxyBangumi).First(&enabledConfig).Error; err != nil || enabledConfig.Value != model.ConfigValueTrue {
		return
	}

	client.SetProxy(config.Value)
}

func BangumiLoginHandler(c *gin.Context) {
	appID, appSecret := getBangumiConfig()
	if appID == "" || appSecret == "" {
		c.String(http.StatusBadRequest, "请先在设置中配置 Bangumi App ID 和 Secret")
		return
	}

	redirectURI := "http://localhost:8080/api/bangumi/callback"

	client := bangumi.NewClient(appID, appSecret, redirectURI)
	applyProxyToBangumiClient(client)
	url := client.GetAuthorizationURL()

	c.Redirect(http.StatusTemporaryRedirect, url)
}

func BangumiCallbackHandler(c *gin.Context) {
	code := c.Query("code")
	if code == "" {
		c.String(http.StatusBadRequest, "No code provided")
		return
	}

	appID, appSecret := getBangumiConfig()
	redirectURI := "http://localhost:8080/api/bangumi/callback"
	client := bangumi.NewClient(appID, appSecret, redirectURI)

	tokenResp, err := client.ExchangeToken(code)
	if err != nil {
		log.Printf("Bangumi exchange token error: %v", err)
		c.String(http.StatusInternalServerError, "Login failed: "+err.Error())
		return
	}

	saveBangumiTokens(tokenResp.AccessToken, tokenResp.RefreshToken)

	// Redirect back to settings page
	c.Redirect(http.StatusTemporaryRedirect, "/settings")
}

// Helper to generate Bangumi Status CONTENT (no outer div)
func renderBangumiContent() string {
	accessToken, refreshToken := getBangumiTokens()

	if accessToken == "" {
		return `<div class="text-sm text-gray-500 flex items-center gap-2">
			<span>🔴 未连接</span>
			<span class="text-xs text-gray-400">(请先输入 Access Token 并保存)</span>
		</div>`
	}

	client := bangumi.NewClient("", "", "")
	applyProxyToBangumiClient(client)
	user, err := client.GetCurrentUser(accessToken)
	if err != nil {
		if refreshToken != "" {
			appID, appSecret := getBangumiConfig()
			if appID != "" && appSecret != "" {
				client := bangumi.NewClient(appID, appSecret, "http://localhost:8080/api/bangumi/callback")
				applyProxyToBangumiClient(client)
				if tokenResp, errRefresh := client.RefreshToken(refreshToken); errRefresh == nil {
					saveBangumiTokens(tokenResp.AccessToken, tokenResp.RefreshToken)
					user, err = client.GetCurrentUser(tokenResp.AccessToken)
				}
			}
		}
	}

	if err != nil {
		log.Printf("Bangumi profile fetch failed: %v", err)
		return `<div class="text-sm text-red-500 flex items-center gap-2">
			<span>🔴 连接失败</span>
			<span class="text-xs text-gray-400">(Token 无效或过期，请重新生成)</span>
		</div>`
	}

	return `
	<div class="flex items-center gap-4 bg-pink-50 p-4 rounded-xl border border-pink-100 animate-fade-in-up">
		<a href="` + user.URL + `" target="_blank">
			<img src="` + user.Avatar.Medium + `" class="w-12 h-12 rounded-full border-2 border-white shadow-sm hover:scale-105 transition">
		</a>
		<div>
			<div class="font-bold text-gray-800">` + user.Nickname + ` <span class="text-xs font-normal text-gray-500">(@` + user.Username + `)</span></div>
			<div class="text-xs text-pink-500 flex items-center gap-1">
				<span>🌸</span> Bangumi 已连接
			</div>
		</div>
		<div class="ml-auto">
			<button hx-post="/api/bangumi/logout" hx-target="#bangumi-status" class="px-3 py-1.5 rounded-lg border border-gray-200 text-xs text-gray-500 hover:text-red-500 hover:bg-white transition bg-white/50">
				清除 Token
			</button>
		</div>
	</div>
	`
}

// RenderBangumiStatusOOB string for settings page global save update
func RenderBangumiStatusOOB() string {
	content := renderBangumiContent()
	return fmt.Sprintf(`<div id="bangumi-status" hx-swap-oob="innerHTML" class="min-h-[80px]">%s</div>`, content)
}

func BangumiProfileHandler(c *gin.Context) {
	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, renderBangumiContent())
}

func BangumiLogoutHandler(c *gin.Context) {
	// Clear tokens
	db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyBangumiAccessToken).Update("value", "")
	db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyBangumiRefreshToken).Update("value", "")

	// Check if configured to show "Connect" button state
	c.String(http.StatusOK, `<div class="text-sm text-gray-500 flex items-center gap-2"><span>🔴 未连接</span><span class="text-xs text-gray-400">(请先输入 Access Token 并保存)</span></div>`)
}
