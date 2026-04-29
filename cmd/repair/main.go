package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

type repairReport struct {
	DryRun                    bool     `json:"dry_run"`
	QBReachable               bool     `json:"qb_reachable"`
	SyncUpdated               int      `json:"sync_updated"`
	SyncCompleted             int      `json:"sync_completed"`
	SyncFailed                int      `json:"sync_failed"`
	SyncActive                int      `json:"sync_active"`
	SyncUnmatched             int      `json:"sync_unmatched"`
	LibraryScanned            int      `json:"library_scanned"`
	LibraryMatched            int      `json:"library_matched"`
	LibraryRepaired           int      `json:"library_repaired"`
	ArchivedScanned           int      `json:"archived_scanned"`
	ArchivedCount             int      `json:"archived_count"`
	ArchivedProtected         int      `json:"archived_protected"`
	RetriedAfterArchive       int      `json:"retried_after_archive"`
	RetriedStaleSubscriptions int      `json:"retried_stale_subscriptions"`
	PlannedActions            []string `json:"planned_actions,omitempty"`
	Recommendations           []string `json:"recommendations"`
}

func main() {
	jsonMode := flag.Bool("json", false, "以 JSON 输出修复结果")
	dryRun := flag.Bool("dry-run", false, "仅检查 qBittorrent 连通性并列出将执行的修复，不写库不触发重试")
	flag.Parse()

	if err := config.LoadConfig(""); err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	db.InitDB(config.AppConfig.Database.Path)

	report := repairReport{DryRun: *dryRun}
	qbCfg := qbutil.LoadConfig()
	if qbutil.ManagedBinaryMissing(qbCfg, config.BinDir()) || qbutil.MissingExternalURL(qbCfg) {
		report.Recommendations = append(report.Recommendations, "qBittorrent 当前不可用，请先检查下载器配置。")
		output(report, *jsonMode)
		return
	}

	client := downloader.NewQBittorrentClient(qbCfg.URL)
	if err := client.Login(qbCfg.Username, qbCfg.Password); err != nil {
		log.Fatalf("qBittorrent 登录失败: %v", err)
	}
	report.QBReachable = true

	if *dryRun {
		report.PlannedActions = []string{
			"同步 qBittorrent 与下载日志状态 (SyncDownloadLogStatusesWithQBClient)",
			"扫描本地媒体库回补缺失下载记录 (RepairDownloadLogsFromLocalLibrary, 6h 阈值)",
			"归档 30 天以上的陈旧下载日志 (ArchiveStaleDownloadLogs)",
			"对受影响订阅触发自动重试 (RetrySubscriptionsByID)",
			"对长期停滞超过 6h 的订阅触发重试 (RetryStaleSubscriptions)",
		}
		report.Recommendations = append(report.Recommendations, "dry-run 模式：未对数据库或订阅做任何写入；去掉 --dry-run 以正式执行。")
		output(report, *jsonMode)
		return
	}

	syncResult, err := service.SyncDownloadLogStatusesWithQBClient(client)
	if err != nil {
		log.Fatalf("同步下载日志状态失败: %v", err)
	}
	report.SyncUpdated = syncResult.Updated
	report.SyncCompleted = syncResult.Completed
	report.SyncFailed = syncResult.Failed
	report.SyncActive = syncResult.Active
	report.SyncUnmatched = syncResult.Unmatched

	repairResult, err := service.RepairDownloadLogsFromLocalLibrary(6 * time.Hour)
	if err != nil {
		log.Fatalf("本地媒体库回补失败: %v", err)
	}
	report.LibraryScanned = repairResult.Scanned
	report.LibraryMatched = repairResult.Matched
	report.LibraryRepaired = repairResult.Repaired

	archiveResult, err := service.ArchiveStaleDownloadLogs(client, 30*24*time.Hour)
	if err != nil {
		log.Fatalf("归档陈旧下载日志失败: %v", err)
	}
	report.ArchivedScanned = archiveResult.Scanned
	report.ArchivedCount = archiveResult.Archived
	report.ArchivedProtected = archiveResult.Protected

	if len(archiveResult.AffectedSubscriptionIDs) > 0 {
		if err := service.RetrySubscriptionsByID(context.Background(), client, archiveResult.AffectedSubscriptionIDs, "cli_repair"); err != nil {
			log.Fatalf("归档后的订阅重试失败: %v", err)
		}
		report.RetriedAfterArchive = len(archiveResult.AffectedSubscriptionIDs)
	}

	retriedStale, err := service.RetryStaleSubscriptions(context.Background(), client, 6*time.Hour, "cli_repair")
	if err != nil {
		log.Fatalf("长期停滞订阅重试失败: %v", err)
	}
	report.RetriedStaleSubscriptions = retriedStale

	if report.SyncUnmatched > 0 {
		report.Recommendations = append(report.Recommendations, "仍有未匹配的下载日志，建议在订阅页检查历史记录和标题映射。")
	}
	if report.ArchivedCount > 0 {
		report.Recommendations = append(report.Recommendations, "已归档一批陈旧日志，建议回到订阅页确认是否需要补缺集重检。")
	}
	if report.LibraryRepaired > 0 {
		report.Recommendations = append(report.Recommendations, "本地媒体库已经回补部分下载记录，可以在首页再执行一次同步确认状态。")
	}
	if len(report.Recommendations) == 0 {
		report.Recommendations = append(report.Recommendations, "本轮修复没有发现明显阻塞项，主链状态比较健康。")
	}

	output(report, *jsonMode)
}

func output(report repairReport, jsonMode bool) {
	if jsonMode {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			log.Fatalf("输出 JSON 失败: %v", err)
		}
		return
	}

	if report.DryRun {
		fmt.Println("== AnimateAutoTool Repair (DRY RUN) ==")
	} else {
		fmt.Println("== AnimateAutoTool Repair ==")
	}
	fmt.Printf("qBittorrent: %v\n", report.QBReachable)
	if report.DryRun {
		fmt.Println()
		fmt.Println("将要执行的操作:")
		for _, action := range report.PlannedActions {
			fmt.Println("- " + action)
		}
	} else {
		fmt.Printf("下载日志同步: updated=%d completed=%d failed=%d active=%d unmatched=%d\n",
			report.SyncUpdated, report.SyncCompleted, report.SyncFailed, report.SyncActive, report.SyncUnmatched)
		fmt.Printf("本地库回补: scanned=%d matched=%d repaired=%d\n",
			report.LibraryScanned, report.LibraryMatched, report.LibraryRepaired)
		fmt.Printf("陈旧日志归档: scanned=%d archived=%d protected=%d\n",
			report.ArchivedScanned, report.ArchivedCount, report.ArchivedProtected)
		fmt.Printf("自动恢复: archive_retry=%d stale_retry=%d\n",
			report.RetriedAfterArchive, report.RetriedStaleSubscriptions)
	}
	fmt.Println()
	fmt.Println("建议:")
	for _, recommendation := range report.Recommendations {
		fmt.Println("- " + recommendation)
	}
}
