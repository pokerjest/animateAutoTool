package api

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/httpx"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
)

func GetJellyfinStatusHandler(c *gin.Context) {
	style := c.Query("style")
	serveConnectionStatusFragment(c, jellyfinConnectionStatusView(""), RenderJellyfinStatus(style))
}

func RenderJellyfinStatusOOB() string {
	return renderConnectionStatusOOB(jellyfinConnectionStatusView(""), RenderJellyfinStatus(""))
}

func RenderJellyfinStatus(style string) string {
	connected, errStr := CheckJellyfinConnection()
	view := jellyfinConnectionStatusView("")
	return renderConnectionStatus(view, connected, errStr, style)
}

func CheckJellyfinConnection() (bool, string) {
	urlValue := configValue(model.ConfigKeyJellyfinUrl)
	apiKey := configValue(model.ConfigKeyJellyfinApiKey)

	if urlValue == "" || apiKey == "" {
		log.Printf("DEBUG: Jellyfin connection check failed: Config missing (hasURL=%t, hasKey=%t)", urlValue != "", apiKey != "")
		return false, "配置缺失"
	}

	proxyEnabled, proxyURL := loadProxySettings(model.ConfigKeyProxyJellyfin)
	probe := newConnectionProbe("jellyfin", urlValue, apiKey, proxyEnabled, proxyURL)
	if stat, ok := probe.load(); ok {
		return stat.Success, stat.Msg
	}

	apiURL := fmt.Sprintf("%s/System/Info", strings.TrimRight(urlValue, "/"))
	log.Printf("DEBUG: Testing Jellyfin connection to: %s", apiURL)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return false, fmt.Sprintf("内部请求创建失败: %v", err)
	}
	req.Header.Set("X-Emby-Token", apiKey)
	req.Header.Set("Accept", "application/json")

	client := httpx.NewHTTPClient(5 * time.Second)
	if proxyEnabled == ValueTrue && proxyURL != "" {
		client = httpx.NewHTTPClientWithProxy(5*time.Second, proxyURL)
	}

	resp, err := client.Do(req)
	if err != nil {
		probe.store(false, err.Error(), "", time.Minute)
		log.Printf("DEBUG: Jellyfin connection error: %v", err)
		return false, err.Error()
	}
	defer safeio.Close(resp.Body)

	log.Printf("DEBUG: Jellyfin connection response: %d", resp.StatusCode)

	if resp.StatusCode == http.StatusOK {
		probe.store(true, "", "", 5*time.Minute)
		return true, ""
	}

	errMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
	if resp.StatusCode == http.StatusUnauthorized {
		errMsg = "API Key 无效"
	}
	probe.store(false, errMsg, "", time.Minute)
	return false, errMsg
}

func JellyfinLoginHandler(c *gin.Context) {
	url := c.PostForm("jellyfin_url")
	username := c.PostForm("jellyfin_username")
	password := c.PostForm("jellyfin_password")

	if url == "" || username == "" {
		c.String(http.StatusOK, `<div id="jellyfin-login-status" class="text-red-500 text-sm mt-2">❌ 需要填写 URL 和 用户名</div>`)
		return
	}

	client := newConfiguredJellyfinClient(url, "")
	resp, err := client.AuthenticateContext(c.Request.Context(), username, password)
	if err != nil {
		c.String(http.StatusOK, fmt.Sprintf(`<div id="jellyfin-login-status" class="text-red-500 text-sm mt-2">❌ 登录失败: %s</div>`, err.Error()))
		return
	}

	successMsg := `<div id="jellyfin-login-status" class="text-emerald-600 font-bold text-sm mt-2 flex items-center gap-2">✅ 获取成功 <span class="text-xs font-normal text-gray-500">(Token 已自动填入)</span></div>`
	updateInput := fmt.Sprintf(`<input id="jellyfin_api_key_input" name="jellyfin_api_key" value="%s" type="text"
                                    class="w-full px-5 py-3 rounded-2xl bg-white/50 border border-gray-200 focus:bg-white focus:border-orange-300 focus:ring-4 focus:ring-orange-500/10 outline-none transition-all font-medium font-mono text-sm shadow-inner group-hover:border-orange-200"
                                    placeholder="输入 API Key，或使用下方登录自动获取" hx-swap-oob="true">`, resp.AccessToken)

	c.String(http.StatusOK, successMsg+updateInput)
}

func jellyfinConnectionStatusView(meta string) connectionStatusView {
	//nolint:gosec // UI labels mention API Key, but no credential literal is embedded here.
	return connectionStatusView{
		ID:             "jellyfin-status",
		ConnectedLabel: "已连接 Jellyfin",
		ConnectedMeta:  meta,
		MissingHint:    "请先输入 URL 和 API Key 并保存",
		MissingToken:   "配置缺失",
		InvalidToken:   "API Key 无效",
	}
}
