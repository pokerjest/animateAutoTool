package api

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
)

type DashboardData struct {
	SkipLayout        bool
	ActiveSubs        int64
	TodayDownloads    int64
	QBConnected       bool
	QBVersion         string
	BangumiLogin      bool
	TMDBConnected     bool
	JellyfinConnected bool
}

func DashboardHandler(c *gin.Context) {
	start := time.Now()
	log.Printf("DEBUG: DashboardHandler Started at %v", start)
	defer func() {
		log.Printf("DEBUG: DashboardHandler Finished in %v", time.Since(start))
	}()

	skip := IsHTMX(c)

	var activeSubs int64
	db.DB.Model(&model.Subscription{}).Where("is_active = ?", true).Count(&activeSubs)

	var totalDownloads int64
	db.DB.Model(&model.DownloadLog{}).Count(&totalDownloads)

	var qbConnected bool
	var qbVersion string

	var bangumiLogin bool
	var tokenConfig model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyBangumiAccessToken).First(&tokenConfig).Error; err == nil && tokenConfig.Value != "" {
		bangumiLogin = true
	}

	var tmdbConnected bool
	var tmdbConfig model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyTMDBToken).First(&tmdbConfig).Error; err == nil && tmdbConfig.Value != "" {
		tmdbConnected = true
	}

	var jellyfinConnected bool
	var jellyfinConfig model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyJellyfinUrl).First(&jellyfinConfig).Error; err == nil && jellyfinConfig.Value != "" {
		jellyfinConnected = true
	}

	data := DashboardData{
		SkipLayout:        skip,
		ActiveSubs:        activeSubs,
		TodayDownloads:    totalDownloads,
		QBConnected:       qbConnected,
		QBVersion:         qbVersion,
		BangumiLogin:      bangumiLogin,
		TMDBConnected:     tmdbConnected,
		JellyfinConnected: jellyfinConnected,
	}

	c.HTML(http.StatusOK, "index.html", data)
}

func DashboardBangumiDataHandler(c *gin.Context) {
	var watchingList []bangumi.UserCollectionItem

	var tokenConfig model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyBangumiAccessToken).First(&tokenConfig).Error; err == nil && tokenConfig.Value != "" {
		client := bangumi.NewClient("", "", "")
		user, err := client.GetCurrentUser(tokenConfig.Value)
		if err == nil {
			watching, err1 := client.GetUserCollection(tokenConfig.Value, user.Username, 3, 12, 0)
			if err1 != nil {
				log.Printf("Error fetching watching collection: %v", err1)
			} else {
				watchingList = watching
			}
		} else {
			log.Printf("Error fetching user profile: %v", err)
		}
	}

	c.HTML(http.StatusOK, "dashboard_bangumi.html", gin.H{
		"WatchingList": watchingList,
	})
}

func DashboardQBStatusHandler(c *gin.Context) {
	start := time.Now()
	log.Printf("DEBUG: DashboardQBStatusHandler Started")
	defer func() {
		log.Printf("DEBUG: DashboardQBStatusHandler Finished in %v", time.Since(start))
	}()

	qbCfg := qbutil.LoadConfig()
	if qbutil.ManagedBinaryMissing(qbCfg, config.BinDir()) {
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, `<div id="qb-status-dashboard" title="Managed qBittorrent binary not found and no external WebUI is configured" class="text-amber-600 font-bold flex items-center gap-1.5 bg-amber-50 px-2 py-0.5 rounded-full text-xs"><span class="w-1.5 h-1.5 rounded-full bg-amber-500"></span> Missing</div>`)
		return
	}
	if qbutil.MissingExternalURL(qbCfg) {
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, `<div id="qb-status-dashboard" title="External qBittorrent mode is enabled, but the WebUI URL is empty" class="text-amber-600 font-bold flex items-center gap-1.5 bg-amber-50 px-2 py-0.5 rounded-full text-xs"><span class="w-1.5 h-1.5 rounded-full bg-amber-500"></span> Config</div>`)
		return
	}

	var qbConnected bool
	var qbVersion string
	if qbCfg.URL != "" {
		qbt := downloader.NewQBittorrentClient(qbCfg.URL)
		if err := qbt.Login(qbCfg.Username, qbCfg.Password); err == nil {
			if ver, err := qbt.GetVersion(); err == nil {
				qbConnected = true
				qbVersion = ver
			}
		}
	}

	html := ""
	if qbConnected {
		html = fmt.Sprintf(`<span class="text-emerald-600 font-bold flex items-center gap-1.5 bg-emerald-50 px-2 py-0.5 rounded-full text-xs" title="%s"><span class="w-1.5 h-1.5 rounded-full bg-emerald-500"></span> Connected (%s)</span>`, qbVersion, qbVersion)
	} else {
		html = `<span class="text-red-500 font-bold flex items-center gap-1.5 bg-red-50 px-2 py-0.5 rounded-full text-xs"><span class="w-1.5 h-1.5 rounded-full bg-red-500"></span> Offline</span>`
	}
	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, html)
}
