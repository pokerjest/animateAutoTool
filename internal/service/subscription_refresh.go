package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
)

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

var newSubscriptionManagerForRefresh = func(source downloader.Downloader) *SubscriptionManager {
	return NewSubscriptionManager(source)
}

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
	Discovered       int
	CanonicalCount   int
	ResourceUpdates  int
	QBRecoveries     int
	AutoSubmitted    int
	Unresolved       int
	NeedsAttention   int
	Errors           []string
}

// RefreshAndRepairSubscriptions is non-destructive but complete: it first
// reconciles qB, local media, compatibility logs and RSS candidates, then
// submits only canonical episodes that have no qB task, local file, durable
// failed state or prior download history. It never archives/deletes records
// and never performs an implicit V2/V3 upgrade.
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

	total := int64(3 + len(subs))
	progress := func(message string, current int64) {
		if report != nil {
			report(SubscriptionRefreshProgress{Message: message, Current: current, Total: total})
		}
	}

	result := SubscriptionRefreshResult{}
	qbReady := true
	progress("正在同步 qBittorrent 下载进度和完成状态", 0)
	syncResult, err := SyncDownloadLogStatuses(source)
	if err != nil {
		qbReady = false
		result.Errors = append(result.Errors, fmt.Sprintf("qBittorrent 状态同步失败: %v", err))
		result.NeedsAttention++
	} else {
		GlobalDownloadLogSyncStatus.RecordSuccess(syncResult)
		result.SyncedLogs = syncResult.Updated
		result.CompletedLogs = syncResult.Completed
		result.QBRecoveries = syncResult.Updated
	}
	progress("下载状态已同步，正在用本地媒体库回补历史记录", 1)

	repairResult, err := RepairDownloadLogsFromLocalLibrary(0)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("本地下载历史回补失败: %v", err))
		result.NeedsAttention++
	} else {
		result.LibraryRepairs = repairResult.Repaired
	}
	progress("本地记录已核对，正在同步资源对账表", 2)
	resourceUpdates, err := ReconcileSubscriptionResourcesFromDownloadLogs()
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("资源记录对账失败: %v", err))
		result.NeedsAttention++
	} else {
		result.ResourceUpdates += resourceUpdates
	}

	updatedProgress, err := recalculateSubscriptionProgress(subs)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("订阅进度重算失败: %v", err))
		result.NeedsAttention++
	} else {
		result.ProgressUpdated = updatedProgress
	}

	manager := newSubscriptionManagerForRefresh(source)
	for index := range subs {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		sub := &subs[index]
		progress(fmt.Sprintf("正在对账 %s 的 RSS 候选", sub.Title), int64(3+index))
		discovery, discoveryErr := manager.DiscoverSubscriptionResourcesContext(ctx, sub)
		result.Checked++
		if discoveryErr != nil {
			result.FailedChecks++
			result.NeedsAttention++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", sub.Title, discoveryErr))
			continue
		}
		result.Discovered += discovery.RSSCount
		result.CanonicalCount += discovery.CanonicalCount
		result.ResourceUpdates += discovery.Updated
		result.Unresolved += discovery.Unresolved
		if discovery.Unresolved > 0 {
			result.WarningChecks++
			result.NeedsAttention += discovery.Unresolved
		}
		if !qbReady {
			result.WarningChecks++
			continue
		}

		// Discovery persisted all candidates first. The normal manager pass can
		// now submit only candidates still in "seen" state; completed,
		// downloading, failed and superseded resources remain protected.
		manager.ProcessSubscriptionWithSourceContext(ctx, sub, "manual")
		result.AutoSubmitted += sub.LastNewDownloads
		switch sub.LastRunStatus {
		case SubscriptionRunStatusError:
			result.FailedChecks++
			result.NeedsAttention++
		case SubscriptionRunStatusWarning:
			result.WarningChecks++
		default:
			result.SuccessfulChecks++
		}
	}
	progress("订阅对账完成，真正缺失的集数已补交下载", total)
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
		"对账完成：检查 %d 条订阅，发现 %d 条 RSS 候选（%d 个规范集数），修复 %d 条下载记录，补交 %d 集下载",
		r.Checked,
		r.Discovered,
		r.CanonicalCount,
		r.SyncedLogs+r.LibraryRepairs,
		r.AutoSubmitted,
	)
	if r.Unresolved > 0 || r.FailedChecks > 0 {
		summary += fmt.Sprintf("；%d 条未解析，%d 条订阅对账失败", r.Unresolved, r.FailedChecks)
	}
	summary += "；未自动删除或归档记录，V2/V3 仍需手动升级"
	return summary
}

var _ SubscriptionRefreshDownloader = (*downloader.QBittorrentClient)(nil)
