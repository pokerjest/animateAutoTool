package api

import "strings"

func humanizeOperationError(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "qb offline"),
		strings.Contains(lower, "qb timeout"),
		strings.Contains(lower, "invalid credentials"),
		strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "refused establish"),
		strings.Contains(lower, "qbit"),
		strings.Contains(lower, "qbittorrent"):
		return "无法连接 qBittorrent，请检查 WebUI 地址、账号或服务状态。"
	case strings.Contains(lower, "rss unavailable"),
		strings.Contains(lower, "rss"),
		strings.Contains(lower, "feed"):
		return "订阅源暂时不可用，请稍后重试或检查 RSS 配置。"
	case strings.Contains(lower, "permission denied"):
		return "权限不足，请检查目录或文件访问权限。"
	case strings.Contains(lower, "timeout"),
		strings.Contains(lower, "deadline exceeded"),
		strings.Contains(lower, "network error"):
		return "网络请求超时，请稍后重试。"
	case strings.Contains(lower, "no such file"),
		strings.Contains(lower, "does not exist"),
		strings.Contains(lower, "not found"):
		return "目标文件或资源不存在，建议重新扫描或检查路径配置。"
	default:
		return raw
	}
}
