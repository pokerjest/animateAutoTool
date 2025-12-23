package api

import (
	"github.com/gin-gonic/gin"
)

func InitRoutes(r *gin.Engine) {
	// 加载模板，注意路径问题，在此我们假设运行在项目根目录
	// 匹配 web/templates 下的所有 html
	// 注意：嵌套 define 需要全部加载
	r.LoadHTMLGlob("web/templates/*.html")
	r.Static("/static", "web/static")

	r.GET("/", DashboardHandler)
	r.GET("/subscriptions", SubscriptionsHandler)
	r.GET("/settings", SettingsHandler)

	// API
	apiGroup := r.Group("/api")
	{
		apiGroup.POST("/sync", func(c *gin.Context) {
			// TODO: Trigger Sync
			c.JSON(200, gin.H{"status": "ok"})
		})

		// Subscriptions
		apiGroup.POST("/subscriptions", CreateSubscriptionHandler)
		apiGroup.POST("/subscriptions/:id/toggle", ToggleSubscriptionHandler)
		apiGroup.POST("/subscriptions/:id/run", RunSubscriptionHandler)
		apiGroup.PUT("/subscriptions/:id", UpdateSubscriptionHandler)
		apiGroup.DELETE("/subscriptions/:id", DeleteSubscriptionHandler)
		apiGroup.GET("/search", SearchAnimeHandler)
		apiGroup.GET("/search/subgroups", GetSubgroupsHandler)
		apiGroup.GET("/preview", PreviewRSSHandler)

		// Settings
		apiGroup.POST("/settings", UpdateSettingsHandler)
		apiGroup.POST("/settings/test-connection", TestConnectionHandler)
	}
}
