package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
	"github.com/pokerjest/animateAutoTool/internal/scheduler"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

var runDashboardSyncNow = func(ctx context.Context) error {
	var steps []string
	var errs []string

	schedulerMgr := scheduler.NewManagerWithContext(ctx)
	defer schedulerMgr.Stop()
	if scheduler.IsRunInProgress() {
		steps = append(steps, "订阅检查（已在运行，跳过重复触发）")
	} else {
		schedulerMgr.CheckUpdatesContext(ctx)
		steps = append(steps, "订阅检查")
	}

	scanner := service.NewScannerService()
	if err := scanner.ScanAll(); err != nil {
		errs = append(errs, fmt.Sprintf("本地扫描失败: %v", err))
	} else {
		steps = append(steps, "本地扫描")
		agent := service.NewAgentService()
		agent.RunAgentForLibrary()
		steps = append(steps, "媒体库整理")
	}

	qbCfg := qbutil.LoadConfig()
	if !qbutil.ManagedBinaryMissing(qbCfg, config.BinDir()) && !qbutil.MissingExternalURL(qbCfg) && strings.TrimSpace(qbCfg.URL) != "" {
		client := downloader.NewQBittorrentClient(qbCfg.URL)
		if err := client.LoginContext(ctx, qbCfg.Username, qbCfg.Password); err != nil {
			errs = append(errs, fmt.Sprintf("下载状态同步失败: %v", err))
		} else {
			if _, err := service.SyncDownloadLogStatusesWithQBClient(client); err != nil {
				errs = append(errs, fmt.Sprintf("qB 对账失败: %v", err))
			} else {
				steps = append(steps, "qB 对账")
			}

			if _, err := service.RepairDownloadLogsFromLocalLibrary(6 * time.Hour); err != nil {
				errs = append(errs, fmt.Sprintf("本地回补失败: %v", err))
			} else {
				steps = append(steps, "本地回补")
			}

			if _, err := service.ArchiveStaleDownloadLogs(client, 30*24*time.Hour); err != nil {
				errs = append(errs, fmt.Sprintf("归档旧下载记录失败: %v", err))
			} else {
				steps = append(steps, "旧日志归档")
			}
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "；"))
	}

	log.Printf("Dashboard sync completed: %s", strings.Join(steps, ", "))
	return nil
}

func DashboardSyncHandler(c *gin.Context) {
	go func() {
		if err := runDashboardSyncNow(context.Background()); err != nil {
			log.Printf("Dashboard sync failed: %v", err)
		}
	}()

	message := "已在后台启动订阅检查、本地扫描和下载状态同步"
	triggerAppToast(c, message, "success")
	c.JSON(http.StatusOK, gin.H{
		"status":  "started",
		"message": message,
	})
}
