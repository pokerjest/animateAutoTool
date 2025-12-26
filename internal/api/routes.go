package api

import (
	"encoding/json"
	"fmt"
	"html/template"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

func InitRoutes(r *gin.Engine) {
	// Perform startup cleanup
	svc := service.NewLocalAnimeService()
	svc.CleanupGarbage()
	svc.StartMetadataMigration() // Start background image caching

	// 注册模板函数
	r.SetFuncMap(template.FuncMap{
		"div": func(a, b float64) float64 {
			return a / b
		},
		"toGB": func(size int64) string {
			gb := float64(size) / 1024 / 1024 / 1024
			return fmt.Sprintf("%.2f GB", gb)
		},
		"json": func(v interface{}) template.JS {
			a, _ := json.Marshal(v)
			return template.JS(a)
		},
		"toJson": func(v interface{}) string {
			a, _ := json.Marshal(v)
			return string(a)
		},
	})

	// 加载模板，注意路径问题，在此我们假设运行在项目根目录
	// 匹配 web/templates 下的所有 html
	// 注意：嵌套 define 需要全部加载
	r.LoadHTMLGlob("web/templates/*.html")
	r.Static("/static", "web/static")

	r.GET("/", DashboardHandler)
	r.GET("/subscriptions", SubscriptionsHandler)
	r.GET("/settings", SettingsHandler)
	// Library
	r.GET("/library", GetLibraryHandler)
	r.POST("/library/refresh", RefreshLibraryMetadataHandler)
	r.GET("/local-anime", LocalAnimePageHandler)
	r.GET("/backup", BackupPageHandler)

	// API
	apiGroup := r.Group("/api")
	{
		apiGroup.POST("/sync", func(c *gin.Context) {
			// Trigger Sync (TODO: Implement actual sync logic if needed, currently just UI feedback)
			// User requested 1s delay for transition
			time.Sleep(1 * time.Second)
			c.JSON(200, gin.H{"status": "ok"})
		})

		// Subscriptions
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
		apiGroup.POST("/subscriptions/refresh", RefreshSubscriptionsHandler)

		// Settings
		apiGroup.POST("/settings", UpdateSettingsHandler) // Keep for backward compat if needed, or remove?
		apiGroup.POST("/settings/qb-save-test", QBSaveAndTestHandler)
		apiGroup.POST("/settings/bangumi-save", BangumiSaveHandler)
		apiGroup.GET("/settings/qb-status", GetQBStatusHandler)
		apiGroup.GET("/settings/tmdb-status", GetTMDBStatusHandler)
		apiGroup.GET("/settings/anilist-status", GetAniListStatusHandler)
		apiGroup.GET("/settings/jellyfin-status", GetJellyfinStatusHandler)
		apiGroup.POST("/settings/test-connection", TestConnectionHandler)

		// Local Anime
		apiGroup.POST("/local-directories", AddLocalDirectoryHandler)
		apiGroup.DELETE("/local-directories/:id", DeleteLocalDirectoryHandler)
		apiGroup.POST("/local-directories/scan", ScanLocalDirectoryHandler)
		apiGroup.GET("/local-anime/:id/files", GetLocalAnimeFilesHandler) // Keep for debugging if needed
		apiGroup.POST("/local-directories/:id/rename-preview", PreviewDirectoryRenameHandler)
		apiGroup.POST("/local-directories/:id/rename", ApplyDirectoryRenameHandler)
		apiGroup.POST("/local-directories/:id/refresh-metadata", RefreshLocalAnimeMetadataHandler)
		apiGroup.POST("/local-anime/:id/switch-source", SwitchLocalAnimeSourceHandler)

		// Backup
		apiGroup.GET("/backup/export", ExportBackupHandler)
		apiGroup.POST("/backup/import", ImportBackupHandler) // Keep for legacy or direct upload
		apiGroup.POST("/backup/analyze", AnalyzeBackupHandler)
		apiGroup.POST("/backup/execute", ExecuteRestoreHandler)

		// R2 Backup
		apiGroup.GET("/backup/r2/config", GetR2ConfigHandler)
		apiGroup.POST("/backup/r2/config", UpdateR2ConfigHandler)
		apiGroup.POST("/backup/r2/upload", UploadToR2Handler)
		apiGroup.GET("/backup/r2/list", ListR2BackupsHandler)

		apiGroup.POST("/backup/r2/stage", StageR2BackupHandler) // New Preview Endpoint
		apiGroup.POST("/backup/r2/restore", RestoreFromR2Handler)
		apiGroup.POST("/backup/r2/delete", DeleteR2BackupHandler)
		apiGroup.POST("/backup/r2/test", TestR2ConnectionHandler)
		apiGroup.GET("/backup/r2/progress/:taskId", GetR2ProgressHandler)

		// Bangumi Integration
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
	}
}
