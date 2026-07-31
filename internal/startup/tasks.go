package startup

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
	"github.com/pokerjest/animateAutoTool/internal/runtimejournal"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/pokerjest/animateAutoTool/internal/taskstate"
	"github.com/pokerjest/animateAutoTool/internal/updater"
	appversion "github.com/pokerjest/animateAutoTool/internal/version"
	"github.com/pokerjest/animateAutoTool/internal/worker"
)

type recoveryCompletion struct {
	done            chan struct{}
	allowBackground bool
}

// Run performs runtime-only initialization that should not happen as a side
// effect of constructing HTTP routes.
func Run(ctx context.Context) func() {
	sessionResult, err := runtimejournal.BeginSession(appversion.AppVersion)
	recovery := completedRecovery(true)
	if err != nil {
		log.Printf("ERROR: failed to create durable runtime session marker: %v", err)
	} else {
		recovery = handlePreviousRuntimeSession(ctx, sessionResult)
	}

	go func() {
		select {
		case <-recovery.done:
			if !recovery.allowBackground {
				log.Printf("ERROR: background workers remain disabled because startup recovery is blocked")
				return
			}
			if ctx.Err() != nil {
				return
			}
			service.NewAuthService().EnsureDefaultUser()
			service.NewScannerService().CleanupGarbage()
			worker.StartMetadataWorker()
			service.NewMetadataService().StartMetadataMigration()
			worker.StartDownloadLogSyncWorker(ctx)
			updater.Start()
		case <-ctx.Done():
		}
	}()

	startRuntimeMonitor()
	return func() {
		// Do not close SQLite while a crash-recovery pass is still reconciling
		// media, metadata, or subscription state.
		<-recovery.done
		if err := runtimejournal.EndSession(); err != nil {
			log.Printf("WARN: failed to clear runtime session marker during shutdown: %v", err)
		}
	}
}

func handlePreviousRuntimeSession(ctx context.Context, result runtimejournal.StartResult) *recoveryCompletion {
	recovery := &recoveryCompletion{done: make(chan struct{})}
	if result.PreviousError != "" {
		log.Printf("ERROR: previous runtime journal could not be read completely: %s", result.PreviousError)
	}
	if result.Previous == nil {
		recovery.allowBackground = true
		close(recovery.done)
		return recovery
	}

	previous := result.Previous
	operations := make([]string, 0, len(previous.Operations))
	for _, operation := range previous.Operations {
		if name := strings.TrimSpace(operation.Name); name != "" {
			operations = append(operations, name)
		}
		for _, recovered := range operation.RecoveryOf {
			if recovered = strings.TrimSpace(recovered); recovered != "" {
				operations = append(operations, recovered)
			}
		}
	}
	operations = uniqueOperationNames(operations)
	operationSummary := "none"
	if len(operations) > 0 {
		operationSummary = strings.Join(operations, ",")
	}
	log.Printf(
		"WARN: unclean shutdown detected previous_pid=%d previous_version=%s started_at=%s interrupted_operations=%s; "+
			"the process may have crashed, been force-killed, or lost host power",
		previous.PID,
		previous.AppVersion,
		previous.StartedAt.Format(time.RFC3339),
		operationSummary,
	)

	if err := db.CheckIntegrity(db.DB); err != nil {
		log.Printf("ERROR: database integrity check after unclean shutdown failed: %v", err)
		_ = service.ReportLibraryIssue(service.LibraryIssueInput{
			IssueKey:  "runtime:database-integrity",
			IssueType: service.LibraryIssueTypeScan,
			Title:     "数据库完整性检查失败",
			Message:   err.Error(),
			Hint:      "请停止服务并从最近的本地安全快照或备份恢复数据库；恢复前不要继续执行扫描或订阅任务。",
		})
		runtimejournal.SetRecoveryBlocked(true)
		close(recovery.done)
		return recovery
	}
	runtimejournal.SetRecoveryBlocked(false)
	_ = service.ResolveLibraryIssue("runtime:database-integrity")

	needsMediaRecovery := containsOperation(operations, runtimejournal.OperationLocalLibraryScan) ||
		containsOperation(operations, runtimejournal.OperationMetadataEnrich)
	needsSubscriptionRecovery := containsOperation(operations, runtimejournal.OperationSubscriptionSync)
	if !needsMediaRecovery && !needsSubscriptionRecovery {
		log.Printf("WARN: previous process ended without a clean shutdown marker; SQLite integrity check passed")
		recovery.allowBackground = true
		close(recovery.done)
		return recovery
	}

	const taskID = "startup-crash-recovery"
	taskstate.Global.Start(taskID, "recovery", "异常退出恢复", "正在恢复上次被中断的数据操作")
	runtimejournal.SetRecoveryInProgress(true)
	if err := runtimejournal.BeginRecoveryOperation(operations); err != nil {
		log.Printf("WARN: failed to persist startup recovery marker: %v", err)
	}
	go func() {
		recoverySucceeded := true
		defer func() {
			// Only a failed integrity check blocks normal workers. Other
			// recovery failures remain visible as tasks/issues while the
			// scheduler and download worker keep retrying when dependencies
			// such as qBittorrent come back online.
			recovery.allowBackground = true
			close(recovery.done)
		}()
		defer func() {
			if recoverySucceeded {
				if err := runtimejournal.EndOperation(runtimejournal.OperationStartupRecovery); err != nil {
					log.Printf("WARN: failed to clear startup recovery marker: %v", err)
				}
			}
		}()
		defer runtimejournal.SetRecoveryInProgress(false)

		if needsMediaRecovery {
			taskstate.Global.Progress(taskID, "正在重新扫描媒体库并核对元数据链接", 0, 3)
			scanner := service.NewScannerService()
			if err := scanner.ScanAllWithProgress(func(progress service.ScanProgress) {
				taskstate.Global.Progress(taskID, progress.Message, progress.Current, progress.Total)
			}); err != nil {
				recoverySucceeded = false
				taskstate.Global.Fail(taskID, fmt.Errorf("异常退出后的媒体库重扫失败: %w", err))
				return
			}
			task, _ := taskstate.Global.Get(taskID)
			taskstate.Global.Progress(taskID, "文件重扫完成，正在恢复元数据并复核订阅链接", task.Current, task.Total)
			if _, err := service.NewAgentService().RunAgentForLibraryWithRepair(nil); err != nil {
				recoverySucceeded = false
				taskstate.Global.Fail(taskID, fmt.Errorf("异常退出后的元数据恢复失败: %w", err))
				return
			}
			taskstate.Global.Progress(taskID, "元数据恢复完成，正在复核订阅与本地媒体链接", 2, 3)
			if _, err := service.RepairDownloadLogsFromLocalLibrary(0); err != nil {
				recoverySucceeded = false
				taskstate.Global.Fail(taskID, fmt.Errorf("异常退出后的订阅链接复核失败: %w", err))
				return
			}
		}

		if needsSubscriptionRecovery {
			taskstate.Global.Progress(taskID, "正在恢复被中断的订阅对账", 2, 3)
			if err := recoverInterruptedSubscriptionSync(ctx); err != nil {
				recoverySucceeded = false
				_ = service.ReportLibraryIssue(service.LibraryIssueInput{
					IssueKey:  "runtime:subscription-recovery",
					IssueType: service.LibraryIssueTypeScan,
					Title:     "订阅异常退出恢复未完成",
					Message:   err.Error(),
					Hint:      "服务会保留现有下载和媒体文件；请确认 qBittorrent 与网络恢复后，在订阅页执行“刷新并修复”。",
				})
				taskstate.Global.Fail(taskID, fmt.Errorf("异常退出后的订阅对账恢复失败: %w", err))
				return
			}
			_ = service.ResolveLibraryIssue("runtime:subscription-recovery")
		}

		taskstate.Global.Complete(taskID, "异常退出恢复完成，媒体库、元数据和订阅链接已重新核对")
		log.Printf("Runtime recovery completed interrupted_operations=%s", operationSummary)
	}()
	return recovery
}

