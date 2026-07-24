package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/scheduler"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestDashboardTaskOverviewEndpointRendersStatuses(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	scheduler.GlobalRunStatus.Begin("auto", 3)
	service.GlobalScanStatus.Begin(2)
	service.GlobalScanStatus.Advance("/library/a", 1, 0, nil)
	if service.GlobalRefreshStatus.TryStart() {
		service.GlobalRefreshStatus.SetTotal(5)
		service.GlobalRefreshStatus.UpdateProgress(2, "Test Metadata")
	}
	service.GlobalDownloadLogSyncStatus.RecordSuccess(service.DownloadLogStatusSyncResult{
		Updated:   2,
		Completed: 4,
		Active:    1,
	})
	t.Cleanup(func() {
		scheduler.GlobalRunStatus.Skip("auto", "待命")
		service.GlobalScanStatus.Skip("待命")
		service.GlobalRefreshStatus.Finish("已结束")
		service.GlobalDownloadLogSyncStatus.Reset()
	})

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/dashboard/task-overview", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected dashboard task overview endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "后台任务总览")
	assert.Contains(t, w.Body.String(), "订阅调度")
	assert.Contains(t, w.Body.String(), "扫描中")
	assert.Contains(t, w.Body.String(), "元数据刷新")
	assert.Contains(t, w.Body.String(), "Test Metadata")
	assert.Contains(t, w.Body.String(), "下载状态同步")
	assert.Contains(t, w.Body.String(), "最近同步：qB 修复 2 条")
}

func TestDashboardSyncHandlerStartsBackgroundTasksAndReturnsToast(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	original := runDashboardSyncNow
	triggered := make(chan struct{}, 1)
	runDashboardSyncNow = func(context.Context) error {
		select {
		case triggered <- struct{}{}:
		default:
		}
		return nil
	}
	t.Cleanup(func() {
		runDashboardSyncNow = original
	})

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/sync", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected sync endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case <-triggered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected dashboard sync to start background work")
	}

	assert.Contains(t, w.Body.String(), `"status":"started"`)
	assert.Contains(t, w.Body.String(), "已在后台启动订阅检查、本地扫描和下载状态同步")
	assert.Contains(t, w.Header().Get("HX-Trigger"), "app-toast")
}

func TestRunSubscriptionHandlerReturnsStructuredFailure(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	sub := model.Subscription{Title: "Retry Me", RSSUrl: "https://example.com/rss", IsActive: true}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	original := runSubscriptionCheck
	runSubscriptionCheck = func(sub *model.Subscription, source string) error {
		return fmt.Errorf("dial tcp 127.0.0.1:7603: connect: connection refused")
	}
	t.Cleanup(func() {
		runSubscriptionCheck = original
	})

	cookie, _ := loginCookie(t, r, "admin")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", fmt.Sprintf("/api/subscriptions/%d/run", sub.ID), nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected subscription retry failure to return 500, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "立即检查订阅失败")
	assert.NotContains(t, w.Body.String(), "<script>alert(")
}

func TestJoinHTMLListItemsEscapesWarnings(t *testing.T) {
	html := joinHTMLListItems([]string{`Jellyfin 自动登录失败: <token>`})
	assert.Contains(t, html, "Jellyfin 自动登录失败: &lt;token&gt;")
	assert.NotContains(t, html, "<token>")
}

