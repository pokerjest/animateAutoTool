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
	service.NewLocalAnimeService().CleanupGarbage()

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
	})

	// 加载模板，注意路径问题，在此我们假设运行在项目根目录
	// 匹配 web/templates 下的所有 html
	// 注意：嵌套 define 需要全部加载
	r.LoadHTMLGlob("web/templates/*.html")
	r.Static("/static", "web/static")

	r.GET("/", DashboardHandler)
	r.GET("/subscriptions", SubscriptionsHandler)
	r.GET("/settings", SettingsHandler)
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
		apiGroup.PUT("/subscriptions/:id", UpdateSubscriptionHandler)
		apiGroup.DELETE("/subscriptions/:id", DeleteSubscriptionHandler)
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
		apiGroup.POST("/settings/test-connection", TestConnectionHandler)

		// Local Anime
		apiGroup.POST("/local-directories", AddLocalDirectoryHandler)
		apiGroup.DELETE("/local-directories/:id", DeleteLocalDirectoryHandler)
		apiGroup.POST("/local-directories/scan", ScanLocalDirectoryHandler)
		apiGroup.GET("/local-anime/:id/files", GetLocalAnimeFilesHandler) // Keep for debugging if needed
		apiGroup.POST("/local-directories/:id/rename-preview", PreviewDirectoryRenameHandler)
		apiGroup.POST("/local-directories/:id/rename", ApplyDirectoryRenameHandler)

		// Backup
		apiGroup.GET("/backup/export", ExportBackupHandler)
		apiGroup.POST("/backup/import", ImportBackupHandler) // Keep for legacy or direct upload
		apiGroup.POST("/backup/analyze", AnalyzeBackupHandler)
		apiGroup.POST("/backup/execute", ExecuteRestoreHandler)

		// Bangumi Integration
		apiGroup.GET("/bangumi/login", BangumiLoginHandler)
		apiGroup.GET("/bangumi/callback", BangumiCallbackHandler)
		apiGroup.GET("/bangumi/profile", BangumiProfileHandler)
		apiGroup.POST("/bangumi/logout", BangumiLogoutHandler)
		apiGroup.GET("/bangumi/subject/:id", GetBangumiSubjectHandler)
		apiGroup.POST("/bangumi/subject/:id/collection", UpdateBangumiCollectionHandler)
		apiGroup.POST("/bangumi/subject/:id/progress", UpdateBangumiProgressHandler)
	}
}
