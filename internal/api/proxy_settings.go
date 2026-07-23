package api

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/httpx"
	"github.com/pokerjest/animateAutoTool/internal/jellyfin"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
)

var proxyProbeURL = "https://api.bgm.tv/calendar"

func configuredProxyURL(flagKey string) string {
	if flagKey != "" && configValue(flagKey) != model.ConfigValueTrue {
		return ""
	}

	normalized, err := httpx.NormalizeProxyURL(configValue(model.ConfigKeyProxyURL))
	if err != nil {
		log.Printf("Ignoring invalid configured proxy URL for %s: %v", flagKey, err)
		return ""
	}
	return normalized
}

func newConfiguredMikanParser() *parser.MikanParser {
	client := parser.NewMikanParser()
	if proxyURL := configuredProxyURL(model.ConfigKeyProxyMikan); proxyURL != "" {
		if err := client.SetProxy(proxyURL); err != nil {
			log.Printf("Failed to configure Mikan proxy: %v", err)
		}
	}
	return client
}

func newConfiguredJellyfinClient(baseURL, apiKey string) *jellyfin.Client {
	return jellyfin.NewClientWithProxy(baseURL, apiKey, configuredProxyURL(model.ConfigKeyProxyJellyfin))
}

func V1ProxyTestHandler(c *gin.Context) {
	var input struct {
		ProxyURL string `json:"proxy_url"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		v1Error(c, http.StatusBadRequest, "invalid_proxy_test", "代理测试请求格式不正确")
		return
	}
	normalized, err := httpx.NormalizeProxyURL(input.ProxyURL)
	if err != nil {
		v1Error(c, http.StatusBadRequest, "invalid_proxy_url", err.Error())
		return
	}
	if normalized == "" {
		v1Error(c, http.StatusBadRequest, "proxy_url_required", "请先填写代理地址")
		return
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, proxyProbeURL, nil)
	if err != nil {
		v1Error(c, http.StatusInternalServerError, "proxy_test_failed", "无法创建代理测试请求")
		return
	}
	client := httpx.NewHTTPClientWithProxy(10*time.Second, normalized)
	resp, err := client.Do(req)
	if err != nil {
		v1Error(c, http.StatusBadGateway, "proxy_unreachable", humanizeOperationError(err.Error()))
		return
	}
	defer safeio.Close(resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		v1Error(c, http.StatusBadGateway, "proxy_target_unreachable", "代理已响应，但无法访问测试目标（HTTP "+resp.Status+")")
		return
	}
	v1Data(c, http.StatusOK, gin.H{"connected": true, "detail": "代理连接成功", "protocol": strings.SplitN(normalized, ":", 2)[0]})
}
