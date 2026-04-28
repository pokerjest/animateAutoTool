package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
)

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

func GetAniListStatusHandler(c *gin.Context) {
	style := c.Query("style")
	serveConnectionStatusFragment(c, aniListConnectionStatusView(""), RenderAniListStatus(style))
}

func RenderAniListStatusOOB() string {
	return renderConnectionStatusOOB(aniListConnectionStatusView(""), RenderAniListStatus(""))
}

func RenderAniListStatus(style string) string {
	connected, username, errStr := CheckAniListConnection()
	view := aniListConnectionStatusView(fmt.Sprintf("(%s)", username))
	return renderConnectionStatus(view, connected, errStr, style)
}

func CheckAniListConnection() (bool, string, string) {
	var config model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyAniListToken).First(&config).Error; err != nil || config.Value == "" {
		return false, "", ErrTokenMissing
	}

	token := strings.TrimSpace(config.Value)
	if lowerToken := strings.ToLower(token); strings.HasPrefix(lowerToken, "bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	if token == "" {
		return false, "", ErrTokenMissing
	}

	proxyEnabled, proxyURL := loadProxySettings(model.ConfigKeyProxyAniList)
	probe := newConnectionProbe("anilist", token, proxyEnabled, proxyURL)
	if stat, ok := probe.load(); ok {
		return stat.Success, stat.Msg2, stat.Msg
	}

	req, err := http.NewRequest("POST", "https://graphql.anilist.co", bytes.NewBufferString(`{"query": "{ Viewer { name id } }"}`))
	if err != nil {
		return false, "", fmt.Sprintf("内部请求创建失败: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	if transport := buildProxyTransport(proxyEnabled, proxyURL); transport != nil {
		client.Transport = transport
	}

	resp, err := client.Do(req)
	if err != nil {
		probe.store(false, err.Error(), "", time.Minute)
		return false, "", err.Error()
	}
	defer safeio.Close(resp.Body)

	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		probe.store(false, errMsg, "", time.Minute)
		return false, "", errMsg
	}

	var result AniListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, "", "响应解析失败"
	}
	if len(result.Errors) > 0 {
		return false, "", result.Errors[0].Message
	}

	probe.store(true, "", result.Data.Viewer.Name, 5*time.Minute)
	return true, result.Data.Viewer.Name, ""
}

func aniListConnectionStatusView(meta string) connectionStatusView {
	return connectionStatusView{
		ID:             "anilist-status",
		ConnectedLabel: "已连接 AniList",
		ConnectedMeta:  meta,
		MissingHint:    "请先输入 Token 并保存",
		MissingToken:   ErrTokenMissing,
		InvalidToken:   "401",
	}
}
