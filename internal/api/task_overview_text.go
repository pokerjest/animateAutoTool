package api

const (
	taskNameMetadataRefresh = "元数据刷新"
	taskNameDownloadSync    = "下载状态同步"
)

func taskNeverRunSummary(taskName string) string {
	switch taskName {
	case "订阅调度":
		return "还没有运行过订阅调度"
	case "本地扫描":
		return "还没有运行过本地库扫描"
	case taskNameMetadataRefresh:
		return "还没有运行过元数据全库刷新"
	case taskNameDownloadSync:
		return "下载状态同步尚未运行"
	default:
		return "该任务还没有运行过"
	}
}

func taskNeverRunDetail(taskName string) string {
	switch taskName {
	case "订阅调度":
		return "启动后会自动轮询，也可以在订阅页手动立即运行。"
	case "本地扫描":
		return "从本地番剧页触发扫描后，这里会展示最近一轮摘要。"
	case taskNameMetadataRefresh:
		return "媒体库页的刷新按钮会在这里显示进度和结果。"
	case taskNameDownloadSync:
		return "连接 qB 后，系统会自动回填下载日志、目标路径和完成状态。"
	default:
		return "任务启动后，这里会展示最新进度和结果。"
	}
}

func taskCompletedSummary(preferred, fallback string) string {
	return fallbackText(preferred, fallback)
}

func taskFollowupDetail(taskName string) string {
	switch taskName {
	case taskNameMetadataRefresh:
		return "可在媒体库页再次触发全量或增量刷新。"
	case taskNameDownloadSync:
		return "目标路径、完成状态和下载记录最近一轮同步正常。"
	default:
		return ""
	}
}
