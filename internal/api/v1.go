package api

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/ai"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/httpx"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"github.com/pokerjest/animateAutoTool/internal/taskstate"
	appversion "github.com/pokerjest/animateAutoTool/internal/version"
	"gorm.io/gorm"
)

type v1Envelope struct {
	Data    any            `json:"data,omitempty"`
	Meta    map[string]any `json:"meta,omitempty"`
	Message string         `json:"message,omitempty"`
}

type v1ErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func v1Data(c *gin.Context, status int, data any) {
	c.JSON(status, v1Envelope{Data: data})
}

func v1Message(c *gin.Context, status int, message string, data any) {
	c.JSON(status, v1Envelope{Data: data, Message: message})
}

func v1Page(c *gin.Context, items any, page, pageSize int, total int64) {
	c.JSON(http.StatusOK, v1Envelope{Data: gin.H{"items": items}, Meta: map[string]any{"page": page, "page_size": pageSize, "total": total}})
}

func v1Pagination(c *gin.Context, defaultSize int) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", strconv.Itoa(defaultSize)))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = defaultSize
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}

func v1Error(c *gin.Context, status int, code, message string) {
	payload := v1ErrorEnvelope{}
	payload.Error.Code = code
	payload.Error.Message = message
	c.AbortWithStatusJSON(status, payload)
}

func mapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func initV1Routes(r *gin.Engine) {
	v1 := r.Group("/api/v1")
	{
		v1.GET("/session", V1SessionHandler)
		v1.POST("/session/login", SameOriginMiddleware(), V1LoginHandler)
		v1.POST("/session/bootstrap", DirectLocalOnlyMiddleware(), SameOriginMiddleware(), V1BootstrapSessionHandler)
	}

	recovery := v1.Group("/recovery")
	recovery.Use(DirectLocalOnlyMiddleware(), SameOriginMiddleware())
	{
		recovery.POST("/reset", V1RecoveryHandler)
	}

	protected := v1.Group("")
	protected.Use(AuthMiddleware(), SameOriginMiddleware())
	{
		protected.POST("/session/logout", V1LogoutHandler)
		protected.POST("/session/change-password", V1ChangePasswordHandler)
		protected.GET("/events", SSEHandler)
		protected.GET("/tasks", V1TasksHandler)
		protected.GET("/tasks/:task_id", V1TaskHandler)

		protected.GET("/setup/readiness", V1SetupReadinessHandler)
		protected.POST("/setup/bootstrap", V1SetupBootstrapHandler)
		protected.POST("/system/pick-directory", V1PickDirectoryHandler)

		protected.GET("/dashboard", V1DashboardHandler)
		protected.POST("/tasks/sync", V1SyncHandler)
		protected.GET("/subscriptions", V1SubscriptionsHandler)
		protected.POST("/subscriptions", V1CreateSubscriptionHandler)
		protected.POST("/subscriptions/batch", V1BatchCreateHandler)
		protected.POST("/subscriptions/batch-preview", V1BatchPreviewHandler)
		protected.GET("/subscriptions/validate-rss", V1ValidateRSSHandler)
		protected.GET("/subscriptions/search", V1MikanSearchHandler)
		protected.GET("/subscriptions/rss-preview", V1RSSPreviewHandler)
		protected.GET("/subscriptions/mikan/dashboard", V1MikanDashboardHandler)
		protected.GET("/subscriptions/mikan/episodes", V1MikanEpisodesHandler)
		protected.GET("/subscriptions/mikan/subgroups", V1MikanSubgroupsHandler)
		protected.PUT("/subscriptions/:id", V1UpdateSubscriptionHandler)
		protected.POST("/subscriptions/:id/toggle", V1SubscriptionActionHandler("toggle"))
		protected.POST("/subscriptions/:id/run", V1SubscriptionActionHandler("run"))
		protected.POST("/subscriptions/:id/repair/:action", V1SubscriptionRepairHandler)
		protected.POST("/subscriptions/:id/refresh-metadata", V1RefreshSubscriptionMetadataHandler)
		protected.POST("/subscriptions/:id/source", V1SubscriptionSourceHandler)
		protected.DELETE("/subscriptions/:id", V1DeleteSubscriptionHandler)
		protected.GET("/subscriptions/:id/history", V1SubscriptionHistoryHandler)

		protected.GET("/calendar", V1CalendarHandler)
		protected.GET("/calendar/posters/:id", V1CalendarPosterHandler)
		protected.GET("/library", V1LibraryHandler)
		protected.POST("/library/refresh", V1RefreshLibraryHandler)
		protected.POST("/library/metadata/:id/refresh", V1RefreshMetadataItemHandler)
		protected.POST("/library/fix-match", V1FixMatchHandler)
		protected.GET("/metadata/search", V1MetadataSearchHandler)
		protected.GET("/posters/:id", GetPosterHandler)

		protected.GET("/local-anime", V1LocalAnimeHandler)
		protected.GET("/local-anime/:id/episodes", V1LocalAnimeEpisodesHandler)
		protected.GET("/local-anime/:id/files", V1LocalAnimeFilesHandler)
		protected.POST("/local-anime/scan", V1LocalScanHandler)
		protected.POST("/local-directories", V1AddLocalDirectoryHandler)
		protected.DELETE("/local-directories/:id", V1DeleteLocalDirectoryHandler)
		protected.POST("/local-anime/:id/refresh-metadata", V1RefreshLocalMetadataHandler)
		protected.POST("/local-anime/:id/source", V1LocalAnimeSourceHandler)
		protected.POST("/local-directories/:id/rename-preview", V1RenamePreviewHandler)
		protected.POST("/local-directories/:id/rename", V1RenameApplyHandler)
		protected.GET("/jellyfin/stream/:id", ProxyVideoHandler)
		protected.GET("/jellyfin/play/:id", GetPlayInfoHandler)
		protected.POST("/jellyfin/progress", ReportProgressHandler)
		protected.POST("/bangumi/subject/:id/collection", V1BangumiCollectionHandler)
		protected.POST("/bangumi/subject/:id/progress", V1BangumiProgressHandler)

		protected.GET("/backup", V1BackupHandler)
		protected.GET("/backup/export", ExportBackupHandler)
		protected.POST("/backup/analyze", V1AnalyzeBackupHandler)
		protected.POST("/backup/restore", V1RestoreBackupHandler)
		protected.GET("/backup/r2/list", V1R2ListHandler)
		protected.POST("/backup/r2/upload", V1R2UploadHandler)
		protected.POST("/backup/r2/stage", V1R2StageHandler)
		protected.GET("/backup/r2/progress/:taskId", V1R2ProgressHandler)
		protected.POST("/backup/r2/test", V1R2TestHandler)
		protected.POST("/backup/r2/delete", V1DeleteR2Handler)

		protected.GET("/health", V1HealthHandler)
		protected.GET("/runtime", V1RuntimeHandler)
		protected.GET("/audit-logs", V1AuditLogsHandler)
		protected.GET("/settings", V1SettingsHandler)
		protected.PUT("/settings", V1UpdateSettingsHandler)
		protected.POST("/settings/proxy/test", V1ProxyTestHandler)
		protected.GET("/settings/connections/:provider", V1ConnectionStatusHandler)
		protected.GET("/settings/maintenance", V1MaintenanceHandler)
		protected.POST("/settings/updater/:action", V1UpdaterActionHandler)
		protected.GET("/settings/ai", V1AIStatusHandler)
		protected.GET("/settings/ai/models", V1AIModelsHandler)
		protected.POST("/assistant/messages", V1AssistantMessageHandler)
		protected.DELETE("/assistant/messages", V1AssistantClearHandler)
	}
}

