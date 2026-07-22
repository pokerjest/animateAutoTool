package api

import (
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/config"
	webassets "github.com/pokerjest/animateAutoTool/web"
)

func InitRoutes(r *gin.Engine) {
	store := cookie.NewStore([]byte(config.AppConfig.Auth.SecretKey))
	store.Options(sessionCookieOptions(nil, 0))
	r.Use(sessions.Sessions("animate_session", store))
	r.Use(SecurityHeadersMiddleware())
	r.Use(BootstrapLocalOnlyMiddleware())

	staticFS, err := webassets.StaticFS()
	if err != nil {
		panic(err)
	}
	r.StaticFS("/static", staticFS)

	distAssets, err := webassets.DistAssetsFS()
	if err != nil {
		panic(err)
	}
	r.StaticFS("/assets", distAssets)

	// Image proxy is intentionally public so cached posters can render before
	// the SPA has completed session discovery.
	r.GET("/api/tmdb/image", ProxyTMDBImageHandler)
	initV1Routes(r)

	index, err := webassets.SPAIndex()
	if err != nil {
		panic(err)
	}
	serveSPA := func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "not_found", "message": "接口不存在"}})
			return
		}
		if c.Request.URL.Path == "/recover" && !requestIsDirectLoopback(c) {
			c.JSON(http.StatusForbidden, gin.H{"error": "此页面仅允许在本机通过 localhost 直接访问。"})
			return
		}
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	}
	spaOrLegacy := func(legacy gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			if gin.Mode() == gin.TestMode {
				legacy(c)
			} else {
				serveSPA(c)
			}
		}
	}

	r.GET("/login", spaOrLegacy(LoginPageHandler))
	recoveryPage := r.Group("")
	recoveryPage.Use(DirectLocalOnlyMiddleware())
	recoveryPage.GET("/recover", spaOrLegacy(RecoveryPageHandler))

	pages := r.Group("")
	pages.Use(AuthMiddleware())
	pages.GET("/", spaOrLegacy(DashboardHandler))
	pages.GET("/setup", spaOrLegacy(SetupPageHandler))
	pages.GET("/subscriptions", spaOrLegacy(SubscriptionsHandler))
	pages.GET("/settings", spaOrLegacy(SettingsHandler))
	pages.GET("/library", spaOrLegacy(GetLibraryHandler))
	pages.GET("/local-anime", spaOrLegacy(LocalAnimePageHandler))
	pages.GET("/calendar", spaOrLegacy(GetCalendarHandler))
	pages.GET("/backup", spaOrLegacy(BackupPageHandler))
	pages.GET("/player", spaOrLegacy(GetPlayerHandler))
	pages.GET("/health", spaOrLegacy(HealthPageHandler))
	pages.GET("/assistant", serveSPA)
	r.NoRoute(serveSPA)
}
