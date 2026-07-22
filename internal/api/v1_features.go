package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"github.com/pokerjest/animateAutoTool/internal/updater"
)

func V1BatchPreviewHandler(c *gin.Context)    { v1RunJSONHandler(c, BatchPreviewHandler) }
func V1BatchCreateHandler(c *gin.Context)     { v1RunJSONHandler(c, CreateBatchSubscriptionHandler) }
func V1ValidateRSSHandler(c *gin.Context)     { v1RunJSONHandler(c, ValidateSubscriptionRSSHandler) }
func V1MikanDashboardHandler(c *gin.Context)  { v1RunJSONHandler(c, GetMikanDashboardHandler) }
func V1MikanEpisodesHandler(c *gin.Context)   { v1RunJSONHandler(c, GetMikanEpisodesHandler) }
func V1MikanSubgroupsHandler(c *gin.Context)  { v1RunJSONHandler(c, GetSubgroupsHandler) }
func V1MetadataSearchHandler(c *gin.Context)  { v1RunJSONHandler(c, SearchMetadataHandler) }
func V1FixMatchHandler(c *gin.Context)        { v1RunJSONHandler(c, FixMatchHandler) }
func V1RenamePreviewHandler(c *gin.Context)   { v1RunJSONHandler(c, PreviewDirectoryRenameHandler) }
func V1RenameApplyHandler(c *gin.Context)     { v1RunJSONHandler(c, ApplyDirectoryRenameHandler) }
func V1LocalAnimeFilesHandler(c *gin.Context) { v1RunJSONHandler(c, GetLocalAnimeFilesHandler) }
func V1PickDirectoryHandler(c *gin.Context)   { v1RunJSONHandler(c, PickDirectoryHandler) }

func V1MikanSearchHandler(c *gin.Context) {
	keyword := strings.TrimSpace(c.Query("q"))
	if keyword == "" {
		v1Error(c, http.StatusBadRequest, "search_query_required", "请输入搜索关键词")
		return
	}
	results, err := parser.NewMikanParser().Search(keyword)
	if err != nil {
		v1Error(c, http.StatusBadGateway, "mikan_search_failed", humanizeOperationError(err.Error()))
		return
	}
	v1Data(c, http.StatusOK, gin.H{"items": results})
}

func V1RSSPreviewHandler(c *gin.Context) {
	rssURL := strings.TrimSpace(c.Query("rss"))
	if rssURL == "" {
		v1Error(c, http.StatusBadRequest, "rss_required", "请输入 RSS 地址")
		return
	}
	episodes, err := parser.NewMikanParser().ParseContext(c.Request.Context(), rssURL)
	if err != nil {
		v1Error(c, http.StatusBadGateway, "rss_preview_failed", humanizeOperationError(err.Error()))
		return
	}
	if len(episodes) > 20 {
		episodes = episodes[:20]
	}
	v1Data(c, http.StatusOK, gin.H{"items": episodes, "total": len(episodes)})
}