func V1TasksHandler(c *gin.Context) {
	v1Data(c, http.StatusOK, gin.H{"items": taskstate.Global.List()})
}

func V1TaskHandler(c *gin.Context) {
	task, ok := taskstate.Global.Get(c.Param("task_id"))
	if !ok {
		v1Error(c, http.StatusNotFound, "task_not_found", "未找到对应任务")
		return
	}
	v1Data(c, http.StatusOK, task)
}

func V1SessionHandler(c *gin.Context) {
	setupPending := bootstrap.BootstrapSetupPending()
	data := gin.H{
		"authenticated":            false,
		"setup_pending":            setupPending,
		"local_setup_available":    setupPending && requestIsDirectLoopback(c),
		"local_recovery_available": requestIsDirectLoopback(c),
		"version":                  appversion.AppVersion,
		"recovery_local_only":      true,
	}
	if user, err := currentSessionUser(c); err == nil && user != nil {
		data["authenticated"] = true
		data["username"] = user.Username
	}
	v1Data(c, http.StatusOK, data)
}

func V1BootstrapSessionHandler(c *gin.Context) {
	bootstrapInfo, pending := bootstrap.PendingAdminBootstrapInfo()
	if !pending {
		v1Error(c, http.StatusConflict, "setup_not_pending", "首次初始化已经完成")
		return
	}

	user, err := store.NewUserStore(db.DB).GetByUsername(bootstrapInfo.Username)
	if err != nil {
		v1Error(c, http.StatusInternalServerError, "bootstrap_user_unavailable", "无法读取初始化管理员账户")
		return
	}

	session := sessions.Default(c)
	session.Set("user_id", user.ID)
	session.Options(sessionCookieOptions(c, 0))
	if err := session.Save(); err != nil {
		v1Error(c, http.StatusInternalServerError, "session_save_failed", "无法保存初始化登录状态")
		return
	}

	clearFailedLoginAttempts(requestClientIP(c))
	auditCtx := auditContextForLogin(c, user.Username)
	auditCtx.UserID = user.ID
	service.RecordAudit(auditCtx, service.AuditEntry{
		Action:  service.AuditActionLoginSuccess,
		Outcome: service.AuditOutcomeSuccess,
		Details: map[string]any{"method": "local_bootstrap"},
	})
	v1Message(c, http.StatusOK, "已建立本机初始化会话", gin.H{"setup_pending": true})
}