func TestRuntimeStatsEndpointRequiresAuthAndReturnsMetrics(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	unauthorized := httptest.NewRecorder()
	unauthReq, _ := http.NewRequest("GET", "/api/runtime/stats", nil)
	markLocalRequest(unauthReq)
	r.ServeHTTP(unauthorized, unauthReq)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized request to fail with 401, got %d", unauthorized.Code)
	}

	cookie, _ := loginCookie(t, r, "admin")

	authorized := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/runtime/stats", nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(authorized, req)

	if authorized.Code != http.StatusOK {
		t.Fatalf("expected runtime stats endpoint to succeed, got %d: %s", authorized.Code, authorized.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(authorized.Body.Bytes(), &payload); err != nil {
		t.Fatalf("expected valid json response, got error: %v", err)
	}

	goInfo, ok := payload["go"].(map[string]any)
	if !ok {
		t.Fatal("expected response to include go runtime section")
	}
	if goroutines, ok := goInfo["goroutines"].(float64); !ok || goroutines < 1 {
		t.Fatalf("expected positive goroutine count, got: %#v", goInfo["goroutines"])
	}

	memoryInfo, ok := payload["memory"].(map[string]any)
	if !ok {
		t.Fatal("expected response to include memory section")
	}
	if _, ok := memoryInfo["heap_alloc_bytes"].(float64); !ok {
		t.Fatalf("expected heap_alloc_bytes to be numeric, got: %#v", memoryInfo["heap_alloc_bytes"])
	}
}

func TestDeploymentCheckEndpointRendersWarnings(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	prev := config.AppConfig
	t.Cleanup(func() {
		config.AppConfig = prev
	})

	config.AppConfig = &config.Config{
		Server: config.ServerConfig{
			Port:           8306,
			PublicURL:      "",
			TrustedProxies: []string{"0.0.0.0/0"},
		},
		Auth: config.AuthConfig{SecretKey: "short-secret"},
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/settings/deployment-check", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected deployment check endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "部署自检")
	assert.Contains(t, w.Body.String(), "还没有设置 server.public_url")
	assert.Contains(t, w.Body.String(), "trusted_proxies 过于宽泛")
}

func TestRenderLocalAnimeDiagnosticsIncludesRepairActions(t *testing.T) {
	animeID := uint(12)
	html, err := renderTemplateToString("local_anime_diagnostics.html", []model.LibraryIssue{
		{
			IssueType:     service.LibraryIssueTypeScan,
			Title:         "Scan Problem",
			DirectoryPath: "/library/scan-problem",
			Message:       "permission denied",
		},
		{
			IssueType:     service.LibraryIssueTypeScrape,
			Title:         "Scrape Problem",
			DirectoryPath: "/library/scrape-problem",
			LocalAnimeID:  &animeID,
			Message:       "tmdb token missing",
		},
	})
	if err != nil {
		t.Fatalf("expected diagnostics template to render, got error: %v", err)
	}

	assert.Contains(t, html, "重新扫描")
	assert.Contains(t, html, "重试刮削")
	assert.Contains(t, html, "修正匹配")
	assert.Contains(t, html, "打开详情")
}

func TestRenderDeploymentCheckTemplateIncludesSummary(t *testing.T) {
	html, err := renderTemplateToString("deployment_check.html", DeploymentCheckReport{
		PassCount: 1,
		WarnCount: 1,
		FailCount: 1,
		Items: []DeploymentCheckItem{
			{Name: "公网访问地址", Status: deploymentCheckFail, Summary: "server.public_url 不是 HTTPS", Action: "请改成 HTTPS"},
			{Name: "受信任代理", Status: deploymentCheckWarn, Summary: "当前只信任本机回环地址"},
			{Name: "会话密钥", Status: deploymentCheckPass, Summary: "会话密钥长度正常"},
		},
	})
	if err != nil {
		t.Fatalf("expected deployment check template to render, got error: %v", err)
	}

	assert.Contains(t, html, "部署自检")
	assert.Contains(t, html, "通过 1")
	assert.Contains(t, html, "注意 1")
	assert.Contains(t, html, "风险 1")
	assert.Contains(t, html, "server.public_url 不是 HTTPS")
}

func TestRenderSubscriptionTrendsTemplateIncludesSummary(t *testing.T) {
	html, err := renderTemplateToString("subscription_trends.html", SubscriptionTrendReport{
		WindowLabel:        "近 7 天",
		CheckedCount:       6,
		SuccessCount:       3,
		WarningCount:       2,
		ErrorCount:         1,
		RecentNewDownloads: 5,
		DownloadLogCount:   8,
		CompletedCount:     4,
		TopIssueSubscriptions: []SubscriptionTrendItem{
			{ID: 1, Title: "Flaky Show", Status: "error", StatusLabel: "失败", LastError: "qb offline", LastCheckLabel: "2 小时前"},
		},
		RecentWinningSubscriptions: []SubscriptionTrendItem{
			{ID: 2, Title: "Stable Show", LastRunSummary: "新增 3 集待下载", LastNewDownloads: 3, LastCheckLabel: "1 小时前"},
		},
	})
	if err != nil {
		t.Fatalf("expected subscription trends template to render, got error: %v", err)
	}

	assert.Contains(t, html, "订阅趋势")
	assert.Contains(t, html, "近 7 天")
	assert.Contains(t, html, "Flaky Show")
	assert.Contains(t, html, "Stable Show")
	assert.Contains(t, html, "新增下载")
}

func TestRenderBackupAnalyzeTemplateIncludesModeSpecificWarning(t *testing.T) {
	html, err := renderTemplateToString("backup_analyze.html", gin.H{
		"Stats": service.BackupDescriptor{
			Mode:              service.BackupModeCloudflare,
			ModeLabel:         service.BackupModeLabel(service.BackupModeCloudflare),
			Description:       service.BackupModeDescription(service.BackupModeCloudflare),
			ConfigStrategy:    "merge",
			GlobalConfigCount: 4,
			HasConfigs:        true,
		},
		"TempFile": "token-123",
	})
	if err != nil {
		t.Fatalf("expected backup analyze template to render, got error: %v", err)
	}

	assert.Contains(t, html, "Cloudflare 云存档凭据")
	assert.Contains(t, html, "合并")
	assert.Contains(t, html, "4 项设置")
	assert.Contains(t, html, `name="restore_configs" checked`)
}

func TestRenderSubscriptionsTemplateUsesDynamicCurrentYear(t *testing.T) {
	html, err := renderTemplateToString("subscriptions.html", SubscriptionsData{
		SkipLayout:    true,
		Subscriptions: []model.Subscription{},
	})
	if err != nil {
		t.Fatalf("expected subscriptions template to render, got error: %v", err)
	}

	assert.Contains(t, html, "new Date().getFullYear() - i")
	assert.NotContains(t, html, "(2025 - i).toString()")
	assert.Contains(t, html, "@app-toast.window")
	assert.Contains(t, html, "showNetworkFailure(error, fallback = '网络请求失败')")
	assert.Contains(t, html, "showFailure(msg, fallback = '操作失败')")
	assert.Contains(t, html, "detailsData?.air_date || '未知日期'")
}

func TestRenderLocalAnimeTemplateIncludesDiagnosticsRepairMethods(t *testing.T) {
	html, err := renderTemplateToString("local_anime.html", LocalAnimeData{
		SkipLayout: true,
		AnimeList:  []model.LocalAnime{},
	})
	if err != nil {
		t.Fatalf("expected local anime template to render, got error: %v", err)
	}

	assert.Contains(t, html, "preferredFixMatchSource(localAnimeId)")
	assert.Contains(t, html, "async retryScrapeIssue(localAnimeId, title)")
	assert.Contains(t, html, "openFixMatchForIssue(localAnimeId, title)")
	assert.Contains(t, html, "refreshScanStatus()")
	assert.Contains(t, html, "local-scan-status-container")
	assert.Contains(t, html, "showPlaybackFailure(error, diagnostic = null)")
	assert.Contains(t, html, "playbackDiagnostic")
	assert.Contains(t, html, "replaceLocalAnimeCard(id, html)")
	assert.Contains(t, html, "highlightRecoveredCard(id)")
	assert.Contains(t, html, "handleLibraryIssueUpdate(detail)")
	assert.Contains(t, html, "@app-toast.window")
	assert.Contains(t, html, "showNetworkFailure(error, fallback = '网络请求失败')")
	assert.Contains(t, html, "showFailure(msg, fallback = '操作失败')")
	assert.Contains(t, html, "detailsData?.air_date || '未知日期'")
}

func TestRenderLocalAnimeCardUsesAnimeRefreshEndpoint(t *testing.T) {
	html, err := renderTemplateToString("local_anime_card.html", model.LocalAnime{
		Model: gorm.Model{ID: 7},
		Title: "Refreshable Show",
	})
	if err != nil {
		t.Fatalf("expected local anime card template to render, got error: %v", err)
	}

	assert.Contains(t, html, `hx-post="/api/local-anime/7/refresh-metadata"`)
}

func TestRenderLocalAnimeCardIncludesRepairSection(t *testing.T) {
	html, err := renderTemplateToString("local_anime_card.html", model.LocalAnime{
		Model:            gorm.Model{ID: 8},
		Title:            "Broken Metadata Show",
		HasRepairActions: true,
		CanRetryScrape:   true,
		CanFixMatch:      true,
		RepairHint:       "当前本地番剧的元数据不完整，可先刷新，再视情况手动修正匹配。",
	})
	if err != nil {
		t.Fatalf("expected local anime card template to render, got error: %v", err)
	}

	assert.Contains(t, html, "智能修复建议")
	assert.Contains(t, html, "重新抓取")
	assert.Contains(t, html, "修正匹配")
}

func TestRepairFeedbackCatalogUsesConsistentLanguage(t *testing.T) {
	assert.Equal(t, "已切回主 RSS，建议立即重新检查", repairPendingSummary(repairActionUseBaseRSS))
	assert.Equal(t, "已清空过滤规则", repairActionLabel(repairActionClearFilter))
	assert.Equal(t, "已清理陈旧下载记录，但自动重检未执行", repairAutoRecheckFailureSummary(repairActionResetStaleLog))
	assert.Equal(t, "已触发缺集重检", repairActionLabel(repairActionRetryMissing))
}

func TestRepairFeedbackCatalogProvidesToastMessages(t *testing.T) {
	assert.Equal(t, "已执行智能修复，订阅已重新检查", repairSuccessToast(repairActionUseBaseRSS))
	assert.Equal(t, "已清理阻塞记录并重新检查", repairSuccessToast(repairActionResetStaleLog))
	assert.Equal(t, "已启动缺集重检，请查看最新订阅结果", repairSuccessToast(repairActionRetryMissing))
	assert.Equal(t, "已重新检查该订阅，请查看最新进展", repairSuccessToast(repairActionRetryStale))
	assert.Equal(t, "本地番剧已完成重新抓取", repairSuccessToast(repairActionRetryScrape))
	assert.Equal(t, "已完成下载状态修复，请查看任务总览", repairSuccessToast(repairActionSyncDownloads))
	assert.Equal(t, "已尝试重新抓取，请查看卡片诊断", repairReviewToast(repairActionRetryScrape))
	assert.Equal(t, "已尝试执行下载状态修复，请查看任务总览", repairReviewToast(repairActionSyncDownloads))
	assert.Equal(t, "立即修复", repairActionCTA(repairActionSyncDownloads))
}

func TestRenderLocalScanStatusTemplateIncludesSummary(t *testing.T) {
	now := time.Now()
	html, err := renderTemplateToString("local_scan_status.html", service.ScanRunStatus{
		IsRunning:            false,
		TotalDirectories:     3,
		ProcessedDirectories: 3,
		AddedCount:           4,
		UpdatedCount:         2,
		FailedDirectories:    1,
		LastStartedAt:        &now,
		LastFinishedAt:       &now,
		LastDuration:         "12 秒",
		LastSummary:          "最近一轮扫描了 3 个目录：新增 4，更新 2，失败 1",
		LastError:            "permission denied",
	})
	if err != nil {
		t.Fatalf("expected scan status template to render, got error: %v", err)
	}

	assert.Contains(t, html, "扫描任务摘要")
	assert.Contains(t, html, "最近一轮扫描了 3 个目录：新增 4，更新 2，失败 1")
	assert.Contains(t, html, "12 秒")
	assert.Contains(t, html, "权限不足，请检查目录或文件访问权限。")
	assert.Contains(t, html, `title="permission denied"`)
}

func TestRenderSettingsTemplateIncludesRuntimeStatsCard(t *testing.T) {
	html, err := renderTemplateToString("settings.html", gin.H{
		"SkipLayout":       true,
		"Config":           map[string]string{},
		"JellyfinServerID": "",
		"Stats":            BackupStats{},
	})
	if err != nil {
		t.Fatalf("expected settings template to render, got error: %v", err)
	}

	assert.Contains(t, html, "runtimeStatsCard()")
	assert.Contains(t, html, "/api/runtime/stats")
	assert.Contains(t, html, "运行时状态")
}

func TestRenderDashboardTemplateListensForAppToast(t *testing.T) {
	html, err := renderTemplateToString("index.html", DashboardData{
		SkipLayout:     true,
		ActiveSubs:     3,
		TodayDownloads: 7,
	})
	if err != nil {
		t.Fatalf("expected dashboard template to render, got error: %v", err)
	}

	assert.Contains(t, html, "@app-toast.window")
}

func TestRenderLibraryTemplateIncludesUnifiedToastHelpers(t *testing.T) {
	html, err := renderTemplateToString("library.html", gin.H{
		"SkipLayout": true,
		"Metadata":   []model.AnimeMetadata{},
	})
	if err != nil {
		t.Fatalf("expected library template to render, got error: %v", err)
	}

	assert.Contains(t, html, "showNetworkFailure(error, fallback = '网络请求失败')")
	assert.Contains(t, html, "showFailure(msg, fallback = '操作失败')")
	assert.Contains(t, html, "air_date: d.air_date || this.modalRaw.air_date")
}

func TestRenderIndexTemplateIncludesUnifiedToastHelpers(t *testing.T) {
	html, err := renderTemplateToString("index.html", DashboardData{
		SkipLayout: true,
	})
	if err != nil {
		t.Fatalf("expected dashboard template to render, got error: %v", err)
	}

	assert.Contains(t, html, "showNetworkFailure(error, fallback = '网络请求失败')")
	assert.Contains(t, html, "showFailure(msg, fallback = '操作失败')")
}

func TestRenderBackupTemplateIncludesStatusMessageHelpers(t *testing.T) {
	html, err := renderTemplateToString("backup.html", gin.H{
		"SkipLayout": true,
		"Stats":      BackupStats{},
	})
	if err != nil {
		t.Fatalf("expected backup template to render, got error: %v", err)
	}

	assert.Contains(t, html, "normalizeMessage(msg, fallback = '')")
	assert.Contains(t, html, "setStatus(target, message, isError = false, fallback = '')")
	assert.Contains(t, html, "请先选择备份文件")
}

func TestRenderSubscriptionHistoryUsesFriendlyErrors(t *testing.T) {
	now := time.Now()
	html, err := renderTemplateToString("subscription_history.html", SubscriptionHistoryData{
		Subscription: model.Subscription{
			Title:         "History Show",
			LastCheckAt:   &now,
			LastRunStatus: service.SubscriptionRunStatusWarning,
			LastError:     "qb offline",
		},
		Runs: []model.SubscriptionRunLog{
			{
				CheckedAt: time.Now(),
				Status:    service.SubscriptionRunStatusError,
				Error:     "rss unavailable",
			},
		},
	})
	if err != nil {
		t.Fatalf("expected subscription history template to render, got error: %v", err)
	}

	assert.Contains(t, html, "无法连接 qBittorrent，请检查 WebUI 地址、账号或服务状态。")
	assert.Contains(t, html, "订阅源暂时不可用，请稍后重试或检查 RSS 配置。")
	assert.Contains(t, html, `title="qb offline"`)
	assert.Contains(t, html, `title="rss unavailable"`)
}

func TestRenderLocalDiagnosticsUsesFriendlyErrors(t *testing.T) {
	html, err := renderTemplateToString("local_anime_diagnostics.html", []model.LibraryIssue{
		{
			IssueType: service.LibraryIssueTypeScan,
			Title:     "Broken Dir",
			Message:   "permission denied",
		},
	})
	if err != nil {
		t.Fatalf("expected diagnostics template to render, got error: %v", err)
	}

	assert.Contains(t, html, "权限不足，请检查目录或文件访问权限。")
	assert.Contains(t, html, `title="permission denied"`)
}

func TestRenderSubscriptionTrendsUsesFriendlyErrors(t *testing.T) {
	html, err := renderTemplateToString("subscription_trends.html", SubscriptionTrendReport{
		TopIssueSubscriptions: []SubscriptionTrendItem{
			{ID: 1, Title: "Flaky Show", Status: "error", StatusLabel: "失败", LastError: "qb offline", LastCheckLabel: "2 小时前"},
		},
	})
	if err != nil {
		t.Fatalf("expected subscription trends template to render, got error: %v", err)
	}

	assert.Contains(t, html, "无法连接 qBittorrent，请检查 WebUI 地址、账号或服务状态。")
	assert.Contains(t, html, `title="qb offline"`)
}

func TestRenderSubscriptionTrendsAvoidsConflictingEmptyState(t *testing.T) {
	html, err := renderTemplateToString("subscription_trends.html", SubscriptionTrendReport{
		WindowLabel:      "近 7 天",
		DownloadLogCount: 23,
		CompletedCount:   68,
	})
	if err != nil {
		t.Fatalf("expected subscription trends template to render, got error: %v", err)
	}

	assert.Contains(t, html, "最近仍有 23 条下载日志更新、68 条完成记录")
	assert.NotContains(t, html, "暂时没有新增下载的订阅。")
}

func TestTaskOverviewTextCatalogKeepsStatusLanguageConsistent(t *testing.T) {
	assert.Equal(t, "还没有运行过订阅调度", taskNeverRunSummary("订阅调度"))
	assert.Equal(t, "从本地番剧页触发扫描后，这里会展示最近一轮摘要。", taskNeverRunDetail("本地扫描"))
	assert.Equal(t, "最近一轮扫描已结束", taskCompletedSummary("", "最近一轮扫描已结束"))
	assert.Equal(t, "可在媒体库页再次触发全量或增量刷新。", taskFollowupDetail("元数据刷新"))
	assert.Equal(t, "目标路径、完成状态和下载记录最近一轮同步正常。", taskFollowupDetail("下载状态同步"))
}

func TestHumanizeOperationErrorUsesFriendlyExplanations(t *testing.T) {
	assert.Equal(t, "无法连接 qBittorrent，请检查 WebUI 地址、账号或服务状态。", humanizeOperationError("qb offline"))
	assert.Equal(t, "订阅源暂时不可用，请稍后重试或检查 RSS 配置。", humanizeOperationError("rss unavailable"))
	assert.Equal(t, "权限不足，请检查目录或文件访问权限。", humanizeOperationError("permission denied"))
}

func TestRenderTaskOverviewCardUsesFriendlyErrorButKeepsRawTitle(t *testing.T) {
	html, err := renderTemplateToString("dashboard_task_overview.html", TaskOverviewData{
		Downloads: TaskOverviewCard{
			Title:        "下载状态同步",
			StatusLabel:  "同步异常",
			StatusTone:   "rose",
			Summary:      "最近一次下载状态同步失败",
			Error:        "qb offline",
			DisplayError: humanizeOperationError("qb offline"),
		},
	})
	if err != nil {
		t.Fatalf("expected dashboard task overview template to render, got error: %v", err)
	}

	assert.Contains(t, html, "无法连接 qBittorrent，请检查 WebUI 地址、账号或服务状态。")
	assert.Contains(t, html, `title="qb offline"`)
}

func TestPopulateLocalAnimeActionHintForIncompleteMetadata(t *testing.T) {
	anime := model.LocalAnime{
		Model:   gorm.Model{ID: 99},
		Title:   "Incomplete Show",
		Image:   "",
		Summary: "",
	}

	populateLocalAnimeActionHint(&anime)

	assert.True(t, anime.HasRepairActions)
	assert.True(t, anime.CanRetryScrape)
	assert.True(t, anime.CanFixMatch)
	assert.Contains(t, anime.RepairHint, "元数据不完整")
}

func TestRenderSubscriptionCardTemplateIncludesRepairSection(t *testing.T) {
	html, err := renderTemplateToString("subscription_card.html", model.Subscription{
		Model:            gorm.Model{ID: 12},
		Title:            "Repairable Show",
		LastRunStatus:    service.SubscriptionRunStatusIdle,
		LastRunSummary:   "当前字幕组 RSS 为空（ANi），但该番剧主 RSS 还有 3 集可用",
		HasRepairActions: true,
		CanUseBaseRSS:    true,
	})
	if err != nil {
		t.Fatalf("expected subscription card template to render, got error: %v", err)
	}

	assert.Contains(t, html, "智能修复建议")
	assert.Contains(t, html, "切回主 RSS")
}
