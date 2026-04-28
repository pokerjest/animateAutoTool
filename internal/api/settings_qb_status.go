package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
)

// QBSaveAndTestHandler saves QB settings and then tests connection.
func QBSaveAndTestHandler(c *gin.Context) {
	qbValues := normalizedQBFormValues(c)
	if err := persistGlobalConfigs(map[string]string{
		model.ConfigKeyQBMode:     qbValues[model.ConfigKeyQBMode],
		model.ConfigKeyQBUrl:      qbValues[model.ConfigKeyQBUrl],
		model.ConfigKeyQBUsername: qbValues[model.ConfigKeyQBUsername],
		model.ConfigKeyQBPassword: qbValues[model.ConfigKeyQBPassword],
		model.ConfigKeyBaseDir:    strings.TrimSpace(c.PostForm(model.ConfigKeyBaseDir)),
	}); err != nil {
		c.String(http.StatusInternalServerError, renderSettingsSaveError(fmt.Sprintf("保存 qB 配置失败: %v", err)))
		return
	}

	statusCache.Delete("qb")
	c.String(http.StatusOK, `<div class="space-y-2"><div class="text-emerald-600 bg-emerald-50 px-3 py-1.5 rounded-lg text-sm font-medium border border-emerald-200 shadow-sm">qB 配置已保存。</div>`+renderQBStatusMessage(qbConfigFromForm(c))+`</div>`)
}

func TestConnectionHandler(c *gin.Context) {
	cfg := qbConfigFromForm(c)
	c.Header("Content-Type", "text/html")

	if qbutil.ManagedBinaryMissing(cfg, config.BinDir()) {
		c.String(http.StatusOK, `<div class="text-amber-600 bg-amber-50 px-3 py-1.5 rounded-lg text-sm font-medium flex items-center gap-2 border border-amber-200 shadow-sm">当前选择了托管 qBittorrent，但本地还没有安装对应二进制。</div>`)
		return
	}
	if qbutil.MissingExternalURL(cfg) {
		c.String(http.StatusBadRequest, `<div class="text-red-600 bg-red-50 px-3 py-1.5 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200 shadow-sm">外部 qBittorrent 模式需要填写 WebUI 地址。</div>`)
		return
	}

	c.String(http.StatusOK, renderQBStatusMessage(cfg))
}

// GetQBStatusHandler tests connection using stored config with caching.
func GetQBStatusHandler(c *gin.Context) {
	qbCfg := qbutil.LoadConfig()

	if qbutil.ManagedBinaryMissing(qbCfg, config.BinDir()) {
		c.String(http.StatusOK, `<div class="text-amber-600 bg-amber-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-amber-200">当前选择了托管 qBittorrent，但本地还没有安装对应二进制。</div>`)
		return
	}
	if qbutil.MissingExternalURL(qbCfg) {
		c.String(http.StatusOK, `<div class="text-amber-600 bg-amber-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-amber-200">当前启用了外部 qBittorrent 模式，但 WebUI 地址还是空的。</div>`)
		return
	}

	probe := newConnectionProbe("qb", qbCfg.Mode, qbCfg.URL, qbCfg.Username, qbCfg.Password)
	if stat, ok := probe.load(); ok {
		if stat.Success {
			c.String(http.StatusOK, fmt.Sprintf(`<div class="text-emerald-600 bg-emerald-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200">qB 已连接（缓存）%s</div>`, stat.Msg))
		} else {
			c.String(http.StatusOK, fmt.Sprintf(`<div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">qB 连接失败（缓存）: %s</div>`, stat.Msg))
		}
		return
	}

	status := evaluateQBStatus(qbCfg)
	switch status.Kind {
	case qbStatusConnected:
		probe.store(true, fmt.Sprintf("(version: %s)", status.Version), "", 5*time.Minute)
	case qbStatusVersionUnknown:
		probe.store(false, "Version unknown", "", 1*time.Minute)
	case qbStatusOffline:
		probe.store(false, strings.TrimPrefix(status.Message, "qB 连接失败: "), "", 1*time.Minute)
	}

	c.String(http.StatusOK, renderQBStatusMessage(qbCfg))
}
