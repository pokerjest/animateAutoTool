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
	"github.com/pokerjest/animateAutoTool/internal/taskstate"
	"github.com/pokerjest/animateAutoTool/internal/updater"
)

func V1BatchPreviewHandler(c *gin.Context)    { v1RunJSONHandler(c, BatchPreviewHandler) }
func V1BatchCreateHandler(c *gin.Context)     { v1RunJSONHandler(c, CreateBatchSubscriptionHandler) }
func V1ValidateRSSHandler(c *gin.Context)     { v1RunJSONHandler(c, ValidateSubscriptionRSSHandler) }
func V1MetadataSearchHandler(c *gin.Context)  { v1RunJSONHandler(c, SearchMetadataHandler) }
func V1FixMatchHandler(c *gin.Context)        { v1RunJSONHandler(c, FixMatchHandler) }
func V1RenamePreviewHandler(c *gin.Context)   { v1RunJSONHandler(c, PreviewDirectoryRenameHandler) }
func V1RenameApplyHandler(c *gin.Context)     { v1RunJSONHandler(c, ApplyDirectoryRenameHandler) }
func V1LocalAnimeFilesHandler(c *gin.Context) { v1RunJSONHandler(c, GetLocalAnimeFilesHandler) }
func V1PickDirectoryHandler(c *gin.Context)   { v1RunJSONHandler(c, PickDirectoryHandler) }

type v1MikanClient interface {
	ParseContext(context.Context, string) ([]parser.Episode, error)
	SearchContext(context.Context, string) ([]parser.SearchResult, error)
	GetSubgroupsContext(context.Context, string) ([]parser.Subgroup, error)
	GetDashboardContext(context.Context, string, string) (*parser.MikanDashboard, error)
}

type v1MikanDiscoveryItem struct {
	MikanID string `json:"mikan_id"`
	Title   string `json:"title"`
	Image   string `json:"image"`
}

type v1MikanSubgroup struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	IsAll bool   `json:"is_all"`
}

var newV1MikanClient = func() v1MikanClient {
	return newConfiguredMikanParser()
}

var enrichV1LocalAnime = func(anime *model.LocalAnime) error {
	return service.NewMetadataService().EnrichAnime(anime)
}

func mikanDiscoveryItems(items []parser.SearchResult) []v1MikanDiscoveryItem {
	result := make([]v1MikanDiscoveryItem, 0, len(items))
	for _, item := range items {
		result = append(result, v1MikanDiscoveryItem{
			MikanID: strings.TrimSpace(item.MikanID),
			Title:   strings.TrimSpace(item.Title),
			Image:   strings.TrimSpace(item.Image),
		})
	}
	return result
}

func firstQuery(c *gin.Context, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(c.Query(name)); value != "" {
			return value
		}
	}
	return ""
}

func validMikanNumericID(value string) bool {
	if value == "" {
		return false
	}
	_, err := strconv.ParseUint(value, 10, 64)
	return err == nil
}

func V1MikanSearchHandler(c *gin.Context) {
	keyword := strings.TrimSpace(c.Query("q"))
	if keyword == "" {
		v1Error(c, http.StatusBadRequest, "search_query_required", "请输入搜索关键词")
		return
	}
	results, err := newV1MikanClient().SearchContext(c.Request.Context(), keyword)
	if err != nil {
		v1Error(c, http.StatusBadGateway, "mikan_search_failed", humanizeOperationError(err.Error()))
		return
	}
	v1Data(c, http.StatusOK, gin.H{"items": mikanDiscoveryItems(results)})
}

func V1RSSPreviewHandler(c *gin.Context) {
	rssURL := strings.TrimSpace(c.Query("rss"))
	if rssURL == "" {
		v1Error(c, http.StatusBadRequest, "rss_required", "请输入 RSS 地址")
		return
	}
	episodes, err := newV1MikanClient().ParseContext(c.Request.Context(), rssURL)
	if err != nil {
		v1Error(c, http.StatusBadGateway, "rss_preview_failed", humanizeOperationError(err.Error()))
		return
	}
	if len(episodes) > 20 {
		episodes = episodes[:20]
	}
	v1Data(c, http.StatusOK, gin.H{"items": episodes, "total": len(episodes)})
}