func recoverInterruptedSubscriptionSync(ctx context.Context) error {
	if _, err := service.RepairDownloadLogsFromLocalLibrary(0); err != nil {
		return fmt.Errorf("回补本地下载记录: %w", err)
	}
	if _, err := service.ReconcileSubscriptionResourcesFromDownloadLogs(); err != nil {
		return fmt.Errorf("重建订阅资源状态: %w", err)
	}

	qbCfg := qbutil.LoadConfig()
	switch {
	case qbutil.ManagedBinaryMissing(qbCfg, config.BinDir()):
		return fmt.Errorf("未检测到可用的 qBittorrent")
	case qbutil.MissingExternalURL(qbCfg), strings.TrimSpace(qbCfg.URL) == "":
		return fmt.Errorf("qBittorrent WebUI 地址未配置")
	}

	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		client := downloader.NewQBittorrentClient(qbCfg.URL)
		loginCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		lastErr = client.LoginContext(loginCtx, qbCfg.Username, qbCfg.Password)
		cancel()
		if lastErr == nil {
			result, err := service.RefreshAndRepairSubscriptions(ctx, client, nil)
			if err != nil {
				return err
			}
			log.Printf("Subscription crash recovery completed: %s", result.Summary())
			return nil
		}
		log.Printf("WARN: qBittorrent unavailable during subscription recovery attempt=%d/5 error=%v", attempt, lastErr)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * 2 * time.Second):
		}
	}
	return fmt.Errorf("qBittorrent 连续重试后仍不可用: %w", lastErr)
}

func containsOperation(operations []string, target string) bool {
	for _, operation := range operations {
		if operation == target {
			return true
		}
	}
	return false
}

func uniqueOperationNames(operations []string) []string {
	seen := make(map[string]struct{}, len(operations))
	unique := make([]string, 0, len(operations))
	for _, operation := range operations {
		if _, ok := seen[operation]; ok {
			continue
		}
		seen[operation] = struct{}{}
		unique = append(unique, operation)
	}
	return unique
}

func completedRecovery(allowBackground bool) *recoveryCompletion {
	recovery := &recoveryCompletion{
		done:            make(chan struct{}),
		allowBackground: allowBackground,
	}
	close(recovery.done)
	return recovery
}