func V1LoginHandler(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		v1Error(c, http.StatusBadRequest, "invalid_request", "登录请求格式不正确")
		return
	}
	clientIP := requestClientIP(c)
	if retryAfter, blocked := checkLoginThrottle(clientIP); blocked {
		c.Header("Retry-After", strconv.Itoa(max(1, int(retryAfter.Seconds()))))
		v1Error(c, http.StatusTooManyRequests, "login_throttled", "登录尝试过于频繁，请稍后再试")
		return
	}
	user, err := service.NewAuthService().Login(req.Username, req.Password)
	if err != nil {
		registerFailedLoginAttempt(clientIP)
		service.RecordAudit(auditContextForLogin(c, req.Username), service.AuditEntry{Action: service.AuditActionLoginFailure, Outcome: service.AuditOutcomeFailure, Details: map[string]string{"reason": "invalid_credentials"}})
		v1Error(c, http.StatusUnauthorized, "invalid_credentials", "用户名或密码不正确")
		return
	}
	clearFailedLoginAttempts(clientIP)
	session := sessions.Default(c)
	session.Set("user_id", user.ID)
	maxAge := 0
	if req.RememberMe {
		maxAge = 3600 * 24 * 30
	}
	session.Options(sessionCookieOptions(c, maxAge))
	if err := session.Save(); err != nil {
		v1Error(c, http.StatusInternalServerError, "session_save_failed", "无法保存登录状态")
		return
	}
	auditCtx := auditContextForLogin(c, user.Username)
	auditCtx.UserID = user.ID
	service.RecordAudit(auditCtx, service.AuditEntry{Action: service.AuditActionLoginSuccess, Outcome: service.AuditOutcomeSuccess, Details: map[string]any{"remember_me": req.RememberMe}})
	v1Message(c, http.StatusOK, "登录成功", gin.H{"setup_pending": bootstrap.BootstrapSetupPending()})
}

func V1LogoutHandler(c *gin.Context) {
	auditCtx := buildAuditContext(c)
	session := sessions.Default(c)
	session.Clear()
	session.Options(sessionCookieOptions(c, -1))
	if err := session.Save(); err != nil {
		v1Error(c, http.StatusInternalServerError, "session_save_failed", "无法保存退出状态")
		return
	}
	service.RecordAudit(auditCtx, service.AuditEntry{Action: service.AuditActionLogout, Outcome: service.AuditOutcomeSuccess})
	v1Message(c, http.StatusOK, "已退出登录", nil)
}

func V1ChangePasswordHandler(c *gin.Context) {
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(strings.TrimSpace(req.NewPassword)) < 8 {
		v1Error(c, http.StatusBadRequest, "invalid_password", "新密码至少需要 8 个字符")
		return
	}
	uid, err := currentSessionUserID(c)
	if err != nil {
		v1Error(c, http.StatusUnauthorized, "unauthorized", "当前登录状态已失效")
		return
	}
	if err := service.NewAuthService().ChangePassword(uid, req.OldPassword, strings.TrimSpace(req.NewPassword)); err != nil {
		service.RecordAudit(buildAuditContext(c), service.AuditEntry{Action: service.AuditActionPasswordChange, Outcome: service.AuditOutcomeFailure, Details: map[string]string{"error": err.Error()}})
		v1Error(c, http.StatusBadRequest, "password_change_failed", err.Error())
		return
	}
	service.RecordAudit(buildAuditContext(c), service.AuditEntry{Action: service.AuditActionPasswordChange, Outcome: service.AuditOutcomeSuccess})
	v1Message(c, http.StatusOK, "密码修改成功", nil)
}

func V1RecoveryHandler(c *gin.Context) {
	var req LocalRecoveryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		v1Error(c, http.StatusBadRequest, "invalid_request", "本地重置密码请求格式不正确")
		return
	}
	req.Username, req.Password, req.Confirm = strings.TrimSpace(req.Username), strings.TrimSpace(req.Password), strings.TrimSpace(req.Confirm)
	if req.Username == "" || len(req.Password) < 8 || req.Password != req.Confirm {
		v1Error(c, http.StatusBadRequest, "invalid_password", "请填写用户名，并确保两次输入的密码一致且不少于 8 个字符")
		return
	}
	if info, pending := bootstrap.PendingAdminBootstrapInfo(); pending && req.Username != info.Username {
		v1Error(c, http.StatusBadRequest, "bootstrap_user_mismatch", "首次初始化期间只能重置 bootstrap 管理员")
		return
	}
	if err := service.NewAuthService().ResetPasswordByUsername(req.Username, req.Password); err != nil {
		service.RecordAudit(auditContextForLogin(c, req.Username), service.AuditEntry{Action: service.AuditActionPasswordRecoveryLoc, Outcome: service.AuditOutcomeFailure, Details: map[string]string{"error": err.Error()}})
		v1Error(c, http.StatusBadRequest, "password_reset_failed", err.Error())
		return
	}
	clearFailedLoginAttempts(requestClientIP(c))
	service.RecordAudit(auditContextForLogin(c, req.Username), service.AuditEntry{Action: service.AuditActionPasswordRecoveryLoc, Outcome: service.AuditOutcomeSuccess})
	v1Message(c, http.StatusOK, "密码重置成功", nil)
}

func V1SetupReadinessHandler(c *gin.Context) {
	v1Data(c, http.StatusOK, SetupReadinessResponse{Services: collectSetupReadinessStatuses()})
}

func V1SetupBootstrapHandler(c *gin.Context) {
	v1RunJSONHandler(c, CompleteBootstrapSetupHandler)
}

