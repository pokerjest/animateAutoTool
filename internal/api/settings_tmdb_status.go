package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/httpx"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
)

func GetTMDBStatusHandler(c *gin.Context) {
	style := c.Query("style")
	serveConnectionStatusFragment(c, tmdbConnectionStatusView(""), RenderTMDBStatus(style))
}

func RenderTMDBStatusOOB() string {
	return renderConnectionStatusOOB(tmdbConnectionStatusView(""), RenderTMDBStatus(""))
}

func RenderTMDBStatus(style string) string {
	connected, errStr := CheckTMDBConnection()
	view := tmdbConnectionStatusView("")
	return renderConnectionStatus(view, connected, errStr, style)
}

func CheckTMDBConnection() (bool, string) {
	token := configValue(model.ConfigKeyTMDBToken)
	if token == "" {
		return false, ErrTokenMissing
	}

	proxyEnabled, proxyURL := loadProxySettings(model.ConfigKeyProxyTMDB)
	probe := newConnectionProbe("tmdb", token, proxyEnabled, proxyURL)
	if stat, ok := probe.load(); ok {
		return stat.Success, stat.Msg
	}

	req, err := http.NewRequest("GET", "https://api.themoviedb.org/3/configuration", nil)
	if err != nil {
		return false, fmt.Sprintf("内部请求创建失败: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	client := httpx.NewHTTPClient(10 * time.Second)
	if proxyEnabled == ValueTrue && proxyURL != "" {
		client = httpx.NewHTTPClientWithProxy(10*time.Second, proxyURL)
	}

	resp, err := client.Do(req)
	if err != nil {
		probe.store(false, err.Error(), "", time.Minute)
		return false, err.Error()
	}
	defer safeio.Close(resp.Body)

	if resp.StatusCode == http.StatusOK {
		probe.store(true, "", "", 5*time.Minute)
		return true, ""
	}

	errMsg := "Token Invalid"
	if resp.StatusCode != http.StatusUnauthorized {
		errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	probe.store(false, errMsg, "", time.Minute)
	return false, errMsg
}

func tmdbConnectionStatusView(meta string) connectionStatusView {
	return connectionStatusView{
		ID:             "tmdb-status",
		ConnectedLabel: "已连接 TMDB",
		ConnectedMeta:  meta,
		MissingHint:    "请先输入 Token 并保存",
		MissingToken:   ErrTokenMissing,
		InvalidToken:   "Token Invalid",
	}
}
