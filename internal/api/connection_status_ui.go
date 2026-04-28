package api

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

type connectionStatusView struct {
	ID             string
	ConnectedLabel string
	ConnectedMeta  string
	MissingHint    string
	MissingToken   string
	InvalidToken   string
}

func renderConnectionStatusOOB(view connectionStatusView, content string) string {
	return strings.Replace(content, fmt.Sprintf(`id="%s"`, view.ID), fmt.Sprintf(`id="%s" hx-swap-oob="innerHTML"`, view.ID), 1)
}

func serveConnectionStatusFragment(c *gin.Context, view connectionStatusView, content string) {
	c.Header("Content-Type", "text/html")
	c.String(200, trimConnectionStatusWrapper(view, content))
}

func trimConnectionStatusWrapper(view connectionStatusView, html string) string {
	openTag := fmt.Sprintf(`<div id="%s">`, view.ID)
	if strings.Contains(html, openTag) {
		html = strings.Replace(html, openTag, "", 1)
		html = strings.TrimSuffix(html, "</div>")
	}
	return html
}

func renderConnectionDashboardStatus(connected bool, errStr, invalidTokenMarker string) string {
	if connected {
		return StatusConnectedHTML
	}
	errText := StatusNotConnected
	if invalidTokenMarker != "" && strings.Contains(errStr, invalidTokenMarker) {
		errText = "未连接（凭据无效）"
	} else if errStr != "" {
		errText = StatusConnectionFail
	}
	return fmt.Sprintf(`<span class="text-red-500 font-bold flex items-center gap-1" title="%s"><span class="w-2 h-2 rounded-full bg-red-500"></span> %s</span>`, errStr, errText)
}

func renderConnectionSettingsStatus(view connectionStatusView, connected bool, errStr string) string {
	if connected {
		label := view.ConnectedLabel
		if strings.TrimSpace(view.ConnectedMeta) != "" {
			label += " " + view.ConnectedMeta
		}
		return fmt.Sprintf(`<div id="%s"><div class="text-emerald-600 bg-emerald-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200">✅ %s</div></div>`, view.ID, label)
	}

	if errStr == view.MissingToken {
		return fmt.Sprintf(`<div id="%s"><div class="text-sm text-gray-500 flex items-center gap-2"><span>🔴 未连接</span><span class="text-xs text-gray-400">(%s)</span></div></div>`, view.ID, view.MissingHint)
	}

	if view.InvalidToken != "" && strings.Contains(errStr, view.InvalidToken) {
		return fmt.Sprintf(`<div id="%s"><div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ 认证失败（%s）</div></div>`, view.ID, errStr)
	}

	return fmt.Sprintf(`<div id="%s"><div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ 连接失败: %s</div></div>`, view.ID, errStr)
}

func renderConnectionStatus(view connectionStatusView, connected bool, errStr string, style string) string {
	if style == StyleDashboard {
		return renderConnectionDashboardStatus(connected, errStr, view.InvalidToken)
	}
	return renderConnectionSettingsStatus(view, connected, errStr)
}
