package api

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/jellyfin"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

type LocalAnimeData struct {
	SkipLayout       bool
	Directories      []model.LocalAnimeDirectory
	AnimeList        []model.LocalAnime
	JellyfinURL      string
	JellyfinServerID string // Added Server ID
}

// LocalAnimePageHandler 渲染本地番剧管理页面
func LocalAnimePageHandler(c *gin.Context) {
	skip := isHTMX(c)

	var dirs []model.LocalAnimeDirectory
	db.DB.Find(&dirs)

	var animes []model.LocalAnime
	db.DB.Preload("Metadata").Find(&animes) // TODO: Pagination? For now fetch all

	var urlCfg, keyCfg model.GlobalConfig
	db.DB.Where("key = ?", model.ConfigKeyJellyfinUrl).First(&urlCfg)
	db.DB.Where("key = ?", model.ConfigKeyJellyfinApiKey).First(&keyCfg)

	serverId := ""
	if urlCfg.Value != "" && keyCfg.Value != "" {
		// Best effort fetch of Server ID
		// We could cache this, but fetching here ensures freshness if server changes
		// Or we can rely on cached status if we had one. Simple fetch is safe enough for page load.
		go func() {
			// Optional: Async check or sync?
			// Doing it sync for page load might be slow if JF is down.
			// Ideally we cache this in DB or memory on startup.
			// For now, let's just create a client and try quickly.
		}()

		// Let's try to fetch it quickly with short timeout or rely on stored config if we had it?
		// Better: We can store it in DB when we test connection?
		// For now, let's try to fetch it synchronously but with short timeout?
		// Actually, `jellyfin.NewClient` is cheap. `GetPublicInfo` does an HTTP request.
		// If Jellyfin is local, it's fast.
		// NOTE: To avoid blocking page load if Jellyfin is down, we should probably fetch this async or use a cached value.
		// However, for this specific "fix mismatch" user request, let's fetch it.
		// Optimization: We could reuse the client from elsewhere?
		client := jellyfin.NewClient(urlCfg.Value, keyCfg.Value)
		info, err := client.GetPublicInfo()
		if err == nil {
			serverId = info.Id
			log.Printf("DEBUG: Fetched Jellyfin Server ID for LocalAnime page: %s", serverId)
		} else {
			log.Printf("ERROR: Failed to fetch Jellyfin Server ID: %v", err)
		}
	}

	data := LocalAnimeData{
		SkipLayout:       skip,
		Directories:      dirs,
		AnimeList:        animes,
		JellyfinURL:      urlCfg.Value,
		JellyfinServerID: serverId,
	}

	c.HTML(http.StatusOK, "local_anime.html", data)
}

// AddLocalDirectoryHandler 添加新的目录
func AddLocalDirectoryHandler(c *gin.Context) {
	path := c.PostForm("path")
	if path == "" {
		c.String(http.StatusBadRequest, "路径不能为空")
		return
	}

	scannerSvc := service.NewScannerService()
	if err := scannerSvc.AddDirectory(path); err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("添加失败: %v", err))
		return
	}

	// Trigger immediate scan and Jellyfin sync
	go func() {
		// Sync to Jellyfin
		var urlCfg, keyCfg model.GlobalConfig
		db.DB.Where("key = ?", model.ConfigKeyJellyfinUrl).First(&urlCfg)
		db.DB.Where("key = ?", model.ConfigKeyJellyfinApiKey).First(&keyCfg)

		if urlCfg.Value != "" && keyCfg.Value != "" {
			client := jellyfin.NewClient(urlCfg.Value, keyCfg.Value)
			libName := filepath.Base(path)
			// Use "tvshows" as default for Anime
			if err := client.CreateLibrary(libName, path, "tvshows"); err != nil {
				log.Printf("Failed to auto-create Jellyfin library: %v", err)
			} else {
				log.Printf("Successfully created Jellyfin library: %s", libName)
			}
		}

		scanner := service.NewScannerService()
		if err := scanner.ScanAll(); err != nil {
			fmt.Printf("Error scanning all directories: %v\n", err)
		}
	}()

	time.Sleep(500 * time.Millisecond) // Wait a bit for UI
	c.Header("HX-Redirect", "/local-anime")
	c.Status(http.StatusOK)
}

// DeleteLocalDirectoryHandler 删除目录
func DeleteLocalDirectoryHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.String(http.StatusBadRequest, "Invalid ID")
		return
	}

	scannerSvc := service.NewScannerService()
	if err := scannerSvc.RemoveDirectory(uint(id)); err != nil {
		c.String(http.StatusInternalServerError, "删除失败")
		return
	}

	c.Status(http.StatusOK)
}

// ScanLocalDirectoryHandler 触发重新扫描
func ScanLocalDirectoryHandler(c *gin.Context) {
	scanner := service.NewScannerService()
	go func() {
		// Phase 1: Scanner (Events emitted via EventBus)
		if err := scanner.ScanAll(); err != nil {
			fmt.Printf("Error scanning all directories: %v\n", err)
			return
		}

		// Phase 2: Agent (Should also be triggered via EventBus in future, but explicit here for now)
		agent := service.NewAgentService()
		agent.RunAgentForLibrary()
	}()

	c.JSON(http.StatusOK, gin.H{"status": "started", "message": "扫描已在后台启动"})
}

// RegenerateNFOHandler 手动触发 NFO 重建
func RegenerateNFOHandler(c *gin.Context) {
	metaSvc := service.NewMetadataService()
	go func() {
		count, err := metaSvc.RegenerateAllNFOs()
		if err != nil {
			log.Printf("ERROR: NFO Regeneration failed: %v", err)
		} else {
			log.Printf("INFO: NFO Regeneration completed. Processed %d series.", count)
		}
	}()

	c.String(http.StatusOK, "NFO 重建任务已在后台启动，详情请查看日志")
}
