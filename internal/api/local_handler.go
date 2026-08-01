package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/runtimejournal"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

type LocalAnimeData struct {
	SkipLayout       bool
	Directories      []model.LocalAnimeDirectory
	AnimeList        []model.LocalAnime
	ScanStatus       service.ScanRunStatus
	Diagnostics      []model.LibraryIssue
	JellyfinURL      string
	JellyfinServerID string // Added Server ID
	HighlightAnimeID uint
	AutoOpenAnimeID  uint
	AutoFocusEpisode string
}

// LocalAnimePageHandler 渲染本地番剧管理页面
func LocalAnimePageHandler(c *gin.Context) {
	skip := IsHTMX(c)
	highlightID := uint(0)
	if raw := c.Query("highlight"); raw != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 32); err == nil {
			highlightID = uint(parsed)
		}
	}
	autoOpenID := uint(0)
	if c.Query("open") == "1" || c.Query("open") == ValueTrue {
		autoOpenID = highlightID
	}
	focusEpisode := c.Query("focus_episode")

	var dirs []model.LocalAnimeDirectory
	db.DB.Find(&dirs)

	var animes []model.LocalAnime
	page, pageSize := boundedPagination(c, 200, 1000)
	offset := paginationOffset(page, pageSize)
	db.DB.Preload("Metadata").Order("id desc").Limit(pageSize).Offset(offset).Find(&animes)
	populateLocalAnimeActionHints(animes)

	diagnostics, err := service.ListOpenLibraryIssues(12)
	if err != nil {
		log.Printf("ERROR: failed to load library diagnostics: %v", err)
	}

	jellyfinURL := configValue(model.ConfigKeyJellyfinUrl)
	jellyfinAPIKey := configValue(model.ConfigKeyJellyfinApiKey)

	serverId := ""
	if jellyfinURL != "" && jellyfinAPIKey != "" {

		// Let's try to fetch it quickly with short timeout or rely on stored config if we had it?
		// Better: We can store it in DB when we test connection?
		// For now, let's try to fetch it synchronously but with short timeout?
		// Actually, `jellyfin.NewClient` is cheap. `GetPublicInfo` does an HTTP request.
		// If Jellyfin is local, it's fast.
		// NOTE: To avoid blocking page load if Jellyfin is down, we should probably fetch this async or use a cached value.
		// However, for this specific "fix mismatch" user request, let's fetch it.
		// Optimization: We could reuse the client from elsewhere?
		client := newConfiguredJellyfinClient(jellyfinURL, jellyfinAPIKey)
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		info, err := client.GetPublicInfoContext(ctx)
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
		ScanStatus:       service.GlobalScanStatus.Snapshot(),
		Diagnostics:      diagnostics,
		JellyfinURL:      jellyfinURL,
		JellyfinServerID: serverId,
		HighlightAnimeID: highlightID,
		AutoOpenAnimeID:  autoOpenID,
		AutoFocusEpisode: focusEpisode,
	}

	c.HTML(http.StatusOK, "local_anime.html", data)
}

func LocalAnimeScanStatusHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "local_scan_status.html", service.GlobalScanStatus.Snapshot())
}

func GetLocalAnimeCardHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		htmlBadRequest(c, "本地番剧 ID 无效")
		return
	}

	var anime model.LocalAnime
	if err := db.DB.Preload("Metadata").First(&anime, uint(id)).Error; err != nil {
		htmlNotFound(c, "未找到本地番剧")
		return
	}
	populateLocalAnimeActionHint(&anime)

	c.HTML(http.StatusOK, "local_anime_card.html", anime)
}

func LocalAnimeDiagnosticsHandler(c *gin.Context) {
	diagnostics, err := service.ListOpenLibraryIssues(12)
	if err != nil {
		c.String(http.StatusInternalServerError, "加载诊断失败")
		return
	}

	c.HTML(http.StatusOK, "local_anime_diagnostics.html", diagnostics)
}

func populateLocalAnimeActionHints(animes []model.LocalAnime) {
	for i := range animes {
		populateLocalAnimeActionHint(&animes[i])
	}
}

func populateLocalAnimeActionHint(anime *model.LocalAnime) {
	if anime == nil {
		return
	}

	anime.HasRepairActions = false
	anime.CanRetryScrape = false
	anime.CanFixMatch = false
	anime.RepairHint = ""

	var issue model.LibraryIssue
	issueFound := false
	if anime.ID != 0 && db.DB != nil {
		err := db.DB.
			Where("local_anime_id = ? AND issue_type = ? AND status = ?", anime.ID, service.LibraryIssueTypeScrape, service.LibraryIssueStatusOpen).
			Order("updated_at DESC").
			First(&issue).Error
		issueFound = err == nil
	}

	if issueFound {
		anime.HasRepairActions = true
		anime.CanRetryScrape = true
		anime.CanFixMatch = true
		if issue.Hint != "" {
			anime.RepairHint = issue.Hint
		} else {
			anime.RepairHint = "元数据抓取失败，可先重试；如果仍不准确，再手动修正匹配。"
		}
		return
	}

	if anime.Metadata == nil || anime.Image == "" || anime.Summary == "" {
		anime.HasRepairActions = true
		anime.CanRetryScrape = true
		anime.CanFixMatch = true
		anime.RepairHint = "当前本地番剧的元数据不完整，可先刷新，再视情况手动修正匹配。"
	}
}