func V1DashboardHandler(c *gin.Context) {
	var active, downloads, libraryItems, localSeries, openIssues int64
	db.DB.Model(&model.Subscription{}).Where("is_active = ?", true).Count(&active)
	db.DB.Model(&model.DownloadLog{}).Count(&downloads)
	db.DB.Model(&model.AnimeMetadata{}).Count(&libraryItems)
	db.DB.Model(&model.LocalAnime{}).Count(&localSeries)
	db.DB.Model(&model.LibraryIssue{}).Where("status = ?", service.LibraryIssueStatusOpen).Count(&openIssues)
	var recent []model.DownloadLog
	db.DB.Order("created_at DESC").Limit(8).Find(&recent)
	taskData := TaskOverviewData{Scheduler: buildSchedulerTaskCard(), Scanner: buildScannerTaskCard(), Metadata: buildMetadataTaskCard(), Downloads: buildDownloadSyncTaskCard()}
	tasks := []TaskOverviewCard{taskData.Scheduler, taskData.Scanner, taskData.Metadata, taskData.Downloads}
	taskPayload := make([]gin.H, 0, len(tasks))
	for _, task := range tasks {
		taskPayload = append(taskPayload, gin.H{"title": task.Title, "status_label": task.StatusLabel, "status_tone": task.StatusTone, "summary": task.Summary, "detail": task.Detail, "progress_text": task.ProgressText, "display_error": task.DisplayError})
	}
	v1Data(c, http.StatusOK, gin.H{
		"active_subscriptions": active, "downloads": downloads, "library_items": libraryItems, "local_series": localSeries, "open_issues": openIssues,
		"services": gin.H{"bangumi": configValue(model.ConfigKeyBangumiAccessToken) != "", "tmdb": configValue(model.ConfigKeyTMDBToken) != "", "jellyfin": configValue(model.ConfigKeyJellyfinUrl) != ""},
		"tasks":    taskPayload, "recent_downloads": recent,
	})
}

func V1SyncHandler(c *gin.Context) {
	const taskID = "manual-sync"
	taskstate.Global.Start(taskID, "sync", "立即同步", "正在同步订阅、本地媒体和下载状态")
	go func() {
		if err := runDashboardSyncNow(context.Background()); err != nil {
			log.Printf("manual dashboard sync failed: %v", err)
			taskstate.Global.Fail(taskID, err)
			return
		}
		taskstate.Global.Complete(taskID, "同步完成")
	}()
	v1Message(c, http.StatusAccepted, "同步任务已经启动", gin.H{"task_id": taskID, "status": "running"})
}

func V1SubscriptionsHandler(c *gin.Context) {
	items, err := listSubscriptionsWithMetadata()
	if err != nil {
		v1Error(c, http.StatusInternalServerError, "subscriptions_unavailable", "无法读取订阅")
		return
	}
	populateSubscriptionStats(items)
	page, pageSize := v1Pagination(c, 100)
	total := len(items)
	start := min((page-1)*pageSize, total)
	end := min(start+pageSize, total)
	c.JSON(http.StatusOK, v1Envelope{Data: gin.H{"items": items[start:end], "scheduler": schedulerSnapshot(), "trend": loadSubscriptionTrendReport(7)}, Meta: map[string]any{"page": page, "page_size": pageSize, "total": total}})
}

func schedulerSnapshot() any {
	return buildSchedulerTaskCard()
}

func V1CreateSubscriptionHandler(c *gin.Context) {
	var sub model.Subscription
	if err := c.ShouldBindJSON(&sub); err != nil {
		v1Error(c, http.StatusBadRequest, "invalid_subscription", "订阅内容格式不正确")
		return
	}
	if strings.TrimSpace(sub.Title) == "" || strings.TrimSpace(sub.RSSUrl) == "" {
		v1Error(c, http.StatusBadRequest, "invalid_subscription", "番剧名称和 RSS 地址不能为空")
		return
	}
	if err := createSubscriptionInternal(&sub); err != nil {
		status := http.StatusInternalServerError
		if err.Error() == "exists" {
			status = http.StatusConflict
		}
		v1Error(c, status, "subscription_create_failed", err.Error())
		return
	}
	v1Message(c, http.StatusCreated, "订阅已添加", sub)
}

func V1SubscriptionActionHandler(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		sub, err := subscriptionByID(c.Param("id"))
		if err != nil {
			v1Error(c, http.StatusNotFound, "subscription_not_found", "未找到对应订阅")
			return
		}
		switch action {
		case "toggle":
			sub.IsActive = !sub.IsActive
			if err := saveSubscription(sub); err != nil {
				v1Error(c, http.StatusInternalServerError, "subscription_save_failed", err.Error())
				return
			}
		case "run":
			taskID := "subscription-" + c.Param("id")
			taskstate.Global.Start(taskID, "subscription", "订阅检查", "正在检查 "+sub.Title)
			go func(target *model.Subscription) {
				if err := runSubscriptionCheck(target, "manual"); err != nil {
					log.Printf("manual subscription run failed: %v", err)
					taskstate.Global.Fail(taskID, err)
					return
				}
				taskstate.Global.Complete(taskID, "订阅检查完成")
			}(sub)
			v1Message(c, http.StatusAccepted, "订阅检查已经启动", gin.H{"task_id": taskID, "status": "running"})
			return
		}
		updated, _ := loadSubscriptionCard(sub.ID)
		v1Message(c, http.StatusOK, "订阅状态已更新", updated)
	}
}

