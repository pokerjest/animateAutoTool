package api

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

func getBangumiConfig() (string, string) {
	return configValue(model.ConfigKeyBangumiAppID), configValue(model.ConfigKeyBangumiAppSecret)
}

func getBangumiTokens() (string, string) {
	return configValue(model.ConfigKeyBangumiAccessToken), configValue(model.ConfigKeyBangumiRefreshToken)
}

func saveBangumiTokens(accessToken, refreshToken string) error {
	return persistGlobalConfigs(map[string]string{
		model.ConfigKeyBangumiAccessToken:  accessToken,
		model.ConfigKeyBangumiRefreshToken: refreshToken,
	})
}

func applyProxyToBangumiClient(client *bangumi.Client) {
	proxyURL := configuredProxyURL(model.ConfigKeyProxyBangumi)
	if proxyURL != "" {
		if err := client.SetProxy(proxyURL); err != nil {
			log.Printf("Failed to configure Bangumi proxy: %v", err)
		}
	}
}

func BangumiLoginHandler(c *gin.Context) {
	appID, appSecret := getBangumiConfig()
	if appID == "" || appSecret == "" {
		c.String(http.StatusBadRequest, "请先在设置中配置 Bangumi App ID 和 Secret")
		return
	}

	redirectURI := getBangumiRedirectURI(c)
	client := bangumi.NewClient(appID, appSecret, redirectURI)
	applyProxyToBangumiClient(client)
	url := client.GetAuthorizationURL()

	c.Redirect(http.StatusTemporaryRedirect, url)
}

func BangumiCallbackHandler(c *gin.Context) {
	code := c.Query("code")
	if code == "" {
		htmlBadRequest(c, "缺少 Bangumi 授权码")
		return
	}

	appID, appSecret := getBangumiConfig()
	redirectURI := getBangumiRedirectURI(c)
	client := bangumi.NewClient(appID, appSecret, redirectURI)
	applyProxyToBangumiClient(client)

	tokenResp, err := client.ExchangeToken(code)
	if err != nil {
		log.Printf("Bangumi exchange token error: %v", err)
		htmlServerError(c, "Bangumi 登录", err)
		return
	}

	if err := saveBangumiTokens(tokenResp.AccessToken, tokenResp.RefreshToken); err != nil {
		log.Printf("Bangumi token persistence error: %v", err)
		htmlServerError(c, "保存 Bangumi 授权", err)
		return
	}

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
				client := bangumi.NewClient(appID, appSecret, getBangumiRedirectURI(nil))
				applyProxyToBangumiClient(client)
				if tokenResp, errRefresh := client.RefreshToken(refreshToken); errRefresh == nil {
					if saveErr := saveBangumiTokens(tokenResp.AccessToken, tokenResp.RefreshToken); saveErr != nil {
						err = saveErr
					} else {
						user, err = client.GetCurrentUser(tokenResp.AccessToken)
					}
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
	html := renderBangumiContent()
	// Force Simple Return
	c.String(http.StatusOK, html)
}

func BangumiLogoutHandler(c *gin.Context) {
	// Clear tokens
	if err := saveBangumiTokens("", ""); err != nil {
		log.Printf("Bangumi token clear error: %v", err)
		c.String(http.StatusInternalServerError, "清除 Bangumi Token 失败")
		return
	}

	// Check if configured to show "Connect" button state
	c.String(http.StatusOK, `<div class="text-sm text-gray-500 flex items-center gap-2"><span>🔴 未连接</span><span class="text-xs text-gray-400">(请先输入 Access Token 并保存)</span></div>`)
}
