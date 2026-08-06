package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	applogging "github.com/pokerjest/animateAutoTool/internal/logging"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/pokerjest/animateAutoTool/internal/taskstate"
	appversion "github.com/pokerjest/animateAutoTool/internal/version"
)

const diagnosticLogFileCount = 3
const healthDiagnosticLogFileCount = 24 * 7

var healthDiagnosticExportMu sync.Mutex

// V1ExportDiagnosticLogsHandler downloads the newest three hourly server logs
// as a ZIP without exposing their absolute paths.
func V1ExportDiagnosticLogsHandler(c *gin.Context) {
	path, filename, included, err := applogging.CreateRecentArchive(config.LogsDir(), "server", diagnosticLogFileCount, time.Now())
	if err != nil {
		if errors.Is(err, applogging.ErrNoHourlyLogs) {
			v1Error(c, http.StatusNotFound, "diagnostic_logs_not_found", "还没有可导出的服务日志，请先运行一段时间后再试")
			return
		}
		v1Error(c, http.StatusInternalServerError, "diagnostic_logs_export_failed", "打包诊断日志失败")
		return
	}
	defer func() { _ = os.Remove(path) }()

	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "application/zip")
	c.Header("X-Log-File-Count", strconv.Itoa(len(included)))
	c.FileAttachment(path, filename)
}

type healthDiagnosticManifest struct {
	ExportedAt             time.Time `json:"exported_at"`
	AppVersion             string    `json:"app_version"`
	GoVersion              string    `json:"go_version"`
	OS                     string    `json:"os"`
	Architecture           string    `json:"architecture"`
	HealthLogBehavior      string    `json:"health_log_behavior"`
	SecretsIncluded        bool      `json:"secrets_included"`
	MayContainMediaDetails bool      `json:"may_contain_media_details"`
}

type healthLibraryIssue struct {
	ID              uint       `json:"id"`
	IssueKey        string     `json:"issue_key"`
	IssueType       string     `json:"issue_type"`
	Title           string     `json:"title"`
	DirectoryPath   string     `json:"directory_path"`
	LocalAnimeID    *uint      `json:"local_anime_id,omitempty"`
	Message         string     `json:"message"`
	Hint            string     `json:"hint"`
	OccurrenceCount int        `json:"occurrence_count"`
	FirstSeenAt     time.Time  `json:"first_seen_at"`
	LastSeenAt      *time.Time `json:"last_seen_at,omitempty"`
}

type healthSubscriptionFailure struct {
	ID                  uint      `json:"id"`
	SubscriptionID      uint      `json:"subscription_id"`
	SubscriptionTitle   string    `json:"subscription_title"`
	CheckedAt           time.Time `json:"checked_at"`
	TriggerSource       string    `json:"trigger_source"`
	Status              string    `json:"status"`
	Summary             string    `json:"summary"`
	Error               string    `json:"error"`
	TotalEpisodes       int       `json:"total_episodes"`
	FilteredCount       int       `json:"filtered_count"`
	DuplicateCount      int       `json:"duplicate_count"`
	NewDownloads        int       `json:"new_downloads"`
	FailedDownloads     int       `json:"failed_downloads"`
	LastDownloadedTitle string    `json:"last_downloaded_title"`
}