func V1DeleteSubscriptionHandler(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		v1Error(c, http.StatusBadRequest, "invalid_id", "订阅 ID 无效")
		return
	}
	var existing model.Subscription
	_ = db.DB.Unscoped().First(&existing, uint(id)).Error
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("subscription_id = ?", uint(id)).Delete(&model.DownloadLog{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Delete(&model.Subscription{}, uint(id)).Error
	})
	if err != nil {
		v1Error(c, http.StatusInternalServerError, "subscription_delete_failed", err.Error())
		return
	}
	service.RecordAudit(buildAuditContext(c), service.AuditEntry{Action: service.AuditActionSubscriptionDelete, Outcome: service.AuditOutcomeSuccess, TargetType: "subscription", TargetID: c.Param("id"), Details: map[string]string{"title": existing.Title}})
	v1Message(c, http.StatusOK, "订阅已删除", nil)
}

func V1SubscriptionHistoryHandler(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		v1Error(c, http.StatusBadRequest, "invalid_id", "订阅 ID 无效")
		return
	}
	data, err := loadSubscriptionHistory(uint(id))
	if err != nil {
		v1Error(c, http.StatusNotFound, "subscription_not_found", "未找到对应订阅")
		return
	}
	v1Data(c, http.StatusOK, data)
}

func V1CalendarHandler(c *gin.Context) {
	client := bangumi.NewClient("", "", "")
	applyProxyToBangumiClient(client)
	client.SetTimeout(calendarFetchTimeout)
	calendar, err := client.GetCalendar()
	if err != nil {
		v1Error(c, http.StatusBadGateway, "calendar_unavailable", "无法获取番剧日历："+humanizeOperationError(err.Error()))
		return
	}
	rememberCalendarPosterSources(calendar)
	v1Data(c, http.StatusOK, gin.H{"days": calendar, "today": calendarTodayTab(calendar, time.Now())})
}

func V1LibraryHandler(c *gin.Context) {
	var metadata []model.AnimeMetadata
	if err := db.DB.Order("updated_at DESC").Find(&metadata).Error; err != nil {
		v1Error(c, http.StatusInternalServerError, "library_unavailable", "无法读取番剧图鉴")
		return
	}
	var subs []model.Subscription
	var locals []model.LocalAnime
	db.DB.Select("metadata_id").Where("metadata_id IS NOT NULL").Find(&subs)
	db.DB.Select("id, metadata_id").Where("metadata_id IS NOT NULL").Find(&locals)
	subMap, localMap := map[uint]bool{}, map[uint]uint{}
	for _, item := range subs {
		if item.MetadataID != nil {
			subMap[*item.MetadataID] = true
		}
	}
	for _, item := range locals {
		if item.MetadataID != nil {
			localMap[*item.MetadataID] = item.ID
		}
	}
	items := make([]LibraryItem, 0, len(metadata))
	seenIDs, seenTitles := map[int]bool{}, map[string]bool{}
	for _, item := range metadata {
		if item.BangumiID > 0 && seenIDs[item.BangumiID] {
			continue
		}
		if seenTitles[item.Title] {
			continue
		}
		if item.BangumiID > 0 {
			seenIDs[item.BangumiID] = true
		}
		seenTitles[item.Title] = true
		items = append(items, LibraryItem{AnimeMetadata: item, IsSubscribed: subMap[item.ID], IsLocal: localMap[item.ID] > 0, LocalAnimeID: localMap[item.ID]})
	}
	page, pageSize := v1Pagination(c, 100)
	total := len(items)
	start := min((page-1)*pageSize, total)
	end := min(start+pageSize, total)
	v1Page(c, items[start:end], page, pageSize, int64(total))
}

func V1RefreshLibraryHandler(c *gin.Context) {
	force := c.Query("force") == ValueTrue
	if !service.NewMetadataService().StartRefreshAllMetadata(force) {
		v1Message(c, http.StatusAccepted, "元数据已经在刷新中", gin.H{"task_id": "metadata-refresh", "status": "running"})
		return
	}
	v1Message(c, http.StatusAccepted, "元数据刷新已经启动", gin.H{"task_id": "metadata-refresh", "status": "running"})
}

func V1LocalAnimeHandler(c *gin.Context) {
	var dirs []model.LocalAnimeDirectory
	var items []model.LocalAnime
	page, pageSize := v1Pagination(c, 100)
	var total int64
	db.DB.Order("id ASC").Find(&dirs)
	db.DB.Model(&model.LocalAnime{}).Count(&total)
	db.DB.Preload("Metadata").Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items)
	populateLocalAnimeActionHints(items)
	diagnostics, _ := service.ListOpenLibraryIssues(50)
	c.JSON(http.StatusOK, v1Envelope{Data: gin.H{"directories": dirs, "items": items, "scan_status": service.GlobalScanStatus.Snapshot(), "diagnostics": diagnostics}, Meta: map[string]any{"page": page, "page_size": pageSize, "total": total}})
}

