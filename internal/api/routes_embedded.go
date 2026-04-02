package api

import (
	"fmt"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/pokerjest/animateAutoTool/internal/worker"
	webassets "github.com/pokerjest/animateAutoTool/web"
)

func InitRoutes(r *gin.Engine) {
	scannerSvc := service.NewScannerService()
	scannerSvc.CleanupGarbage()

	metaSvc := service.NewMetadataService()
	metaSvc.StartMetadataMigration()

	worker.StartMetadataWorker()

	authSvc := service.NewAuthService()
	authSvc.EnsureDefaultUser()

	store := cookie.NewStore([]byte(config.AppConfig.Auth.SecretKey))
	store.Options(sessionCookieOptions(nil, 0))
	r.Use(sessions.Sessions("animate_session", store))

	tmpl, err := webassets.ParseTemplates(templateFuncMap())
	if err != nil {
		panic(fmt.Errorf("failed to load embedded templates: %w", err))
	}
	r.SetHTMLTemplate(tmpl)

	staticFS, err := webassets.StaticFS()
	if err != nil {
		panic(fmt.Errorf("failed to load embedded static assets: %w", err))
	}
	r.StaticFS("/static", staticFS)

	r.GET("/login", LoginPageHandler)
	r.POST("/api/login", LoginPostHandler)
	r.GET("/logout", LogoutHandler)

	r.GET("/api/tmdb/image", ProxyTMDBImageHandler)

	authorized := r.Group("/")
	authorized.Use(AuthMiddleware())
	{
		authorized.POST("/api/change-password", ChangePasswordHandler)
		authorized.GET("/", DashboardHandler)
		authorized.GET("/setup", SetupPageHandler)
		authorized.GET("/subscriptions", SubscriptionsHandler)
		authorized.GET("/settings", SettingsHandler)
		authorized.GET("/library", GetLibraryHandler)
		authorized.POST("/library/refresh", RefreshLibraryMetadataHandler)
		authorized.GET("/local-anime", LocalAnimePageHandler)
		authorized.GET("/calendar", GetCalendarHandler)
		authorized.GET("/backup", BackupPageHandler)
		authorized.GET("/player", GetPlayerHandler)
		authorized.GET("/api/events", SSEHandler)

		apiGroup := authorized.Group("/api")
		{
			apiGroup.POST("/sync", func(c *gin.Context) {
				time.Sleep(1 * time.Second)
				c.JSON(200, gin.H{"status": "ok"})
			})

			apiGroup.GET("/setup/readiness", SetupReadinessHandler)
			apiGroup.POST("/setup/bootstrap", CompleteBootstrapSetupHandler)
			apiGroup.POST("/subscriptions", CreateSubscriptionHandler)
			apiGroup.POST("/subscriptions/batch", CreateBatchSubscriptionHandler)
			apiGroup.POST("/subscriptions/batch-preview", BatchPreviewHandler)
			apiGroup.POST("/subscriptions/:id/toggle", ToggleSubscriptionHandler)
			apiGroup.POST("/subscriptions/:id/run", RunSubscriptionHandler)
			apiGroup.POST("/subscriptions/:id/refresh-metadata", RefreshSubscriptionMetadataHandler)
			apiGroup.PUT("/subscriptions/:id", UpdateSubscriptionHandler)
			apiGroup.DELETE("/subscriptions/:id", DeleteSubscriptionHandler)
			apiGroup.POST("/subscriptions/:id/switch-source", SwitchSubscriptionSourceHandler)
			apiGroup.GET("/search", SearchAnimeHandler)
			apiGroup.GET("/search/subgroups", GetSubgroupsHandler)
			apiGroup.GET("/preview", PreviewRSSHandler)
			apiGroup.GET("/mikan/dashboard", GetMikanDashboardHandler)
			apiGroup.GET("/mikan/episodes", GetMikanEpisodesHandler)

			apiGroup.POST("/play/magnet", PlayMagnetHandler)

			apiGroup.POST("/subscriptions/refresh", RefreshSubscriptionsHandler)

			apiGroup.POST("/settings", UpdateSettingsHandler)
			apiGroup.POST("/settings/qb-save-test", QBSaveAndTestHandler)
			apiGroup.POST("/settings/bangumi-save", BangumiSaveHandler)
			apiGroup.GET("/settings/qb-status", GetQBStatusHandler)
			apiGroup.GET("/settings/tmdb-status", GetTMDBStatusHandler)
			apiGroup.GET("/settings/anilist-status", GetAniListStatusHandler)
			apiGroup.GET("/settings/jellyfin-status", GetJellyfinStatusHandler)
			apiGroup.POST("/settings/jellyfin-login", JellyfinLoginHandler)
			apiGroup.POST("/settings/pikpak-sync", PikPakSyncHandler)
			apiGroup.GET("/settings/pikpak-status", GetPikPakStatusHandler)
			apiGroup.POST("/settings/test-connection", TestConnectionHandler)

			apiGroup.POST("/local-directories", AddLocalDirectoryHandler)
			apiGroup.DELETE("/local-directories/:id", DeleteLocalDirectoryHandler)
			apiGroup.POST("/local-directories/scan", ScanLocalDirectoryHandler)
			apiGroup.GET("/local-anime/:id/files", GetLocalAnimeFilesHandler)
			apiGroup.POST("/local-directories/:id/rename-preview", PreviewDirectoryRenameHandler)
			apiGroup.POST("/local-directories/:id/rename", ApplyDirectoryRenameHandler)
			apiGroup.GET("/local-anime/:id/refresh-metadata", RefreshLocalAnimeMetadataHandler)
			apiGroup.POST("/library/fix_match", FixMatchHandler)
			apiGroup.GET("/metadata/search", SearchMetadataHandler)
			apiGroup.POST("/local-anime/:id/switch-source", SwitchLocalAnimeSourceHandler)
			apiGroup.POST("/library/nfo/regenerate", RegenerateNFOHandler)

			apiGroup.GET("/jellyfin/stream/:id", ProxyVideoHandler)

			apiGroup.GET("/jellyfin/play/:id", GetPlayInfoHandler)
			apiGroup.POST("/jellyfin/progress", ReportProgressHandler)

			apiGroup.GET("/backup/export", ExportBackupHandler)
			apiGroup.POST("/backup/import", ImportBackupHandler)
			apiGroup.POST("/backup/analyze", AnalyzeBackupHandler)
			apiGroup.POST("/backup/execute", ExecuteRestoreHandler)

			apiGroup.GET("/backup/r2/config", GetR2ConfigHandler)
			apiGroup.POST("/backup/r2/config", UpdateR2ConfigHandler)
			apiGroup.POST("/backup/r2/upload", UploadToR2Handler)
			apiGroup.GET("/backup/r2/list", ListR2BackupsHandler)
			apiGroup.POST("/backup/r2/stage", StageR2BackupHandler)
			apiGroup.POST("/backup/r2/restore", RestoreFromR2Handler)
			apiGroup.POST("/backup/r2/delete", DeleteR2BackupHandler)
			apiGroup.POST("/backup/r2/test", TestR2ConnectionHandler)
			apiGroup.GET("/backup/r2/progress/:taskId", GetR2ProgressHandler)

			apiGroup.GET("/bangumi/login", BangumiLoginHandler)
			apiGroup.GET("/posters/:id", GetPosterHandler)
			apiGroup.GET("/bangumi/callback", BangumiCallbackHandler)
			apiGroup.GET("/bangumi/profile", BangumiProfileHandler)
			apiGroup.POST("/bangumi/logout", BangumiLogoutHandler)
			apiGroup.GET("/bangumi/subject/:id", GetBangumiSubjectHandler)
			apiGroup.POST("/bangumi/subject/:id/collection", UpdateBangumiCollectionHandler)
			apiGroup.POST("/bangumi/subject/:id/progress", UpdateBangumiProgressHandler)
			apiGroup.GET("/library/refresh/status", GetRefreshStatusHandler)
			apiGroup.POST("/library/metadata/:id/refresh", RefreshItemMetadataHandler)
			apiGroup.GET("/ui/background/random", GetRandomBackgroundHandler)

			apiGroup.GET("/dashboard/bangumi-data", DashboardBangumiDataHandler)
			apiGroup.GET("/dashboard/qb-status", DashboardQBStatusHandler)
		}
	}
}
