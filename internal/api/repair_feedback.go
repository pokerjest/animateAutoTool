package api

type repairAction string

const (
	repairActionUseBaseRSS     repairAction = "use_base_rss"
	repairActionClearFilter    repairAction = "clear_filter"
	repairActionResetStaleLog  repairAction = "reset_stale_log"
	repairActionRetryMissing   repairAction = "retry_missing"
	repairActionRetryStale     repairAction = "retry_stale"
	repairActionRetryUpgrade   repairAction = "retry_upgrade"
	repairActionRefreshLibrary repairAction = "refresh_library"
	repairActionRetryScrape    repairAction = "retry_scrape"
	repairActionSyncDownloads  repairAction = "sync_downloads"
)

func repairPendingSummary(action repairAction) string {
	switch action {
	case repairActionUseBaseRSS:
		return "已切回主 RSS，建议立即重新检查"
	case repairActionClearFilter:
		return "已清空过滤规则，建议立即重新检查"
	case repairActionResetStaleLog:
		return "已清理陈旧下载记录，建议立即重新检查"
	case repairActionRetryMissing:
		return "已触发缺集重检，建议查看最新订阅结果"
	case repairActionRetryStale:
		return "已触发重新检查，建议查看最新进展"
	case repairActionRetryUpgrade:
		return "已触发洗版检查，建议查看是否出现更优片源"
	case repairActionRefreshLibrary:
		return "已触发媒体库刷新，建议稍后确认是否进入可播放状态"
	default:
		return "已执行智能修复，建议立即重新检查"
	}
}

func repairActionLabel(action repairAction) string {
	switch action {
	case repairActionUseBaseRSS:
		return "已切回主 RSS"
	case repairActionClearFilter:
		return "已清空过滤规则"
	case repairActionResetStaleLog:
		return "已清理陈旧下载记录"
	case repairActionRetryMissing:
		return "已触发缺集重检"
	case repairActionRetryStale:
		return "已触发重新检查"
	case repairActionRetryUpgrade:
		return "已触发洗版检查"
	case repairActionRefreshLibrary:
		return "已触发媒体库刷新"
	case repairActionRetryScrape:
		return "已尝试重新抓取"
	default:
		return "已执行智能修复"
	}
}

func repairAutoRecheckFailureSummary(action repairAction) string {
	return repairActionLabel(action) + "，但自动重检未执行"
}

func repairSuccessToast(action repairAction) string {
	switch action {
	case repairActionUseBaseRSS:
		return "已执行智能修复，订阅已重新检查"
	case repairActionClearFilter:
		return "已清空过滤规则并重新检查"
	case repairActionResetStaleLog:
		return "已清理阻塞记录并重新检查"
	case repairActionRetryMissing:
		return "已启动缺集重检，请查看最新订阅结果"
	case repairActionRetryStale:
		return "已重新检查该订阅，请查看最新进展"
	case repairActionRetryUpgrade:
		return "已启动洗版检查，请查看是否出现更优资源"
	case repairActionRefreshLibrary:
		return "已请求刷新媒体库，请稍后查看是否进入可播放状态"
	case repairActionRetryScrape:
		return "本地番剧已完成重新抓取"
	case repairActionSyncDownloads:
		return "已完成下载状态修复，请查看任务总览"
	default:
		return "已执行智能修复"
	}
}

func repairReviewToast(action repairAction) string {
	switch action {
	case repairActionRetryScrape:
		return "已尝试重新抓取，请查看卡片诊断"
	case repairActionSyncDownloads:
		return "已尝试执行下载状态修复，请查看任务总览"
	default:
		return "已执行智能修复，请查看最新状态"
	}
}

func repairActionCTA(action repairAction) string {
	switch action {
	case repairActionSyncDownloads:
		return "立即修复"
	default:
		return "立即处理"
	}
}