func V1LocalAnimeEpisodesHandler(c *gin.Context) {
	laStore := localAnimeStore()
	if laStore == nil {
		v1Error(c, http.StatusServiceUnavailable, "database_unavailable", "数据库未初始化")
		return
	}
	anime, err := laStore.GetWithMetadata(c.Param("id"))
	if err != nil {
		v1Error(c, http.StatusNotFound, "anime_not_found", "未找到本地番剧")
		return
	}
	episodes, err := laStore.ListEpisodesByAnimeIDOrdered(anime.ID)
	if err != nil {
		v1Error(c, http.StatusInternalServerError, "episodes_unavailable", err.Error())
		return
	}
	jellyfinMap, jellyfinURL := fetchJellyfinProgress(anime)
	effectiveSource := ""
	if anime.Metadata != nil {
		effectiveSource = anime.Metadata.DataSource
	}
	bangumiCount, _ := fetchBangumiProgress(anime, effectiveSource)
	anilistCount, _ := fetchAniListProgress(anime, effectiveSource)
	display := buildEpisodeList(episodes, anime, jellyfinMap, jellyfinURL, bangumiCount, anilistCount)
	v1Data(c, http.StatusOK, gin.H{"anime": anime, "episodes": display, "collection_status": CollectionStatus{BangumiCollected: bangumiCount >= 0, AniListCollected: anilistCount >= 0, BangumiWatchedCount: max(0, bangumiCount), AniListWatchedCount: max(0, anilistCount)}})
}

func V1LocalScanHandler(c *gin.Context) {
	const taskID = "local-scan"
	taskstate.Global.Start(taskID, "scan", "本地扫描", "正在扫描本地媒体目录")
	go func() {
		scanner := service.NewScannerService()
		if err := scanner.ScanAll(); err != nil {
			log.Printf("local scan failed: %v", err)
			taskstate.Global.Fail(taskID, err)
			return
		}
		service.NewAgentService().RunAgentForLibrary()
		triggerJellyfinLibraryRefresh(context.Background())
		taskstate.Global.Complete(taskID, "本地扫描完成")
	}()
	v1Message(c, http.StatusAccepted, "本地扫描已经启动", gin.H{"task_id": taskID, "status": "running"})
}