func V1UpdateSubscriptionHandler(c *gin.Context) {
	sub, err := subscriptionByID(c.Param("id"))
	if err != nil {
		v1Error(c, http.StatusNotFound, "subscription_not_found", "未找到对应订阅")
		return
	}
	var input struct {
		Title              string `json:"title"`
		RSSURL             string `json:"rss_url"`
		FilterRule         string `json:"filter_rule"`
		ExcludeRule        string `json:"exclude_rule"`
		BackupRSSURL       string `json:"backup_rss_url"`
		ExpectedEpisodes   int    `json:"expected_episodes"`
		AllowMultiSubgroup bool   `json:"allow_multi_subgroup"`
		AutoDisableOnDone  bool   `json:"auto_disable_on_done"`
		StaleAfterHours    int    `json:"stale_after_hours"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.RSSURL) == "" {
		v1Error(c, http.StatusBadRequest, "invalid_subscription", "番剧名称和 RSS 地址不能为空")
		return
	}
	sub.Title = strings.TrimSpace(input.Title)
	sub.RSSUrl = strings.TrimSpace(input.RSSURL)
	sub.FilterRule = strings.TrimSpace(input.FilterRule)
	sub.ExcludeRule = strings.TrimSpace(input.ExcludeRule)
	sub.BackupRSSUrl = strings.TrimSpace(input.BackupRSSURL)
	sub.ExpectedEpisodes = input.ExpectedEpisodes
	sub.AllowMultiSubgroup = input.AllowMultiSubgroup
	sub.AutoDisableOnDone = input.AutoDisableOnDone
	sub.StaleAfterHours = input.StaleAfterHours
	normalizeSubscriptionStrategy(sub)
	if err := saveSubscription(sub); err != nil {
		v1Error(c, http.StatusInternalServerError, "subscription_save_failed", err.Error())
		return
	}
	updated, _ := loadSubscriptionCard(sub.ID)
	v1Message(c, http.StatusOK, "订阅已保存", updated)
}

func V1SubscriptionRepairHandler(c *gin.Context) {
	action := c.Param("action")
	sub, err := subscriptionByID(c.Param("id"))
	if err != nil {
		v1Error(c, http.StatusNotFound, "subscription_not_found", "未找到对应订阅")
		return
	}
	allowed := map[string]bool{"use-base-rss": true, "clear-filter": true, "reset-logs": true, "retry-missing": true, "recheck-stale": true, "retry-upgrade": true, "refresh-library": true}
	if !allowed[action] {
		v1Error(c, http.StatusBadRequest, "invalid_repair_action", "不支持的修复操作")
		return
	}
	taskID := fmt.Sprintf("subscription-%d-%s", sub.ID, action)
	go func(target *model.Subscription, requested string) {
		var runErr error
		switch requested {
		case "use-base-rss":
			baseRSS, ok := deriveBaseRSSURL(target.RSSUrl)
			if !ok {
				runErr = fmt.Errorf("当前订阅已经是主 RSS")
			} else {
				runErr = useBaseRSSAndRecheck(target, baseRSS)
			}
		case "clear-filter":
			runErr = clearFilterAndRecheck(target)
		case "reset-logs":
			runErr = resetStaleLogsAndRecheck(target, staleLogResetAge)
		case "refresh-library":
			triggerJellyfinLibraryRefresh(context.Background())
		default:
			target.LastRunSummary = "修复检查已启动"
			target.LastError = ""
			if saveErr := saveSubscription(target); saveErr != nil {
				runErr = saveErr
			} else {
				runErr = runSubscriptionCheck(target, "manual")
			}
		}
		if runErr != nil {
			log.Printf("subscription repair %s failed for %d: %v", requested, target.ID, runErr)
			target.LastError = runErr.Error()
			target.LastRunSummary = "修复操作未完成"
			_ = saveSubscription(target)
		}
	}(sub, action)
	v1Message(c, http.StatusAccepted, "修复任务已经启动", gin.H{"task_id": taskID, "status": "running"})
}

func V1RefreshSubscriptionMetadataHandler(c *gin.Context) {
	sub, err := subscriptionByID(c.Param("id"))
	if err != nil {
		v1Error(c, http.StatusNotFound, "subscription_not_found", "未找到对应订阅")
		return
	}
	go func(target *model.Subscription) {
		service.NewMetadataService().EnrichMetadata(target.Metadata, target.Title)
		if err := saveSubscription(target); err != nil {
			log.Printf("subscription metadata refresh failed for %d: %v", target.ID, err)
		}
	}(sub)
	v1Message(c, http.StatusAccepted, "元数据刷新已经启动", gin.H{"task_id": "subscription-metadata-" + c.Param("id"), "status": "running"})
}

func applyV1MetadataSource(metadata *model.AnimeMetadata, source string) error {
	if metadata == nil {
		return fmt.Errorf("当前条目还没有关联元数据")
	}
	switch source {
	case SourceTMDB:
		if metadata.TMDBID == 0 {
			return fmt.Errorf("当前条目没有 TMDB 匹配")
		}
		metadata.Title, metadata.Image, metadata.Summary = metadata.TMDBTitle, metadata.TMDBImage, metadata.TMDBSummary
	case SourceBangumi:
		if metadata.BangumiID == 0 {
			return fmt.Errorf("当前条目没有 Bangumi 匹配")
		}
		metadata.Title, metadata.Image, metadata.Summary = metadata.BangumiTitle, metadata.BangumiImage, metadata.BangumiSummary
	case SourceAniList:
		if metadata.AniListID == 0 {
			return fmt.Errorf("当前条目没有 AniList 匹配")
		}
		metadata.Title, metadata.Image, metadata.Summary = metadata.AniListTitle, metadata.AniListImage, metadata.AniListSummary
	default:
		return fmt.Errorf("不支持的数据源")
	}
	metadata.DataSource = source
	if err := db.DB.Save(metadata).Error; err != nil {
		return err
	}
	service.NewMetadataService().SyncMetadataToModels(metadata)
	return nil
}

func V1SubscriptionSourceHandler(c *gin.Context) {
	sub, err := subscriptionWithMetadataByID(c.Param("id"))
	if err != nil {
		v1Error(c, http.StatusNotFound, "subscription_not_found", "未找到对应订阅")
		return
	}
	if err := applyV1MetadataSource(sub.Metadata, strings.TrimSpace(c.Query("source"))); err != nil {
		v1Error(c, http.StatusBadRequest, "source_switch_failed", err.Error())
		return
	}
	v1Message(c, http.StatusOK, "显示数据源已切换", sub)
}

func V1RefreshMetadataItemHandler(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		v1Error(c, http.StatusBadRequest, "invalid_id", "元数据 ID 无效")
		return
	}
	go func() {
		if err := service.NewMetadataService().RefreshSingleMetadata(uint(id)); err != nil {
			log.Printf("metadata refresh failed for %d: %v", id, err)
		}
	}()
	v1Message(c, http.StatusAccepted, "元数据刷新已经启动", gin.H{"task_id": "metadata-" + c.Param("id"), "status": "running"})
}

func V1DeleteLocalDirectoryHandler(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		v1Error(c, http.StatusBadRequest, "invalid_id", "目录 ID 无效")
		return
	}
	auditCtx := buildAuditContext(c)
	if err := service.NewScannerService().RemoveDirectory(uint(id)); err != nil {
		service.RecordAudit(auditCtx, service.AuditEntry{Action: service.AuditActionLocalDirectoryDelete, Outcome: service.AuditOutcomeFailure, TargetType: "local_directory", TargetID: c.Param("id"), Details: map[string]string{"error": err.Error()}})
		v1Error(c, http.StatusInternalServerError, "directory_delete_failed", err.Error())
		return
	}
	service.RecordAudit(auditCtx, service.AuditEntry{Action: service.AuditActionLocalDirectoryDelete, Outcome: service.AuditOutcomeSuccess, TargetType: "local_directory", TargetID: c.Param("id")})
	v1Message(c, http.StatusOK, "媒体目录已移除；磁盘文件未被删除", nil)
}

func V1RefreshLocalMetadataHandler(c *gin.Context) {
	localStore := localAnimeStore()
	if localStore == nil {
		v1Error(c, http.StatusServiceUnavailable, "database_unavailable", "数据库未初始化")
		return
	}
	anime, err := localStore.GetWithMetadata(c.Param("id"))
	if err != nil {
		v1Error(c, http.StatusNotFound, "anime_not_found", "未找到本地番剧")
		return
	}
	go func(target *model.LocalAnime) {
		if err := service.NewMetadataService().EnrichAnime(target); err != nil {
			log.Printf("local metadata refresh failed for %d: %v", target.ID, err)
		}
		_ = db.DB.Save(target).Error
	}(anime)
	v1Message(c, http.StatusAccepted, "本地番剧元数据刷新已经启动", gin.H{"task_id": "local-metadata-" + c.Param("id"), "status": "running"})
}

func V1LocalAnimeSourceHandler(c *gin.Context) {
	localStore := localAnimeStore()
	if localStore == nil {
		v1Error(c, http.StatusServiceUnavailable, "database_unavailable", "数据库未初始化")
		return
	}
	anime, err := localStore.GetWithMetadata(c.Param("id"))
	if err != nil {
		v1Error(c, http.StatusNotFound, "anime_not_found", "未找到本地番剧")
		return
	}
	if err := applyV1MetadataSource(anime.Metadata, strings.TrimSpace(c.Query("source"))); err != nil {
		v1Error(c, http.StatusBadRequest, "source_switch_failed", err.Error())
		return
	}
	v1Message(c, http.StatusOK, "显示数据源已切换", anime)
}

func V1BangumiCollectionHandler(c *gin.Context) { v1RunJSONHandler(c, UpdateBangumiCollectionHandler) }
func V1BangumiProgressHandler(c *gin.Context)   { v1RunJSONHandler(c, UpdateBangumiProgressHandler) }

func V1AuditLogsHandler(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "25"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 25
	}
	if pageSize > 100 {
		pageSize = 100
	}
	query := store.AuditLogQuery{Limit: pageSize, Offset: (page - 1) * pageSize, Action: c.Query("action"), Username: c.Query("username"), Outcome: c.Query("outcome")}
	auditStore := store.NewAuditLogStore(db.DB)
	rows, err := auditStore.ListRecent(query)
	if err != nil {
		v1Error(c, http.StatusInternalServerError, "audit_logs_unavailable", "无法读取审计日志")
		return
	}
	total, err := auditStore.Count(query)
	if err != nil {
		v1Error(c, http.StatusInternalServerError, "audit_logs_unavailable", "无法统计审计日志")
		return
	}
	c.JSON(http.StatusOK, v1Envelope{Data: gin.H{"items": rows}, Meta: map[string]any{"page": page, "page_size": pageSize, "total": total}})
}

func V1ConnectionStatusHandler(c *gin.Context) {
	provider := c.Param("provider")
	connected, detail, account := false, "", ""
	switch provider {
	case "qb":
		status := evaluateQBStatus(qbutil.LoadConfig())
		connected = status.Kind == qbStatusConnected
		detail = status.Message
	case "tmdb":
		connected, detail = CheckTMDBConnection()
	case "anilist":
		connected, account, detail = CheckAniListConnection()
	case "jellyfin":
		connected, detail = CheckJellyfinConnection()
	case "bangumi":
		token := configValue(model.ConfigKeyBangumiAccessToken)
		if token == "" {
			detail = "尚未配置 Access Token"
			break
		}
		client := bangumi.NewClient("", "", "")
		applyProxyToBangumiClient(client)
		user, err := client.GetCurrentUser(token)
		if err != nil {
			detail = humanizeOperationError(err.Error())
		} else {
			connected, account = true, user.Username
		}
	default:
		v1Error(c, http.StatusBadRequest, "unknown_provider", "不支持的连接类型")
		return
	}
	v1Data(c, http.StatusOK, gin.H{"provider": provider, "connected": connected, "detail": detail, "account": account, "checked_at": time.Now()})
}

func V1MaintenanceHandler(c *gin.Context) {
	v1Data(c, http.StatusOK, gin.H{"deployment": buildDeploymentCheckReport(), "updater": updater.Snapshot()})
}

func V1UpdaterActionHandler(c *gin.Context) {
	action := c.Param("action")
	var status any
	switch action {
	case "check":
		status = updater.TriggerCheckNow("api-v1")
	case "apply":
		status = updater.TriggerCheckAndPullNow("api-v1")
	default:
		v1Error(c, http.StatusBadRequest, "invalid_update_action", "不支持的更新操作")
		return
	}
	v1Message(c, http.StatusAccepted, "更新任务已经启动", gin.H{"task_id": "repo-update-" + action, "status": "running", "snapshot": status})
}

func V1R2ListHandler(c *gin.Context) { v1RunJSONHandler(c, ListR2BackupsHandler) }
func V1R2UploadHandler(c *gin.Context) {
	taskID, err := startR2UploadTask(c.Request.Context())
	if err != nil {
		v1Error(c, http.StatusBadRequest, "r2_not_configured", "R2 配置有误: "+err.Error())
		return
	}
	v1Message(c, http.StatusAccepted, "云备份上传任务已经启动", gin.H{"task_id": taskID, "status": "running"})
}
func V1R2ProgressHandler(c *gin.Context) { v1RunJSONHandler(c, GetR2ProgressHandler) }
func V1R2TestHandler(c *gin.Context)     { v1RunJSONHandler(c, TestR2ConnectionHandler) }

func V1R2StageHandler(c *gin.Context) {
	var req struct {
		Key string `json:"key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Key) == "" {
		v1Error(c, http.StatusBadRequest, "backup_key_missing", "没有提供备份标识")
		return
	}
	c.Request.Form = url.Values{"key": []string{strings.TrimSpace(req.Key)}}
	c.Request.PostForm = c.Request.Form
	v1RunJSONHandler(c, StageR2BackupHandler)
}

func V1AIStatusHandler(c *gin.Context) { v1RunJSONHandler(c, GetAIStatusHandler) }
func V1AIModelsHandler(c *gin.Context) { v1RunJSONHandler(c, GetAIModelsHandler) }
