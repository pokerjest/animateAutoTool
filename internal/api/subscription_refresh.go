package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
	"github.com/pokerjest/animateAutoTool/internal/runtimejournal"
	"github.com/pokerjest/animateAutoTool/internal/scheduler"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/pokerjest/animateAutoTool/internal/taskstate"
)

const subscriptionRefreshTaskID = "subscription-refresh"

var runSubscriptionRefreshNow = func(
	ctx context.Context,
	report service.SubscriptionRefreshProgressFunc,
) (service.SubscriptionRefreshResult, error) {
	qbCfg := qbutil.LoadConfig()
	switch {
	case qbutil.ManagedBinaryMissing(qbCfg, config.BinDir()):
		return service.SubscriptionRefreshResult{}, fmt.Errorf("未检测到可用的 qBittorrent，请先在设置中安装或配置下载器")
	case qbutil.MissingExternalURL(qbCfg), strings.TrimSpace(qbCfg.URL) == "":
		return service.SubscriptionRefreshResult{}, fmt.Errorf("外部 qBittorrent 模式缺少 WebUI 地址")
	}

	client := downloader.NewQBittorrentClient(qbCfg.URL)
	if err := client.LoginContext(ctx, qbCfg.Username, qbCfg.Password); err != nil {
		return service.SubscriptionRefreshResult{}, fmt.Errorf("qBittorrent 登录失败: %w", err)
	}
	return service.RefreshAndRepairSubscriptions(ctx, client, report)
}

func V1RefreshSubscriptionsHandler(c *gin.Context) {
	if runtimejournal.RecoveryBlocked() {
		v1Error(c, http.StatusServiceUnavailable, "database_recovery_blocked", "数据库完整性检查失败，订阅写入已停用，请先恢复数据库")
		return
	}
	if runtimejournal.RecoveryInProgress() {
		v1Error(c, http.StatusConflict, "runtime_recovery_running", "异常退出恢复正在运行，请等待恢复完成")
		return
	}
	if scheduler.IsRunInProgress() {
		v1Error(c, http.StatusConflict, "subscription_check_running", "自动订阅检查正在运行，请稍后再刷新")
		return
	}
	if task, ok := taskstate.Global.Get(subscriptionRefreshTaskID); ok && task.Status == taskstate.StatusRunning {
		v1Message(c, http.StatusAccepted, "订阅刷新任务正在运行", gin.H{"task_id": subscriptionRefreshTaskID, "status": "running"})
		return
	}

	taskstate.Global.Start(subscriptionRefreshTaskID, "subscription-refresh", "刷新并修复订阅", "正在核对下载器和订阅状态")
	go func() {
		result, err := runSubscriptionRefreshNow(context.Background(), func(progress service.SubscriptionRefreshProgress) {
			taskstate.Global.Progress(subscriptionRefreshTaskID, progress.Message, progress.Current, progress.Total)
		})
		if err != nil {
			taskstate.Global.Fail(subscriptionRefreshTaskID, err)
			return
		}
		taskstate.Global.Complete(subscriptionRefreshTaskID, result.Summary())
	}()

	v1Message(c, http.StatusAccepted, "订阅刷新与修复任务已经启动", gin.H{"task_id": subscriptionRefreshTaskID, "status": "running"})
}
