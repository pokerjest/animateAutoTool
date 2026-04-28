package api

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/alist"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

// PikPakSyncHandler synchronizes PikPak settings to AList.
func PikPakSyncHandler(c *gin.Context) {
	username := c.PostForm("pikpak_username")
	password := c.PostForm("pikpak_password")
	refreshToken := c.PostForm("pikpak_refresh_token")
	captchaToken := c.PostForm("pikpak_captcha_token")

	if username == "" || password == "" {
		c.String(http.StatusOK, `<span class="text-red-500">❌ 请先填写 PikPak 用户名和密码</span>`)
		return
	}

	pikpakKeys := map[string]string{
		model.ConfigKeyPikPakUsername:     username,
		model.ConfigKeyPikPakPassword:     password,
		model.ConfigKeyPikPakRefreshToken: refreshToken,
	}
	if err := persistGlobalConfigs(pikpakKeys); err != nil {
		c.String(http.StatusInternalServerError, renderSettingsSaveError(fmt.Sprintf("保存 PikPak 配置失败: %v", err)))
		return
	}

	err := alist.AddPikPakStorage(username, password, refreshToken, captchaToken)
	if err != nil {
		log.Printf("Failed to sync PikPak to AList: %v", err)
		errStr := err.Error()
		if strings.Contains(errStr, "need verify") || strings.Contains(errStr, "Click Here") {
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
	html := ""

	if err != nil {
		html = fmt.Sprintf(`<div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ 读取状态失败: %s</div>`, err.Error())
	} else if status == "work" || status == "WORK" {
		html = `<div class="text-emerald-600 bg-emerald-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200">✅ 运行正常</div>`
	} else if status == "未配置" {
		html = `<div class="text-sm text-gray-500 flex items-center gap-2"><span>🔴 未配置</span><span class="text-xs text-gray-400">(请填写账号密码并点击保存连接)</span></div>`
	} else {
		html = fmt.Sprintf(`<div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">❌ 状态: %s</div>`, status)
	}

	c.String(http.StatusOK, html)
}
