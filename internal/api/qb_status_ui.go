package api

import (
	"fmt"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
)

type qbStatusKind string

const (
	qbStatusManagedMissing qbStatusKind = "managed_missing"
	qbStatusMissingURL     qbStatusKind = "missing_url"
	qbStatusConnected      qbStatusKind = "connected"
	qbStatusOffline        qbStatusKind = "offline"
	qbStatusVersionUnknown qbStatusKind = "version_unknown"
)

type qbStatusView struct {
	Kind    qbStatusKind
	Message string
	Version string
	Title   string
}

func evaluateQBStatus(cfg qbutil.Config) qbStatusView {
	if qbutil.ManagedBinaryMissing(cfg, config.BinDir()) {
		return qbStatusView{
			Kind:    qbStatusManagedMissing,
			Message: "当前选择了托管 qBittorrent，但本地还没有安装对应二进制。",
			Title:   "当前选择了托管 qBittorrent，但本地还没有可用二进制",
		}
	}

	if qbutil.MissingExternalURL(cfg) {
		return qbStatusView{
			Kind:    qbStatusMissingURL,
			Message: "当前启用了外部 qBittorrent 模式，但 WebUI 地址还是空的。",
			Title:   "当前启用了外部 qBittorrent 模式，但 WebUI 地址还是空的",
		}
	}

	client := downloader.NewQBittorrentClient(cfg.URL)
	if err := client.Login(cfg.Username, cfg.Password); err != nil {
		return qbStatusView{
			Kind:    qbStatusOffline,
			Message: fmt.Sprintf("qB 连接失败: %v", err),
			Title:   fmt.Sprintf("qB 连接失败: %v", err),
		}
	}

	if ver, err := client.GetVersion(); err == nil {
		return qbStatusView{
			Kind:    qbStatusConnected,
			Message: fmt.Sprintf("qB 已连接（版本: %s）", ver),
			Version: ver,
			Title:   ver,
		}
	}

	return qbStatusView{
		Kind:    qbStatusVersionUnknown,
		Message: "qB 登录成功，但暂时没拿到版本信息。",
		Title:   "qB 登录成功，但暂时没拿到版本信息。",
	}
}

func renderQBStatusMessage(cfg qbutil.Config) string {
	status := evaluateQBStatus(cfg)
	switch status.Kind {
	case qbStatusManagedMissing, qbStatusMissingURL, qbStatusVersionUnknown:
		return fmt.Sprintf(`<div class="text-amber-600 bg-amber-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-amber-200">%s</div>`, status.Message)
	case qbStatusConnected:
		return fmt.Sprintf(`<div class="text-emerald-600 bg-emerald-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-emerald-200">%s</div>`, status.Message)
	default:
		return fmt.Sprintf(`<div class="text-red-600 bg-red-50 px-3 py-2 rounded-lg text-sm font-medium flex items-center gap-2 border border-red-200">%s</div>`, status.Message)
	}
}

func renderQBDashboardBadge(cfg qbutil.Config) string {
	status := evaluateQBStatus(cfg)
	switch status.Kind {
	case qbStatusManagedMissing:
		return fmt.Sprintf(`<div id="qb-status-dashboard" title="%s" class="text-amber-600 font-bold flex items-center gap-1.5 bg-amber-50 px-2 py-0.5 rounded-full text-xs"><span class="w-1.5 h-1.5 rounded-full bg-amber-500"></span> 缺失</div>`, status.Title)
	case qbStatusMissingURL:
		return fmt.Sprintf(`<div id="qb-status-dashboard" title="%s" class="text-amber-600 font-bold flex items-center gap-1.5 bg-amber-50 px-2 py-0.5 rounded-full text-xs"><span class="w-1.5 h-1.5 rounded-full bg-amber-500"></span> 待配置</div>`, status.Title)
	case qbStatusConnected:
		return fmt.Sprintf(`<span class="text-emerald-600 font-bold flex items-center gap-1.5 bg-emerald-50 px-2 py-0.5 rounded-full text-xs" title="%s"><span class="w-1.5 h-1.5 rounded-full bg-emerald-500"></span> 已连接（%s）</span>`, status.Title, status.Version)
	default:
		return `<span class="text-red-500 font-bold flex items-center gap-1.5 bg-red-50 px-2 py-0.5 rounded-full text-xs"><span class="w-1.5 h-1.5 rounded-full bg-red-500"></span> 离线</span>`
	}
}