type healthDownloadFailure struct {
	ID             uint      `json:"id"`
	SubscriptionID uint      `json:"subscription_id"`
	Title          string    `json:"title"`
	Episode        string    `json:"episode"`
	Season         string    `json:"season"`
	Status         string    `json:"status"`
	TargetFile     string    `json:"target_file"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type healthDatabaseSnapshot struct {
	Available       bool   `json:"available"`
	SchemaVersion   string `json:"schema_version,omitempty"`
	JournalMode     string `json:"journal_mode,omitempty"`
	BusyTimeoutMS   int    `json:"busy_timeout_ms,omitempty"`
	ForeignKeys     int    `json:"foreign_keys,omitempty"`
	OpenConnections int    `json:"open_connections,omitempty"`
	InUse           int    `json:"in_use,omitempty"`
	Idle            int    `json:"idle,omitempty"`
	Error           string `json:"error,omitempty"`
}

// V1ExportHealthDiagnosticsHandler exports the sparse issue log plus current
// state snapshots needed to reproduce defects that require a code change.
func V1ExportHealthDiagnosticsHandler(c *gin.Context) {
	healthDiagnosticExportMu.Lock()
	defer healthDiagnosticExportMu.Unlock()

	now := time.Now()
	attachments, err := buildHealthDiagnosticAttachments(now)
	if err != nil {
		log.Printf("ERROR: HealthDiagnostics: snapshot build failed error=%v", err)
		v1Error(c, http.StatusInternalServerError, "health_diagnostics_snapshot_failed", "生成健康诊断快照失败")
		return
	}
	path, filename, included, err := applogging.CreateHealthArchive(config.LogsDir(), "health", healthDiagnosticLogFileCount, now, attachments)
	if err != nil {
		log.Printf("ERROR: HealthDiagnostics: archive creation failed error=%v", err)
		v1Error(c, http.StatusInternalServerError, "health_diagnostics_export_failed", "打包健康诊断失败")
		return
	}
	defer func() { _ = os.Remove(path) }()

	healthLogCount := 0
	for _, name := range included {
		if strings.HasPrefix(name, "health-") && strings.HasSuffix(name, ".log") {
			healthLogCount++
		}
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "application/zip")
	c.Header("X-Health-Event-File-Count", strconv.Itoa(healthLogCount))
	c.Header("X-Diagnostic-Snapshot-Count", strconv.Itoa(len(included)-healthLogCount))
	c.Header("X-Health-Logs-Consumed", "true")
	c.FileAttachment(path, filename)
	if err := applogging.RemoveArchivedHourlyLogs(config.LogsDir(), "health", included); err != nil {
		// The archive has already been served successfully. Retaining an event
		// log is safer than turning a completed download into an HTTP failure.
		log.Printf("WARN: HealthDiagnostics: consumed log cleanup failed error=%v", err)
	}
	log.Printf(
		"HealthDiagnostics: export completed attachments=%d health_logs=%d filename=%s",
		len(attachments),
		healthLogCount,
		filename,
	)
}

func buildHealthDiagnosticAttachments(now time.Time) ([]applogging.ArchiveAttachment, error) {
	manifest := healthDiagnosticManifest{
		ExportedAt:             now,
		AppVersion:             appversion.AppVersion,
		GoVersion:              runtime.Version(),
		OS:                     runtime.GOOS,
		Architecture:           runtime.GOARCH,
		HealthLogBehavior:      "health-*.log 仅在检测到错误、失败、超时、HTTP 5xx、数据库锁或权限/磁盘异常时写入",
		SecretsIncluded:        false,
		MayContainMediaDetails: true,
	}
	issues, err := healthLibraryIssues()
	if err != nil {
		return nil, err
	}
	subscriptionFailures, err := healthSubscriptionFailures()
	if err != nil {
		return nil, err
	}
	downloadFailures, err := healthDownloadFailures()
	if err != nil {
		return nil, err
	}
	failedTasks := make([]taskstate.Task, 0)
	for _, task := range taskstate.Global.List() {
		if task.Status == taskstate.StatusError {
			failedTasks = append(failedTasks, task)
		}
	}
	currentProblems := buildCurrentProblemsText(buildHealthReport(), issues, failedTasks, subscriptionFailures, downloadFailures)

	values := []struct {
		name  string
		value any
	}{
		{name: "manifest.json", value: manifest},
		{name: "health-report.json", value: buildHealthReport()},
		{name: "runtime.json", value: buildRuntimeSnapshot()},
		{name: "database.json", value: buildHealthDatabaseSnapshot()},
		{name: "open-library-issues.json", value: issues},
		{name: "failed-tasks.json", value: failedTasks},
		{name: "failed-subscription-runs.json", value: subscriptionFailures},
		{name: "failed-downloads.json", value: downloadFailures},
	}
	attachments := []applogging.ArchiveAttachment{{
		Name: "README.txt",
		Data: []byte("AnimateTool 健康诊断包\n\n此压缩包用于提交给开发者处理无法通过界面修复、需要调整代码的问题。\nhealth-*.log 只记录异常事件，不包含普通运行流水。\nJSON 文件保存导出时的健康、运行时、数据库及失败任务快照。\ngoroutines.txt 保存导出瞬间的 Go goroutine 堆栈，用于定位卡死、泄漏和阻塞。\n密钥、Token 和密码不会写入快照；异常日志会遮盖常见凭据。\n注意：为便于定位扫描和媒体问题，条目名称与本地媒体路径可能包含在诊断中，请在分享前确认。\n"),
	}, {Name: "current-problems.txt", Data: []byte(currentProblems)}, {
		Name: "goroutines.txt",
		Data: buildGoroutineDump(),
	}}
	for _, item := range values {
		data, marshalErr := json.MarshalIndent(item.value, "", "  ")
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal %s: %w", item.name, marshalErr)
		}
		redacted, redactErr := redactHealthJSON(data)
		if redactErr != nil {
			return nil, fmt.Errorf("redact %s: %w", item.name, redactErr)
		}
		attachments = append(attachments, applogging.ArchiveAttachment{Name: item.name, Data: append(redacted, '\n')})
	}
	return attachments, nil
}

func buildGoroutineDump() []byte {
	size := 1 << 20
	for size <= 8<<20 {
		buffer := make([]byte, size)
		written := runtime.Stack(buffer, true)
		if written < len(buffer) {
			return buffer[:written]
		}
		size *= 2
	}
	return []byte("goroutine dump exceeded 8 MiB and was omitted\n")
}

// redactHealthJSON redacts string values after decoding the snapshot. Applying
// the line-oriented log redactor to JSON can replace a boolean or number with
// the bare token "[REDACTED]", which makes the exported file invalid JSON.
func redactHealthJSON(data []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	redactHealthJSONValue(&value, "")
	return json.MarshalIndent(value, "", "  ")
}

func redactHealthJSONValue(value *any, key string) {
	if value == nil || *value == nil {
		return
	}
	switch current := (*value).(type) {
	case string:
		if isHealthJSONSecretKey(key) {
			*value = "[REDACTED]"
		} else {
			*value = applogging.RedactHealthLogLine(current)
		}
	case []any:
		for index := range current {
			redactHealthJSONValue(&current[index], key)
		}
	case map[string]any:
		for childKey := range current {
			child := current[childKey]
			redactHealthJSONValue(&child, childKey)
			current[childKey] = child
		}
	}
}

func isHealthJSONSecretKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(key)))
	for _, suffix := range []string{"apikey", "accesstoken", "refreshtoken", "token", "password", "passwd", "secret", "authorization"} {
		if normalized == suffix || strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}

func buildCurrentProblemsText(report HealthReport, issues []healthLibraryIssue, tasks []taskstate.Task, subscriptionFailures []healthSubscriptionFailure, downloadFailures []healthDownloadFailure) string {
	var result strings.Builder
	result.WriteString("AnimateTool 当前问题快照\n")
	result.WriteString("========================\n\n")
	result.WriteString("综合判断：" + report.Summary + "\n")
	for _, recommendation := range report.Recommendations {
		result.WriteString("建议：" + recommendation + "\n")
	}
	fmt.Fprintf(&result, "\n媒体库问题：%d\n", len(issues))
	for _, issue := range issues {
		fmt.Fprintf(&result, "- [%s] %s（出现 %d 次）：%s\n", issue.IssueType, issue.Title, issue.OccurrenceCount, issue.Message)
		if issue.Hint != "" {
			result.WriteString("  当前建议：" + issue.Hint + "\n")
		}
	}
	fmt.Fprintf(&result, "\n失败任务：%d\n", len(tasks))
	for _, task := range tasks {
		fmt.Fprintf(&result, "- %s / %s：%s\n", task.Kind, task.Title, task.Message)
	}
	fmt.Fprintf(&result, "\n失败订阅检查：%d\n", len(subscriptionFailures))
	for _, item := range subscriptionFailures {
		fmt.Fprintf(&result, "- %s：%s %s\n", item.SubscriptionTitle, item.Summary, item.Error)
	}
	fmt.Fprintf(&result, "\n失败下载：%d\n", len(downloadFailures))
	for _, item := range downloadFailures {
		fmt.Fprintf(&result, "- %s %s：%s\n", item.Title, item.Episode, item.Status)
	}
	return applogging.RedactHealthLogLine(result.String())
}

func healthLibraryIssues() ([]healthLibraryIssue, error) {
	if _, err := loadSubscriptionLocalMatchIndex(); err != nil {
		log.Printf("WARN: failed to reconcile subscription conflicts before health snapshot: %v", err)
	}
	issues, err := service.ListOpenLibraryIssues(100)
	if err != nil {
		return nil, err
	}
	result := make([]healthLibraryIssue, 0, len(issues))
	for _, issue := range issues {
		result = append(result, healthLibraryIssue{
			ID: issue.ID, IssueKey: issue.IssueKey, IssueType: issue.IssueType, Title: issue.Title,
			DirectoryPath: issue.DirectoryPath, LocalAnimeID: issue.LocalAnimeID, Message: issue.Message,
			Hint: issue.Hint, OccurrenceCount: issue.OccurrenceCount, FirstSeenAt: issue.CreatedAt, LastSeenAt: issue.LastSeenAt,
		})
	}
	return result, nil
}

func healthSubscriptionFailures() ([]healthSubscriptionFailure, error) {
	if db.DB == nil {
		return []healthSubscriptionFailure{}, nil
	}
	var result []healthSubscriptionFailure
	err := db.DB.Table("subscription_run_logs").
		Select("subscription_run_logs.id, subscription_run_logs.subscription_id, COALESCE(subscriptions.title, '') AS subscription_title, subscription_run_logs.checked_at, subscription_run_logs.trigger_source, subscription_run_logs.status, subscription_run_logs.summary, subscription_run_logs.error, subscription_run_logs.total_episodes, subscription_run_logs.filtered_count, subscription_run_logs.duplicate_count, subscription_run_logs.new_downloads, subscription_run_logs.failed_downloads, subscription_run_logs.last_downloaded_title").
		Joins("LEFT JOIN subscriptions ON subscriptions.id = subscription_run_logs.subscription_id").
		Where(`NOT EXISTS (
			SELECT 1 FROM subscription_run_logs AS newer
			WHERE newer.subscription_id = subscription_run_logs.subscription_id
			  AND (newer.checked_at > subscription_run_logs.checked_at
			       OR (newer.checked_at = subscription_run_logs.checked_at AND newer.id > subscription_run_logs.id))
		)`).
		Where("subscription_run_logs.error <> '' OR subscription_run_logs.failed_downloads > 0 OR subscription_run_logs.status = ?", service.SubscriptionRunStatusError).
		Order("subscription_run_logs.checked_at DESC").Limit(100).Scan(&result).Error
	return result, err
}

func healthDownloadFailures() ([]healthDownloadFailure, error) {
	if db.DB == nil {
		return []healthDownloadFailure{}, nil
	}
	var result []healthDownloadFailure
	err := db.DB.Model(&model.DownloadLog{}).
		Select("id, subscription_id, title, episode, season_val AS season, status, target_file, updated_at").
		Where("status = ?", "failed").Order("updated_at DESC").Limit(100).Scan(&result).Error
	return result, err
}

func buildHealthDatabaseSnapshot() healthDatabaseSnapshot {
	result := healthDatabaseSnapshot{Available: db.DB != nil}
	if db.DB == nil {
		return result
	}
	result.SchemaVersion = db.CurrentSchemaVersion(db.DB)
	if err := db.DB.Raw("PRAGMA journal_mode").Scan(&result.JournalMode).Error; err != nil {
		result.Error = err.Error()
		return result
	}
	if err := db.DB.Raw("PRAGMA busy_timeout").Scan(&result.BusyTimeoutMS).Error; err != nil {
		result.Error = err.Error()
		return result
	}
	if err := db.DB.Raw("PRAGMA foreign_keys").Scan(&result.ForeignKeys).Error; err != nil {
		result.Error = err.Error()
		return result
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	stats := sqlDB.Stats()
	result.OpenConnections = stats.OpenConnections
	result.InUse = stats.InUse
	result.Idle = stats.Idle
	return result
}