func V1MikanDashboardHandler(c *gin.Context) {
	year := strings.TrimSpace(c.Query("year"))
	season := strings.TrimSpace(c.Query("season"))
	if (year == "") != (season == "") {
		v1Error(c, http.StatusBadRequest, "invalid_mikan_season", "年份和季度必须同时提供")
		return
	}
	if year != "" {
		if _, err := strconv.Atoi(year); err != nil {
			v1Error(c, http.StatusBadRequest, "invalid_mikan_year", "Mikan 年份无效")
			return
		}
		switch season {
		case "春", "夏", "秋", "冬":
		default:
			v1Error(c, http.StatusBadRequest, "invalid_mikan_season", "Mikan 季度无效")
			return
		}
	}

	dashboard, err := newV1MikanClient().GetDashboardContext(c.Request.Context(), year, season)
	if err != nil {
		v1Error(c, http.StatusBadGateway, "mikan_dashboard_failed", humanizeOperationError(err.Error()))
		return
	}
	days := make(map[string][]v1MikanDiscoveryItem, len(dashboard.Days))
	for day, items := range dashboard.Days {
		days[day] = mikanDiscoveryItems(items)
	}
	v1Data(c, http.StatusOK, gin.H{"season": strings.TrimSpace(dashboard.Season), "days": days})
}

func V1MikanSubgroupsHandler(c *gin.Context) {
	mikanID := firstQuery(c, "mikan_id", "id", "bangumiId")
	if !validMikanNumericID(mikanID) {
		v1Error(c, http.StatusBadRequest, "invalid_mikan_id", "Mikan 番剧 ID 无效")
		return
	}

	groups, err := newV1MikanClient().GetSubgroupsContext(c.Request.Context(), mikanID)
	if err != nil {
		v1Error(c, http.StatusBadGateway, "mikan_subgroups_failed", humanizeOperationError(err.Error()))
		return
	}
	items := make([]v1MikanSubgroup, 0, len(groups))
	for _, group := range groups {
		id := strings.TrimSpace(group.ID)
		name := strings.TrimSpace(group.Name)
		if id == "" {
			name = "全部字幕组"
		}
		items = append(items, v1MikanSubgroup{ID: id, Name: name, IsAll: id == ""})
	}
	v1Data(c, http.StatusOK, gin.H{"items": items})
}

func V1MikanEpisodesHandler(c *gin.Context) {
	mikanID := firstQuery(c, "mikan_id", "bangumiId", "id")
	if !validMikanNumericID(mikanID) {
		v1Error(c, http.StatusBadRequest, "invalid_mikan_id", "Mikan 番剧 ID 无效")
		return
	}
	subgroupID := firstQuery(c, "subgroup_id", "subgroupId")
	if subgroupID != "" && !validMikanNumericID(subgroupID) {
		v1Error(c, http.StatusBadRequest, "invalid_mikan_subgroup_id", "Mikan 字幕组 ID 无效")
		return
	}

	values := url.Values{"bangumiId": []string{mikanID}}
	if subgroupID != "" {
		values.Set("subgroupid", subgroupID)
	}
	rssURL := "https://mikanani.me/RSS/Bangumi?" + values.Encode()
	episodes, err := newV1MikanClient().ParseContext(c.Request.Context(), rssURL)
	if err != nil {
		v1Error(c, http.StatusBadGateway, "mikan_episodes_failed", humanizeOperationError(err.Error()))
		return
	}
	total := len(episodes)
	if total > 20 {
		episodes = episodes[:20]
	}
	v1Data(c, http.StatusOK, gin.H{"mikan_id": mikanID, "items": episodes, "total": total})
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
		MikanID            string `json:"mikan_id"`
		Image              string `json:"image"`
		SubtitleGroup      string `json:"subtitle_group"`
		Season             string `json:"season"`
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
	sub.MikanID = strings.TrimSpace(input.MikanID)
	sub.Image = strings.TrimSpace(input.Image)
	sub.SubtitleGroup = strings.TrimSpace(input.SubtitleGroup)
	sub.Season = strings.TrimSpace(input.Season)
	sub.FilterRule = strings.TrimSpace(input.FilterRule)
	sub.ExcludeRule = strings.TrimSpace(input.ExcludeRule)
	sub.BackupRSSUrl = strings.TrimSpace(input.BackupRSSURL)
	sub.ExpectedEpisodes = input.ExpectedEpisodes
	sub.AllowMultiSubgroup = input.AllowMultiSubgroup
	sub.AutoDisableOnDone = input.AutoDisableOnDone
	sub.StaleAfterHours = input.StaleAfterHours
	normalizeMikanAssociation(sub)
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
	taskstate.Global.Start(taskID, "subscription-repair", "订阅修复", "正在修复 "+sub.Title)
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
			taskstate.Global.Fail(taskID, runErr)
			return
		}
		taskstate.Global.Complete(taskID, "订阅修复完成")
	}(sub, action)
	v1Message(c, http.StatusAccepted, "修复任务已经启动", gin.H{"task_id": taskID, "status": "running"})
}