// AddLocalDirectoryHandler 添加新的目录
func AddLocalDirectoryHandler(c *gin.Context) {
	path := c.PostForm("path")
	if path == "" {
		htmlBadRequest(c, "路径不能为空")
		return
	}

	scannerSvc := service.NewScannerService()
	if err := scannerSvc.AddDirectory(path); err != nil {
		htmlServerError(c, "添加目录", err)
		return
	}

	// Trigger immediate scan and Jellyfin sync
	GoBackground(func(ctx context.Context) {
		jellyfinURL := configValue(model.ConfigKeyJellyfinUrl)
		jellyfinAPIKey := configValue(model.ConfigKeyJellyfinApiKey)

		if jellyfinURL != "" && jellyfinAPIKey != "" {
			client := newConfiguredJellyfinClient(jellyfinURL, jellyfinAPIKey)
			libName := filepath.Base(path)
			if err := client.CreateLibrary(libName, path, "tvshows"); err != nil {
				log.Printf("Failed to auto-create Jellyfin library: %v", err)
			} else {
				log.Printf("Successfully created Jellyfin library: %s", libName)
			}
		}

		scanner := service.NewScannerService()
		if err := scanner.ScanAllWithProgressContext(ctx, nil); err != nil {
			fmt.Printf("Error scanning all directories: %v\n", err)
			return
		}
		triggerJellyfinLibraryRefresh(ctx)
	})

	c.Header("HX-Redirect", "/local-anime")
	c.Status(http.StatusOK)
}

// DeleteLocalDirectoryHandler 删除目录
func DeleteLocalDirectoryHandler(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		htmlBadRequest(c, "目录 ID 无效")
		return
	}

	auditCtx := buildAuditContext(c)
	scannerSvc := service.NewScannerService()
	if err := scannerSvc.RemoveDirectory(uint(id)); err != nil {
		service.RecordAudit(auditCtx, service.AuditEntry{
			Action:     service.AuditActionLocalDirectoryDelete,
			Outcome:    service.AuditOutcomeFailure,
			TargetType: "local_directory",
			TargetID:   idStr,
			Details:    map[string]string{"error": err.Error()},
		})
		htmlServerError(c, "删除目录", err)
		return
	}

	service.RecordAudit(auditCtx, service.AuditEntry{
		Action:     service.AuditActionLocalDirectoryDelete,
		Outcome:    service.AuditOutcomeSuccess,
		TargetType: "local_directory",
		TargetID:   idStr,
	})
	c.Status(http.StatusOK)
}

// ScanLocalDirectoryHandler 触发重新扫描
func ScanLocalDirectoryHandler(c *gin.Context) {
	if runtimejournal.RecoveryBlocked() {
		htmlServerError(c, "本地扫描", runtimejournal.ErrRecoveryBlocked)
		return
	}
	if runtimejournal.RecoveryInProgress() {
		htmlServerError(c, "本地扫描", runtimejournal.ErrRecoveryInProgress)
		return
	}
	scanner := service.NewScannerService()
	GoBackground(func(ctx context.Context) {
		if err := scanner.ScanAllWithProgressContext(ctx, nil); err != nil {
			fmt.Printf("Error scanning all directories: %v\n", err)
			return
		}

		agent := service.NewAgentService()
		agent.RunAgentForLibrary()
		triggerJellyfinLibraryRefresh(ctx)
	})

	c.JSON(http.StatusOK, gin.H{"status": "started", "message": "扫描已在后台启动"})
}

func triggerJellyfinLibraryRefresh(ctx context.Context) {
	if err := service.RequestJellyfinLibraryRefresh(ctx); err != nil && !errors.Is(err, service.ErrJellyfinNotConfigured) {
		log.Printf("Jellyfin library refresh failed: %v", err)
	}
}

// RegenerateNFOHandler 手动触发 NFO 重建
func RegenerateNFOHandler(c *gin.Context) {
	metaSvc := service.NewMetadataService()
	GoBackground(func(ctx context.Context) {
		count, err := metaSvc.RegenerateAllNFOsContext(ctx)
		if err != nil {
			log.Printf("ERROR: NFO Regeneration failed: %v", err)
		} else {
			log.Printf("INFO: NFO Regeneration completed. Processed %d series.", count)
		}
	})

	c.String(http.StatusOK, "NFO 重建任务已在后台启动，详情请查看日志")
}
