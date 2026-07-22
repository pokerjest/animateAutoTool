package api

import (
	"bytes"
	"html/template"

	"github.com/gin-gonic/gin"
)

func renderTemplateToString(name string, data interface{}) (string, error) {
	tmpl, err := template.New("").Funcs(templateFuncMap()).ParseGlob("testdata/templates/*.html")
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func initLegacyTestRoutes(r *gin.Engine) {
	tmpl, err := template.New("").Funcs(templateFuncMap()).ParseGlob("testdata/templates/*.html")
	if err != nil {
		panic(err)
	}
	r.SetHTMLTemplate(tmpl)
	initLegacyAPICompat(r)
}

// initLegacyAPICompat exists only in the test binary so historical handler
// tests can keep exercising shared service behavior. Production registers
// only /api/v1 and the Vue application routes.
func initLegacyAPICompat(r *gin.Engine) {
	deprecated := func(c *gin.Context) {
		c.Header("Deprecation", "true")
		c.Header("Link", `</api/v1>; rel="successor-version"`)
		c.Next()
	}
	r.POST("/api/login", deprecated, LoginPostHandler)
	r.GET("/logout", deprecated, LogoutHandler)
	recovery := r.Group("/")
	recovery.Use(deprecated, DirectLocalOnlyMiddleware(), SameOriginMiddleware())
	recovery.POST("/api/recovery/reset-admin", LocalResetAdminPasswordHandler)

	authorized := r.Group("/")
	authorized.Use(deprecated, AuthMiddleware(), SameOriginMiddleware())
	{
		authorized.POST("/api/change-password", ChangePasswordHandler)
		authorized.GET("/api/events", SSEHandler)
		api := authorized.Group("/api")
		{
			api.POST("/sync", DashboardSyncHandler)
			api.GET("/setup/readiness", SetupReadinessHandler)
			api.POST("/setup/bootstrap", CompleteBootstrapSetupHandler)
			api.POST("/subscriptions", CreateSubscriptionHandler)
			api.POST("/subscriptions/batch", CreateBatchSubscriptionHandler)
			api.POST("/subscriptions/batch-preview", BatchPreviewHandler)
			api.GET("/subscriptions/validate-rss", ValidateSubscriptionRSSHandler)
			api.POST("/subscriptions/:id/toggle", ToggleSubscriptionHandler)
			api.POST("/subscriptions/:id/run", RunSubscriptionHandler)
			api.POST("/subscriptions/:id/use-base-rss", UseBaseRSSHandler)
			api.POST("/subscriptions/:id/clear-filter", ClearSubscriptionFilterHandler)
			api.POST("/subscriptions/:id/reset-logs", ResetSubscriptionLogsHandler)
			api.POST("/subscriptions/:id/retry-missing", RetryMissingEpisodesHandler)
			api.POST("/subscriptions/:id/recheck-stale", RecheckStaleSubscriptionHandler)
			api.POST("/subscriptions/:id/retry-upgrade", RetryUpgradeSubscriptionHandler)
			api.POST("/subscriptions/:id/refresh-library", RefreshSubscriptionLibraryHandler)
			api.GET("/subscriptions/:id/card", GetSubscriptionCardHandler)
			api.GET("/subscriptions/:id/history", GetSubscriptionHistoryHandler)
			api.GET("/subscriptions/trends", GetSubscriptionTrendsHandler)
			api.POST("/subscriptions/:id/refresh-metadata", RefreshSubscriptionMetadataHandler)
			api.PUT("/subscriptions/:id", UpdateSubscriptionHandler)
			api.DELETE("/subscriptions/:id", DeleteSubscriptionHandler)
			api.POST("/subscriptions/:id/switch-source", SwitchSubscriptionSourceHandler)
			api.GET("/search", SearchAnimeHandler)
			api.GET("/search/subgroups", GetSubgroupsHandler)
			api.GET("/preview", PreviewRSSHandler)
			api.GET("/mikan/dashboard", GetMikanDashboardHandler)
			api.GET("/mikan/episodes", GetMikanEpisodesHandler)
			api.POST("/play/magnet", PlayMagnetHandler)
			api.POST("/subscriptions/refresh", RefreshSubscriptionsHandler)
			api.GET("/subscriptions/scheduler-status", GetSchedulerStatusHandler)
			api.POST("/settings", UpdateSettingsHandler)
			api.GET("/settings/deployment-check", GetDeploymentCheckHandler)
			api.GET("/settings/repo-update-status", GetRepoUpdateStatusHandler)
			api.POST("/settings/repo-update-check", RepoUpdateCheckNowHandler)
			api.POST("/settings/repo-update-pull", RepoUpdatePullNowHandler)
			api.POST("/settings/qb-save-test", QBSaveAndTestHandler)
			api.POST("/settings/bangumi-save", BangumiSaveHandler)
			api.GET("/settings/qb-status", GetQBStatusHandler)
			api.GET("/settings/tmdb-status", GetTMDBStatusHandler)
			api.GET("/settings/anilist-status", GetAniListStatusHandler)
			api.GET("/settings/jellyfin-status", GetJellyfinStatusHandler)
			api.POST("/settings/jellyfin-login", JellyfinLoginHandler)
			api.POST("/settings/pikpak-sync", PikPakSyncHandler)
			api.GET("/settings/pikpak-status", GetPikPakStatusHandler)
			api.POST("/settings/test-connection", TestConnectionHandler)
			api.POST("/system/pick-directory", PickDirectoryHandler)
			api.POST("/local-directories", AddLocalDirectoryHandler)
			api.DELETE("/local-directories/:id", DeleteLocalDirectoryHandler)
			api.POST("/local-directories/scan", ScanLocalDirectoryHandler)
			api.GET("/local-anime/scan-status", LocalAnimeScanStatusHandler)
			api.GET("/local-anime/diagnostics", LocalAnimeDiagnosticsHandler)
			api.GET("/local-anime/:id/card", GetLocalAnimeCardHandler)
			api.GET("/local-anime/:id/files", GetLocalAnimeFilesHandler)
			api.POST("/local-directories/:id/rename-preview", PreviewDirectoryRenameHandler)
			api.POST("/local-directories/:id/rename", ApplyDirectoryRenameHandler)
			api.POST("/local-anime/:id/refresh-metadata", RefreshLocalAnimeMetadataHandler)
			api.POST("/library/fix_match", FixMatchHandler)
			api.GET("/metadata/search", SearchMetadataHandler)
			api.POST("/local-anime/:id/switch-source", SwitchLocalAnimeSourceHandler)
			api.POST("/library/nfo/regenerate", RegenerateNFOHandler)
			api.GET("/jellyfin/stream/:id", ProxyVideoHandler)
			api.GET("/jellyfin/play/:id", GetPlayInfoHandler)
			api.POST("/jellyfin/progress", ReportProgressHandler)
			api.GET("/backup/export", ExportBackupHandler)
			api.POST("/backup/import", ImportBackupHandler)
			api.POST("/backup/analyze", AnalyzeBackupHandler)
			api.POST("/backup/execute", ExecuteRestoreHandler)
			api.GET("/backup/r2/config", GetR2ConfigHandler)
			api.POST("/backup/r2/config", UpdateR2ConfigHandler)
			api.POST("/backup/r2/upload", UploadToR2Handler)
			api.GET("/backup/r2/list", ListR2BackupsHandler)
			api.POST("/backup/r2/stage", StageR2BackupHandler)
			api.POST("/backup/r2/restore", RestoreFromR2Handler)
			api.POST("/backup/r2/delete", DeleteR2BackupHandler)
			api.POST("/backup/r2/test", TestR2ConnectionHandler)
			api.GET("/backup/r2/progress/:taskId", GetR2ProgressHandler)
			api.GET("/bangumi/login", BangumiLoginHandler)
			api.GET("/posters/:id", GetPosterHandler)
			api.GET("/bangumi/callback", BangumiCallbackHandler)
			api.GET("/bangumi/profile", BangumiProfileHandler)
			api.POST("/bangumi/logout", BangumiLogoutHandler)
			api.GET("/bangumi/subject/:id", GetBangumiSubjectHandler)
			api.POST("/bangumi/subject/:id/collection", UpdateBangumiCollectionHandler)
			api.POST("/bangumi/subject/:id/progress", UpdateBangumiProgressHandler)
			api.GET("/library/refresh/status", GetRefreshStatusHandler)
			api.POST("/library/metadata/:id/refresh", RefreshItemMetadataHandler)
			api.GET("/ui/background/random", GetRandomBackgroundHandler)
			api.GET("/dashboard/bangumi-data", DashboardBangumiDataHandler)
			api.GET("/dashboard/qb-status", DashboardQBStatusHandler)
			api.GET("/dashboard/task-overview", DashboardTaskOverviewHandler)
			api.POST("/download-logs/repair", RepairDownloadLogsHandler)
			api.GET("/health/report", HealthReportHandler)
			api.GET("/runtime/stats", RuntimeStatsHandler)
			api.GET("/audit-logs", ListAuditLogsHandler)
			api.POST("/ai/chat", AIChatHandler)
			api.POST("/ai/clear", AIClearHistoryHandler)
			api.GET("/ai/config", GetAIStatusHandler)
			api.POST("/ai/config", AIConfigHandler)
			api.GET("/ai/models", GetAIModelsHandler)
		}
	}
}