func V1RefreshSubscriptionMetadataHandler(c *gin.Context) {
	sub, err := subscriptionByID(c.Param("id"))
	if err != nil {
		v1Error(c, http.StatusNotFound, "subscription_not_found", "未找到对应订阅")
		return
	}
	taskID := "subscription-metadata-" + c.Param("id")
	taskstate.Global.Start(taskID, "metadata", "刷新订阅元数据", "正在刷新 "+sub.Title)
	go func(target *model.Subscription) {
		service.NewMetadataService().EnrichMetadata(target.Metadata, target.Title)
		if err := saveSubscription(target); err != nil {
			log.Printf("subscription metadata refresh failed for %d: %v", target.ID, err)
			taskstate.Global.Fail(taskID, err)
			return
		}
		taskstate.Global.Complete(taskID, "订阅元数据刷新完成")
	}(sub)
	v1Message(c, http.StatusAccepted, "元数据刷新已经启动", gin.H{"task_id": taskID, "status": "running"})
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
	taskID := "metadata-" + c.Param("id")
	taskstate.Global.Start(taskID, "metadata", "刷新元数据", "正在刷新单条元数据")
	go func() {
		if err := service.NewMetadataService().RefreshSingleMetadata(uint(id)); err != nil {
			log.Printf("metadata refresh failed for %d: %v", id, err)
			taskstate.Global.Fail(taskID, err)
			return
		}
		taskstate.Global.Complete(taskID, "元数据刷新完成")
	}()
	v1Message(c, http.StatusAccepted, "元数据刷新已经启动", gin.H{"task_id": taskID, "status": "running"})
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
	taskID := "local-metadata-" + c.Param("id")
	taskstate.Global.Start(taskID, "metadata", "刷新本地番剧元数据", "正在刷新 "+anime.Title)
	go func(target *model.LocalAnime) {
		if err := enrichV1LocalAnime(target); err != nil {
			log.Printf("local metadata refresh failed for %d: %v", target.ID, err)
			updateLocalMetadataIssue(target, err)
			taskstate.Global.Fail(taskID, err)
			return
		}
		updateLocalMetadataIssue(target, nil)
		taskstate.Global.Complete(taskID, "本地番剧元数据刷新完成")
	}(anime)
	v1Message(c, http.StatusAccepted, "本地番剧元数据刷新已经启动", gin.H{"task_id": taskID, "status": "running"})
}

func updateLocalMetadataIssue(anime *model.LocalAnime, refreshErr error) {
	if anime == nil || anime.ID == 0 {
		return
	}
	issueKey := "scrape:" + strconv.FormatUint(uint64(anime.ID), 10)
	if refreshErr == nil {
		_ = service.ResolveLibraryIssue(issueKey)
		return
	}
	_ = service.ReportLibraryIssue(service.LibraryIssueInput{
		IssueKey:      issueKey,
		IssueType:     service.LibraryIssueTypeScrape,
		Title:         anime.Title,
		DirectoryPath: anime.Path,
		LocalAnimeID:  &anime.ID,
		Message:       refreshErr.Error(),
		Hint:          "元数据刷新会自动重试临时数据库锁；若仍失败，请等待扫描结束后再次刷新。",
	})
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
	case "mikan":
		_, err := newConfiguredMikanParser().GetDashboardContext(c.Request.Context(), "", "")
		if err != nil {
			detail = humanizeOperationError(err.Error())
		} else {
			connected = true
		}
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
	taskID := "repo-update-" + action
	var title string
	switch action {
	case "check":
		title = "检查应用更新"
	case "apply":
		title = "下载并应用更新"
	default:
		v1Error(c, http.StatusBadRequest, "invalid_update_action", "不支持的更新操作")
		return
	}
	taskstate.Global.Start(taskID, "updater", title, title+"进行中")
	go func() {
		var status updater.Status
		if action == "check" {
			status = updater.CheckNow("api-v1")
		} else {
			status = updater.CheckAndPullNow("api-v1")
		}
		if strings.TrimSpace(status.LastError) != "" {
			taskstate.Global.Fail(taskID, fmt.Errorf("%s", status.LastError))
			return
		}
		taskstate.Global.Complete(taskID, status.LastMessage)
	}()
	v1Message(c, http.StatusAccepted, "更新任务已经启动", gin.H{"task_id": taskID, "status": "running"})
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