func V1AddLocalDirectoryHandler(c *gin.Context) {
	var req struct {
		Path string `json:"path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Path) == "" {
		v1Error(c, http.StatusBadRequest, "invalid_path", "目录路径不能为空")
		return
	}
	if err := service.NewScannerService().AddDirectory(strings.TrimSpace(req.Path)); err != nil {
		v1Error(c, http.StatusBadRequest, "directory_add_failed", err.Error())
		return
	}
	const taskID = "local-scan"
	taskstate.Global.Start(taskID, "scan", "本地扫描", "目录已添加，正在扫描本地媒体")
	go func() {
		if err := service.NewScannerService().ScanAll(); err != nil {
			taskstate.Global.Fail(taskID, err)
			return
		}
		service.NewAgentService().RunAgentForLibrary()
		triggerJellyfinLibraryRefresh(context.Background())
		taskstate.Global.Complete(taskID, "目录已添加，本地扫描完成")
	}()
	v1Message(c, http.StatusAccepted, "目录已添加，扫描任务已经启动", gin.H{"task_id": taskID, "status": "running"})
}

func V1BackupHandler(c *gin.Context) {
	stats := getDBStats(db.DB, db.CurrentDBPath)
	configured := configValue(model.ConfigKeyR2Endpoint) != "" && configValue(model.ConfigKeyR2Bucket) != ""
	r2BackupCacheLock.RLock()
	files := append([]R2BackupFile(nil), r2BackupCache...)
	r2BackupCacheLock.RUnlock()
	v1Data(c, http.StatusOK, gin.H{
		"stats": gin.H{"subscription_count": stats.SubscriptionCount, "download_log_count": stats.DownloadLogCount, "local_anime_count": stats.LocalAnimeCount, "user_count": stats.UserCount, "global_config_count": stats.GlobalConfigCount, "database_size": stats.DatabaseSize, "last_modified": stats.LastModified},
		"r2":    gin.H{"configured": configured, "files": files},
	})
}

func V1AnalyzeBackupHandler(c *gin.Context) {
	file, err := c.FormFile("backup_file")
	if err != nil {
		v1Error(c, http.StatusBadRequest, "backup_file_missing", "请选择一个备份文件")
		return
	}
	tempFile, err := os.CreateTemp("", "restore_analyze_*.db")
	if err != nil {
		v1Error(c, http.StatusInternalServerError, "temp_file_failed", "无法创建临时文件")
		return
	}
	src, err := file.Open()
	if err != nil {
		safeio.Remove(tempFile.Name())
		v1Error(c, http.StatusBadRequest, "backup_open_failed", "无法打开备份文件")
		return
	}
	_, copyErr := io.Copy(tempFile, src)
	safeio.Close(src)
	safeio.Close(tempFile)
	if copyErr != nil || !isValidSQLite(tempFile.Name()) {
		safeio.Remove(tempFile.Name())
		v1Error(c, http.StatusBadRequest, "invalid_backup", "无效的数据库备份文件")
		return
	}
	stats, err := service.InspectBackup(tempFile.Name())
	if err != nil {
		safeio.Remove(tempFile.Name())
		v1Error(c, http.StatusBadRequest, "invalid_backup", "无法分析备份内容")
		return
	}
	token := registerRestoreArtifact(tempFile.Name())
	v1Data(c, http.StatusOK, gin.H{"stats": stats, "restore_token": token})
}

func V1RestoreBackupHandler(c *gin.Context) {
	var req struct {
		RestoreToken  string   `json:"restore_token"`
		Categories    []string `json:"categories"`
		RegenerateNFO bool     `json:"regenerate_nfo"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.RestoreToken == "" {
		v1Error(c, http.StatusBadRequest, "restore_token_missing", "没有可恢复的备份文件")
		return
	}
	tempPath, err := consumeRestoreArtifact(req.RestoreToken)
	if err != nil {
		v1Error(c, http.StatusBadRequest, "restore_token_invalid", err.Error())
		return
	}
	defer safeio.Remove(tempPath)
	selected := map[string]bool{}
	for _, item := range req.Categories {
		selected[item] = true
	}
	options := service.RestoreOptions{Configs: selected["configs"], Metadata: selected["metadata"], Subscriptions: selected["subscriptions"], Logs: selected["logs"], Local: selected["local"], Users: selected["users"], RegenerateNFO: req.RegenerateNFO}
	if err := service.NewRestoreService().PerformRestore(tempPath, options); err != nil {
		service.RecordAudit(buildAuditContext(c), service.AuditEntry{Action: service.AuditActionBackupRestore, Outcome: service.AuditOutcomeFailure, Details: map[string]any{"options": options, "error": err.Error()}})
		v1Error(c, http.StatusInternalServerError, "restore_failed", err.Error())
		return
	}
	service.RecordAudit(buildAuditContext(c), service.AuditEntry{Action: service.AuditActionBackupRestore, Outcome: service.AuditOutcomeSuccess, Details: map[string]any{"options": options}})
	if options.RegenerateNFO {
		go func() { _, _ = service.NewMetadataService().RegenerateAllNFOs() }()
	}
	v1Message(c, http.StatusOK, "备份恢复完成", nil)
}

func V1DeleteR2Handler(c *gin.Context) {
	var req struct {
		Key string `json:"key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Key == "" {
		v1Error(c, http.StatusBadRequest, "backup_key_missing", "没有提供备份标识")
		return
	}
	c.Request.Form = url.Values{"key": []string{req.Key}}
	c.Request.PostForm = c.Request.Form
	v1RunJSONHandler(c, DeleteR2BackupHandler)
}

func V1HealthHandler(c *gin.Context) { v1Data(c, http.StatusOK, buildHealthReport()) }

func V1RuntimeHandler(c *gin.Context) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	v1Data(c, http.StatusOK, gin.H{"uptime_seconds": int64(time.Since(runtimeStatsStartedAt).Seconds()), "go": gin.H{"goroutines": runtime.NumGoroutine(), "gomaxprocs": runtime.GOMAXPROCS(0), "num_cpu": runtime.NumCPU()}, "memory": gin.H{"heap_alloc_bytes": mem.HeapAlloc, "sys_bytes": mem.Sys}, "gc": gin.H{"num_gc": mem.NumGC}})
}

var v1SecretConfigKeys = map[string]bool{
	model.ConfigKeyQBPassword: true, model.ConfigKeyTMDBToken: true, model.ConfigKeyAniListToken: true, model.ConfigKeyBangumiAppSecret: true,
	model.ConfigKeyBangumiAccessToken: true, model.ConfigKeyBangumiRefreshToken: true,
	model.ConfigKeyJellyfinPassword: true, model.ConfigKeyJellyfinApiKey: true, model.ConfigKeyAListToken: true, model.ConfigKeyAIApiKey: true,
	model.ConfigKeyR2AccessKey: true, model.ConfigKeyR2SecretKey: true, model.ConfigKeyPikPakPassword: true, model.ConfigKeyPikPakRefreshToken: true,
}

func V1SettingsHandler(c *gin.Context) {
	values, _, stats := loadSettingsViewData()
	configured := map[string]bool{}
	for key := range v1SecretConfigKeys {
		configured[key] = strings.TrimSpace(values[key]) != ""
		values[key] = ""
	}
	v1Data(c, http.StatusOK, gin.H{"values": values, "configured": configured, "stats": stats})
}

func V1UpdateSettingsHandler(c *gin.Context) {
	var req struct {
		Values map[string]string `json:"values"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		v1Error(c, http.StatusBadRequest, "invalid_settings", "设置格式不正确")
		return
	}
	allowed := map[string]bool{}
	for _, key := range []string{model.ConfigKeyQBMode, model.ConfigKeyQBUrl, model.ConfigKeyQBUsername, model.ConfigKeyQBPassword, model.ConfigKeyBaseDir, model.ConfigKeyBangumiAppID, model.ConfigKeyBangumiAppSecret, model.ConfigKeyBangumiAccessToken, model.ConfigKeyBangumiRefreshToken, model.ConfigKeyTMDBToken, model.ConfigKeyAniListToken, model.ConfigKeyProxyURL, model.ConfigKeyProxyBangumi, model.ConfigKeyProxyMikan, model.ConfigKeyProxyTMDB, model.ConfigKeyProxyAniList, model.ConfigKeyProxyJellyfin, model.ConfigKeyProxyAI, model.ConfigKeyProxyUpdater, model.ConfigKeyJellyfinUrl, model.ConfigKeyJellyfinDirectUrl, model.ConfigKeyJellyfinUsername, model.ConfigKeyJellyfinPassword, model.ConfigKeyJellyfinApiKey, model.ConfigKeyAListUrl, model.ConfigKeyAListToken, model.ConfigKeyPikPakUsername, model.ConfigKeyPikPakPassword, model.ConfigKeyPikPakRefreshToken, model.ConfigKeyAIBaseURL, model.ConfigKeyAIModel, model.ConfigKeyAIApiKey, model.ConfigKeyR2Endpoint, model.ConfigKeyR2Bucket, model.ConfigKeyR2AccessKey, model.ConfigKeyR2SecretKey, model.ConfigKeyRepoUpdateEnabled, model.ConfigKeyRepoAutoPullEnabled, model.ConfigKeyRepoUpdateIntervalMinutes, model.ConfigKeyRepoUpdateOwner, model.ConfigKeyRepoUpdateName, model.ConfigKeyRepoRequireChecksum} {
		allowed[key] = true
	}
	updates := map[string]string{}
	for key, value := range req.Values {
		if !allowed[key] {
			continue
		}
		if v1SecretConfigKeys[key] && strings.TrimSpace(value) == "" {
			continue
		}
		value = strings.TrimSpace(value)
		if key == model.ConfigKeyProxyURL {
			normalized, err := httpx.NormalizeProxyURL(value)
			if err != nil {
				v1Error(c, http.StatusBadRequest, "invalid_proxy_url", err.Error())
				return
			}
			value = normalized
		}
		if key == model.ConfigKeyJellyfinDirectUrl {
			normalized, err := normalizeJellyfinBaseURL(value)
			if err != nil {
				v1Error(c, http.StatusBadRequest, "invalid_jellyfin_direct_url", err.Error())
				return
			}
			value = normalized
		}
		updates[key] = value
	}
	if err := store.NewConfigStore(db.DB).SetMany(updates); err != nil {
		v1Error(c, http.StatusInternalServerError, "settings_save_failed", err.Error())
		return
	}
	service.RecordAudit(buildAuditContext(c), service.AuditEntry{Action: service.AuditActionSettingsUpdate, Outcome: service.AuditOutcomeSuccess, Details: map[string]any{"keys": mapKeys(updates)}})
	statusCache = sync.Map{}
	v1Message(c, http.StatusOK, "设置已保存", nil)
}

func V1AssistantMessageHandler(c *gin.Context) {
	var req struct {
		Message string `json:"message"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Message) == "" {
		v1Error(c, http.StatusBadRequest, "message_required", "请输入消息")
		return
	}
	apiKey, baseURL, modelName := configValue(model.ConfigKeyAIApiKey), configValue(model.ConfigKeyAIBaseURL), configValue(model.ConfigKeyAIModel)
	if apiKey == "" {
		v1Error(c, http.StatusPreconditionFailed, "ai_not_configured", "请先在设置页配置 AI API Key")
		return
	}
	if modelName == "" {
		modelName = defaultAIModel
	}
	historyKey := aiChatHistoryKey(c)
	chatMutex.Lock()
	defer chatMutex.Unlock()
	history := truncateChatHistory(globalChatHistories[historyKey])
	if len(history) == 0 {
		history = append(history, ai.ChatMessage{Role: "system", Content: aiSystemPrompt})
	}
	history = append(history, ai.ChatMessage{Role: "user", Content: strings.TrimSpace(req.Message)})
	client := ai.NewClientWithProxy(baseURL, apiKey, modelName, configuredProxyURL(model.ConfigKeyProxyAI))
	answer := ""
	for attempts := 0; attempts < 6; attempts++ {
		resp, err := client.CreateChatCompletion(c.Request.Context(), ai.ChatCompletionRequest{Model: modelName, Messages: history, Tools: GlobalAIRegistry.GetToolDefinitions()})
		if err != nil {
			v1Error(c, http.StatusBadGateway, "ai_request_failed", "调用大模型失败，请检查连接设置")
			return
		}
		if len(resp.Choices) == 0 {
			v1Error(c, http.StatusBadGateway, "ai_empty_response", "大模型没有返回内容")
			return
		}
		choice := resp.Choices[0].Message
		history = append(history, choice)
		if len(choice.ToolCalls) == 0 {
			answer = strings.TrimSpace(choice.Content)
			break
		}
		for _, call := range choice.ToolCalls {
			result, err := GlobalAIRegistry.ExecuteTool(c.Request.Context(), call.Function.Name, call.Function.Arguments)
			if err != nil {
				result = err.Error()
			}
			history = append(history, ai.ChatMessage{Role: "tool", ToolCallID: call.ID, Name: call.Function.Name, Content: result})
		}
	}
	if answer == "" {
		answer = "执行完毕。"
	}
	globalChatHistories[historyKey] = history
	v1Data(c, http.StatusOK, gin.H{"message": answer})
}

func V1AssistantClearHandler(c *gin.Context) {
	key := aiChatHistoryKey(c)
	chatMutex.Lock()
	delete(globalChatHistories, key)
	chatMutex.Unlock()
	v1Message(c, http.StatusOK, "对话已清空", nil)
}
