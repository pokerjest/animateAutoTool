package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
)

const subscriptionRefreshArchiveAge = 24 * time.Hour

type SubscriptionRefreshDownloader interface {
	downloader.Downloader
	TorrentStatusSource
}

type SubscriptionRefreshProgress struct {
	Message string
	Current int64
	Total   int64
}

type SubscriptionRefreshProgressFunc func(SubscriptionRefreshProgress)

type SubscriptionRefreshResult struct {
	SyncedLogs       int
	CompletedLogs    int
	LibraryRepairs   int
	ArchivedLogs     int
	ProgressUpdated  int
	Checked          int
	SuccessfulChecks int
	WarningChecks    int
	FailedChecks     int
}

// RefreshAndRepairSubscriptions reconciles persisted download state before it
// evaluates RSS feeds again. This order matters: a completed local/qB task must
// be restored to history first, otherwise the following subscription check can
// incorrectly classify the same episode as missing and submit it again.
func RefreshAndRepairSubscriptions(
	ctx context.Context,
	source SubscriptionRefreshDownloader,
	report SubscriptionRefreshProgressFunc,
) (SubscriptionRefreshResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if source == nil {
		return SubscriptionRefreshResult{}, fmt.Errorf("qBittorrent 客户端不可用")
	}

	subStore := subscriptionStore()
	if subStore == nil {
		return SubscriptionRefreshResult{}, fmt.Errorf("数据库未初始化")
	}
	subs, err := subStore.ListActive()
	if err != nil {
		return SubscriptionRefreshResult{}, fmt.Errorf("读取活跃订阅: %w", err)
	}

	total := int64(4 + len(subs))
	progress := func(message string, current int64) {
		if report != nil {
			report(SubscriptionRefreshProgress{Message: message, Current: current, Total: total})
		}
	}

	result := SubscriptionRefreshResult{}
	progress("正在同步 qBittorrent 下载进度和完成状态", 0)
	syncResult, err := SyncDownloadLogStatuses(source)
	if err != nil {
		return result, fmt.Errorf("同步 qBittorrent 下载状态: %w", err)
	}
	GlobalDownloadLogSyncStatus.RecordSuccess(syncResult)
	result.SyncedLogs = syncResult.Updated
	result.CompletedLogs = syncResult.Completed
	progress("下载状态已同步，正在用本地媒体库回补历史记录", 1)

	repairResult, err := RepairDownloadLogsFromLocalLibrary(0)
	if err != nil {
		return result, fmt.Errorf("回补本地下载历史: %w", err)
	}
	result.LibraryRepairs = repairResult.Repaired
	progress("本地记录已核对，正在归档失效或被替代的旧记录", 2)

	archiveResult, err := ArchiveStaleDownloadLogs(source, subscriptionRefreshArchiveAge)
	if err != nil {
		return result, fmt.Errorf("归档失效下载记录: %w", err)
	}
	result.ArchivedLogs = archiveResult.Archived
	progress("旧记录已归档，正在重新计算每条订阅的已下载集数与缺集状态", 3)

	updatedProgress, err := recalculateSubscriptionProgress(subs)
	if err != nil {
		return result, fmt.Errorf("重新计算订阅进度: %w", err)
	}
	result.ProgressUpdated = updatedProgress
	progress("订阅进度已重算，正在重新检查活跃订阅", 4)

	manager := NewSubscriptionManager(source)
	for index := range subs {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		sub := &subs[index]
		progress(fmt.Sprintf("正在重新检查 %s", sub.Title), int64(4+index))
		manager.ProcessSubscriptionWithSourceContext(ctx, sub, "manual")
		result.Checked++
		switch sub.LastRunStatus {
		case SubscriptionRunStatusError:
			result.FailedChecks++
		case SubscriptionRunStatusWarning:
			result.WarningChecks++
		default:
			result.SuccessfulChecks++
		}
	}
	progress("订阅刷新与修复完成", total)
	return result, nil
}

func recalculateSubscriptionProgress(subs []model.Subscription) (int, error) {
	logStore := downloadLogStore()
	subStore := subscriptionStore()
	if logStore == nil || subStore == nil {
		return 0, fmt.Errorf("数据库未初始化")
	}

	updated := 0
	for index := range subs {
		sub := &subs[index]
		logs, err := logStore.ListBySubscriptionAndStatuses(
			sub.ID,
			[]string{downloadLogStatusDownloading, downloadLogStatusCompleted, downloadLogStatusRenamed},
		)
		if err != nil {
			return updated, err
		}

		lastEpisode := 0
		for _, logEntry := range logs {
			episode := parser.NormalizeEpisodeNumber(strings.TrimSpace(logEntry.Episode))
			if episode == "" {
				episode = parser.EpisodeNumberFromTitle(logEntry.Title)
			}
			if episode == "" {
				continue
			}
			value, parseErr := strconv.ParseFloat(episode, 64)
			if parseErr != nil {
				continue
			}
			if candidate := int(value); candidate > lastEpisode {
				lastEpisode = candidate
			}
		}
		if lastEpisode == sub.LastEp {
			continue
		}
		if err := subStore.SetLastEpisode(sub.ID, lastEpisode); err != nil {
			return updated, err
		}
		sub.LastEp = lastEpisode
		updated++
	}
	return updated, nil
}

func (r SubscriptionRefreshResult) Summary() string {
	summary := fmt.Sprintf(
		"刷新完成：检查 %d 条订阅，修复 %d 条下载记录，归档 %d 条旧记录",
		r.Checked,
		r.SyncedLogs+r.LibraryRepairs,
		r.ArchivedLogs,
	)
	if r.WarningChecks > 0 || r.FailedChecks > 0 {
		summary += fmt.Sprintf("；%d 条警告，%d 条失败", r.WarningChecks, r.FailedChecks)
	}
	return summary
}

var _ SubscriptionRefreshDownloader = (*downloader.QBittorrentClient)(nil)
