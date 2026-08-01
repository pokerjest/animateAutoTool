package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/ai"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/runtimejournal"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/pokerjest/animateAutoTool/internal/taskstate"
)

// GlobalAIRegistry holds the model-visible read/proposal tools and the
// server-only write tools. A tool handler is always called through the
// registry executor so it receives schema validation, timeouts and logging.
var GlobalAIRegistry *ai.Registry

func init() {
	GlobalAIRegistry = ai.NewRegistry()
	GlobalAIRegistry.SetObserver(service.RecordAIToolRun)
	registerTools()
}

func registerTools() {
	GlobalAIRegistry.Register("get_system_status",
		"获取当前系统的运行状态，包括内存使用、协程数量和运行时间",
		ai.JSONSchemaObject(map[string]any{}, []string{}),
		func(ctx context.Context, args string) (string, error) {
			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)
			return marshalToolResult(map[string]any{
				"uptime_seconds":   int64(time.Since(runtimeStatsStartedAt).Seconds()),
				"goroutines":       runtime.NumGoroutine(),
				"heap_alloc_bytes": mem.HeapAlloc,
			})
		})

	GlobalAIRegistry.Register("get_health_report",
		"读取当前健康报告、配置完整性和确定性修复建议。只读。",
		ai.JSONSchemaObject(map[string]any{}, []string{}),
		func(ctx context.Context, args string) (string, error) {
			return marshalToolResult(buildHealthReport())
		})

	GlobalAIRegistry.Register("get_library_issue_context",
		"读取一个本地媒体库问题及其上下文。只读。",
		ai.JSONSchemaObject(map[string]any{
			"issue_id": ai.JSONSchemaProperty("integer", "媒体库问题 ID"),
		}, []string{"issue_id"}),
		getLibraryIssueContextTool)

	GlobalAIRegistry.Register("get_filename_context",
		"读取文件名识别所需的文件、父目录、相邻视频、番剧和已有剧集上下文。只读。",
		ai.JSONSchemaObject(map[string]any{
			"local_anime_id": ai.JSONSchemaProperty("integer", "本地番剧 ID"),
			"path":           ai.JSONSchemaProperty("string", "待识别视频的绝对路径"),
		}, []string{"local_anime_id", "path"}),
		getFilenameContextTool)

	GlobalAIRegistry.Register("get_local_anime_context",
		"读取本地番剧及其剧集、元数据和路径上下文。只读。",
		ai.JSONSchemaObject(map[string]any{
			"local_anime_id": ai.JSONSchemaProperty("integer", "本地番剧 ID"),
		}, []string{"local_anime_id"}),
		getLocalAnimeContextTool)

	GlobalAIRegistry.Register("get_metadata_candidates",
		"兼容旧流程：从单个已配置的 Bangumi、TMDB 或 AniList 搜索真实候选。需要跨源匹配时请使用 search_metadata_sources。只读。",
		ai.JSONSchemaObject(map[string]any{
			"local_anime_id": ai.JSONSchemaProperty("integer", "本地番剧 ID"),
			"source":         ai.JSONSchemaProperty("string", "bangumi、tmdb 或 anilist"),
			"query":          ai.JSONSchemaProperty("string", "可选的搜索关键词"),
		}, []string{"local_anime_id", "source"}),
		getMetadataCandidatesTool)

	GlobalAIRegistry.Register("search_metadata_sources",
		"从已批准的 Bangumi、TMDB、AniList 接口联查同一作品的真实候选。只读，AI 不得自行访问 URL 或虚构 ID。",
		ai.JSONSchemaObject(map[string]any{
			"local_anime_id": ai.JSONSchemaProperty("integer", "本地番剧 ID"),
			"source":         ai.JSONSchemaProperty("string", "可选的起始来源：bangumi、tmdb 或 anilist"),
			"source_id":      ai.JSONSchemaProperty("integer", "可选的起始来源 ID"),
			"query":          ai.JSONSchemaProperty("string", "可选的标题或关键词"),
		}, []string{"local_anime_id"}),
		searchMetadataSourcesTool)

	GlobalAIRegistry.Register("get_subscription_diagnostics",
		"读取订阅配置、最近检查结果、下载记录和 RSS 诊断。只读。",
		ai.JSONSchemaObject(map[string]any{
			"subscription_id": ai.JSONSchemaProperty("integer", "订阅 ID"),
		}, []string{"subscription_id"}),
		getSubscriptionDiagnosticsTool)

	GlobalAIRegistry.Register("get_sanitized_log_excerpt",
		"读取最近的健康日志异常片段。只允许读取应用日志目录，结果自动脱敏。",
		ai.JSONSchemaObject(map[string]any{
			"query": ai.JSONSchemaProperty("string", "可选的关键词"),
			"limit": ai.JSONSchemaProperty("integer", "最多返回的行数，默认 40"),
		}, []string{}),
		getSanitizedLogExcerptTool)

	GlobalAIRegistry.RegisterProposal("preview_filename_resolution",
		"根据识别出的季度和集数创建一个待确认的文件整理提案，不执行文件修改。",
		ai.JSONSchemaObject(map[string]any{
			"local_anime_id":   ai.JSONSchemaProperty("integer", "本地番剧 ID"),
			"path":             ai.JSONSchemaProperty("string", "视频路径"),
			"season":           ai.JSONSchemaProperty("integer", "季度号"),
			"episode":          ai.JSONSchemaProperty("integer", "集数"),
			"episode_end":      ai.JSONSchemaProperty("integer", "多集文件的结束集数，可选"),
			"episode_type":     ai.JSONSchemaProperty("string", "episode、special、ova、opening、ending、trailer 或 collection"),
			"absolute_episode": ai.JSONSchemaProperty("integer", "可选绝对集数"),
			"confidence":       map[string]any{"type": "number", "minimum": 0, "maximum": 1, "description": "0 到 1 的置信度"},
			"summary":          ai.JSONSchemaProperty("string", "简短说明"),
			"evidence":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "分析依据"},
			"warnings":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "风险提示"},
		}, []string{"local_anime_id", "path", "season", "episode", "confidence", "summary"}),
		previewFilenameResolutionTool)

	GlobalAIRegistry.RegisterProposal("propose_metadata_match",
		"根据后端提供的真实候选创建待确认的元数据匹配提案，不执行修改。",
		ai.JSONSchemaObject(map[string]any{
			"local_anime_id": ai.JSONSchemaProperty("integer", "本地番剧 ID"),
			"source":         ai.JSONSchemaProperty("string", "兼容旧客户端的数据源"),
			"source_id":      ai.JSONSchemaProperty("integer", "兼容旧客户端的候选 ID"),
			"matches": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
				"bangumi_id": ai.JSONSchemaProperty("integer", "Bangumi ID"),
				"tmdb_id":    ai.JSONSchemaProperty("integer", "TMDB ID"),
				"anilist_id": ai.JSONSchemaProperty("integer", "AniList ID"),
			}},
			"query":      ai.JSONSchemaProperty("string", "后端候选搜索关键词"),
			"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1, "description": "0 到 1 的置信度"},
			"summary":    ai.JSONSchemaProperty("string", "匹配理由"),
			"evidence":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "分析依据"},
			"warnings":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "冲突或风险提示"},
		}, []string{"local_anime_id", "confidence", "summary"}),
		proposeMetadataMatchTool)

	GlobalAIRegistry.RegisterProposal("propose_health_repair",
		"把健康诊断映射成已有修复动作的待确认提案，不执行修改。",
		ai.JSONSchemaObject(map[string]any{
			"target_type": ai.JSONSchemaProperty("string", "问题目标类型"),
			"target_id":   ai.JSONSchemaProperty("string", "问题目标 ID"),
			"action":      ai.JSONSchemaProperty("string", "现有确定性修复动作"),
			"confidence":  map[string]any{"type": "number", "minimum": 0, "maximum": 1, "description": "0 到 1 的置信度"},
			"summary":     ai.JSONSchemaProperty("string", "原因和依据"),
			"evidence":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "诊断证据"},
			"warnings":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "修复风险"},
		}, []string{"target_type", "target_id", "action", "confidence", "summary"}),
		proposeHealthRepairTool)

	GlobalAIRegistry.RegisterProposal("propose_subscription_rules",
		"校验订阅规则并创建待确认提案，同时返回近期 RSS 条目的修改前后预览。",
		ai.JSONSchemaObject(map[string]any{
			"subscription_id":   ai.JSONSchemaProperty("integer", "订阅 ID"),
			"filter_rule":       ai.JSONSchemaProperty("string", "建议包含规则"),
			"exclude_rule":      ai.JSONSchemaProperty("string", "建议排除规则"),
			"resolution_filter": ai.JSONSchemaProperty("string", "建议清晰度"),
			"subtitle_language": ai.JSONSchemaProperty("string", "建议字幕语言"),
			"confidence":        map[string]any{"type": "number", "minimum": 0, "maximum": 1, "description": "0 到 1 的置信度"},
			"summary":           ai.JSONSchemaProperty("string", "建议理由"),
			"evidence":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "规则依据"},
			"warnings":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "规则风险"},
		}, []string{"subscription_id", "filter_rule", "exclude_rule", "resolution_filter", "subtitle_language", "confidence", "summary"}),
		proposeSubscriptionRulesTool)

	GlobalAIRegistry.RegisterProposal("propose_library_scan",
		"创建一个待确认的本地库扫描提案。不会直接启动扫描。",
		ai.JSONSchemaObject(map[string]any{
			"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1, "description": "0 到 1 的置信度"},
			"summary":    ai.JSONSchemaProperty("string", "扫描理由"),
		}, []string{"confidence", "summary"}),
		proposeLibraryScanTool)

	GlobalAIRegistry.RegisterWrite("apply_local_organize_proposal",
		"执行已经在页面确认的本地整理提案。",
		ai.JSONSchemaObject(map[string]any{
			"plan_id":           ai.JSONSchemaProperty("string", "已有整理预览计划"),
			"include_anime_ids": ai.JSONSchemaProperty("array", "要执行的番剧 ID"),
		}, []string{"plan_id"}),
		applyLocalOrganizeProposalTool)

	GlobalAIRegistry.RegisterWrite("apply_metadata_match_proposal",
		"执行已经在页面确认的元数据匹配提案。",
		ai.JSONSchemaObject(map[string]any{
			"local_anime_id": ai.JSONSchemaProperty("integer", "本地番剧 ID"),
			"source":         ai.JSONSchemaProperty("string", "兼容旧客户端的真实候选来源"),
			"source_id":      ai.JSONSchemaProperty("integer", "兼容旧客户端的真实候选 ID"),
			"matches": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
				"bangumi_id": ai.JSONSchemaProperty("integer", "Bangumi ID"),
				"tmdb_id":    ai.JSONSchemaProperty("integer", "TMDB ID"),
				"anilist_id": ai.JSONSchemaProperty("integer", "AniList ID"),
			}},
		}, []string{"local_anime_id"}),
		applyMetadataMatchProposalTool)

	GlobalAIRegistry.RegisterWrite("apply_subscription_rule_proposal",
		"执行已经在页面确认并通过正则校验的订阅规则提案。",
		ai.JSONSchemaObject(map[string]any{
			"subscription_id":   ai.JSONSchemaProperty("integer", "订阅 ID"),
			"filter_rule":       ai.JSONSchemaProperty("string", "包含规则"),
			"exclude_rule":      ai.JSONSchemaProperty("string", "排除规则"),
			"resolution_filter": ai.JSONSchemaProperty("string", "清晰度筛选"),
			"subtitle_language": ai.JSONSchemaProperty("string", "字幕语言"),
		}, []string{"subscription_id"}),
		applySubscriptionRuleProposalTool)

	GlobalAIRegistry.RegisterWrite("run_confirmed_library_scan",
		"执行已经在页面确认的本地媒体库扫描。",
		ai.JSONSchemaObject(map[string]any{}, []string{}),
		runConfirmedLibraryScanTool)

	GlobalAIRegistry.RegisterWrite("run_confirmed_repair_action",
		"执行已经在页面确认且属于白名单的健康修复动作。",
		ai.JSONSchemaObject(map[string]any{
			"action":    ai.JSONSchemaProperty("string", "白名单修复动作"),
			"target_id": ai.JSONSchemaProperty("string", "可选目标 ID"),
		}, []string{"action"}),
		runConfirmedRepairActionTool)
}

func marshalToolResult(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func decodeToolArgs(raw string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("参数格式不正确: %w", err)
	}
	return nil
}

func getLibraryIssueContextTool(ctx context.Context, raw string) (string, error) {
	var req struct {
		IssueID uint `json:"issue_id"`
	}
	if err := decodeToolArgs(raw, &req); err != nil || req.IssueID == 0 {
		return "", errors.New("issue_id 无效")
	}
	var issue model.LibraryIssue
	if err := db.DB.First(&issue, req.IssueID).Error; err != nil {
		return "", err
	}
	return marshalToolResult(issue)
}

func getLocalAnimeContextTool(ctx context.Context, raw string) (string, error) {
	var req struct {
		LocalAnimeID uint `json:"local_anime_id"`
	}
	if err := decodeToolArgs(raw, &req); err != nil || req.LocalAnimeID == 0 {
		return "", errors.New("local_anime_id 无效")
	}
	var anime model.LocalAnime
	if err := db.DB.Preload("Metadata").First(&anime, req.LocalAnimeID).Error; err != nil {
		return "", err
	}
	var episodes []model.LocalEpisode
	if err := db.DB.Where("local_anime_id = ?", anime.ID).Order("season_num, episode_num, id").Find(&episodes).Error; err != nil {
		return "", err
	}
	return marshalToolResult(map[string]any{"anime": anime, "episodes": episodes})
}

func getFilenameContextTool(ctx context.Context, raw string) (string, error) {
	var req struct {
		LocalAnimeID uint   `json:"local_anime_id"`
		Path         string `json:"path"`
	}
	if err := decodeToolArgs(raw, &req); err != nil || req.LocalAnimeID == 0 || strings.TrimSpace(req.Path) == "" {
		return "", errors.New("local_anime_id 和 path 都不能为空")
	}
	var anime model.LocalAnime
	if err := db.DB.First(&anime, req.LocalAnimeID).Error; err != nil {
		return "", err
	}
	root := filepath.Clean(anime.Path)
	path := filepath.Clean(req.Path)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("文件路径不属于当前番剧目录")
	}
	var episodes []model.LocalEpisode
	if err := db.DB.Where("local_anime_id = ?", anime.ID).Order("season_num, episode_num, id").Find(&episodes).Error; err != nil {
		return "", err
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	neighbors := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !parser.IsVideoFile(entry.Name()) {
			continue
		}
		neighbors = append(neighbors, entry.Name())
	}
	sort.Strings(neighbors)
	if len(neighbors) > 30 {
		neighbors = neighbors[:30]
	}
	return marshalToolResult(map[string]any{
		"anime":         anime,
		"episodes":      episodes,
		"path":          path,
		"relative_path": filepath.ToSlash(rel),
		"filename":      filepath.Base(path),
		"parent":        filepath.Base(filepath.Dir(path)),
		"neighbors":     neighbors,
		"parsed":        parser.ParseFilename(path),
	})
}

func getMetadataCandidatesTool(ctx context.Context, raw string) (string, error) {
	var req struct {
		LocalAnimeID uint   `json:"local_anime_id"`
		Source       string `json:"source"`
		Query        string `json:"query"`
	}
	if err := decodeToolArgs(raw, &req); err != nil || req.LocalAnimeID == 0 {
		return "", errors.New("local_anime_id 无效")
	}
	var anime model.LocalAnime
	if err := db.DB.Preload("Metadata").First(&anime, req.LocalAnimeID).Error; err != nil {
		return "", err
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		query = anime.Title
		if anime.Metadata != nil && anime.Metadata.TitleCN != "" {
			query = anime.Metadata.TitleCN
		}
	}
	results, err := searchMetadataCandidates(ctx, strings.ToLower(strings.TrimSpace(req.Source)), query)
	if err != nil {
		return "", err
	}
	return marshalToolResult(map[string]any{"query": query, "source": req.Source, "candidates": results})
}

func searchMetadataSourcesTool(ctx context.Context, raw string) (string, error) {
	var req struct {
		LocalAnimeID uint   `json:"local_anime_id"`
		Source       string `json:"source"`
		SourceID     int    `json:"source_id"`
		Query        string `json:"query"`
	}
	if err := decodeToolArgs(raw, &req); err != nil || req.LocalAnimeID == 0 {
		return "", errors.New("local_anime_id 无效")
	}
	var anime model.LocalAnime
	if err := db.DB.Preload("Metadata").First(&anime, req.LocalAnimeID).Error; err != nil {
		return "", err
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		query = anime.Title
		if anime.Metadata != nil {
			query = firstNonEmpty(query, anime.Metadata.TitleCN, anime.Metadata.TitleJP, anime.Metadata.TitleEN)
		}
	}
	result, err := searchMetadataMatchCandidates(ctx, metadataSearchOptions{
		Query: query, Source: req.Source, SourceID: req.SourceID,
	})
	if err != nil {
		return "", err
	}
	return marshalToolResult(result)
}

func getSubscriptionDiagnosticsTool(ctx context.Context, raw string) (string, error) {
	var req struct {
		SubscriptionID uint `json:"subscription_id"`
	}
	if err := decodeToolArgs(raw, &req); err != nil || req.SubscriptionID == 0 {
		return "", errors.New("subscription_id 无效")
	}
	var subscription model.Subscription
	if err := db.DB.First(&subscription, req.SubscriptionID).Error; err != nil {
		return "", err
	}
	var runs []model.SubscriptionRunLog
	_ = db.DB.Where("subscription_id = ?", subscription.ID).Order("checked_at DESC").Limit(10).Find(&runs).Error
	var downloads []model.DownloadLog
	_ = db.DB.Where("subscription_id = ?", subscription.ID).Order("created_at DESC").Limit(30).Find(&downloads).Error
	var rssEpisodes []parser.Episode
	var rssError string
	if strings.TrimSpace(subscription.RSSUrl) != "" {
		episodes, err := newV1MikanClient().ParseContext(ctx, subscription.RSSUrl)
		if err != nil {
			rssError = service.SanitizeAIText(err.Error())
		} else {
			if len(episodes) > 30 {
				episodes = episodes[:30]
			}
			rssEpisodes = episodes
		}
	}
	subscription.FilterRule = service.SanitizeAIText(subscription.FilterRule)
	subscription.ExcludeRule = service.SanitizeAIText(subscription.ExcludeRule)
	return marshalToolResult(map[string]any{
		"subscription": subscription, "recent_runs": runs, "recent_downloads": downloads,
		"rss_episodes": rssEpisodes, "rule_evaluation": service.EvaluateSubscriptionRules(&subscription, rssEpisodes),
		"rss_error": rssError,
	})
}

func getSanitizedLogExcerptTool(ctx context.Context, raw string) (string, error) {
	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := decodeToolArgs(raw, &req); err != nil {
		return "", err
	}
	if req.Limit <= 0 {
		req.Limit = 40
	}
	if req.Limit > 100 {
		req.Limit = 100
	}
	entries, err := os.ReadDir(config.LogsDir())
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
	lines := make([]string, 0, req.Limit)
	query := strings.ToLower(strings.TrimSpace(req.Query))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(config.LogsDir(), entry.Name()))
		if readErr != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if query != "" && !strings.Contains(strings.ToLower(line), query) {
				continue
			}
			line = service.SanitizeAIText(line)
			if strings.TrimSpace(line) == "" {
				continue
			}
			lines = append(lines, line)
			if len(lines) >= req.Limit {
				break
			}
		}
		if len(lines) >= req.Limit {
			break
		}
	}
	return marshalToolResult(map[string]any{"lines": lines, "count": len(lines)})
}

func currentAIToolMeta(ctx context.Context) ai.ToolExecutionMeta {
	return ai.ToolExecutionMetaFromContext(ctx)
}

func proposalExpiry(kind string) time.Time {
	if kind == service.AIProposalTypeFilenameResolution {
		return time.Now().Add(15 * time.Minute)
	}
	return time.Now().Add(24 * time.Hour)
}

func previewFilenameResolutionTool(ctx context.Context, raw string) (string, error) {
	var req struct {
		LocalAnimeID    uint     `json:"local_anime_id"`
		Path            string   `json:"path"`
		Season          int      `json:"season"`
		Episode         int      `json:"episode"`
		EpisodeEnd      int      `json:"episode_end"`
		EpisodeType     string   `json:"episode_type"`
		AbsoluteEpisode int      `json:"absolute_episode"`
		Confidence      float64  `json:"confidence"`
		Summary         string   `json:"summary"`
		Evidence        []string `json:"evidence"`
		Warnings        []string `json:"warnings"`
	}
	if err := decodeToolArgs(raw, &req); err != nil || req.LocalAnimeID == 0 || req.Episode <= 0 {
		return "", errors.New("文件识别提案参数无效")
	}
	req.EpisodeType = strings.ToLower(strings.TrimSpace(req.EpisodeType))
	if req.EpisodeType == "" {
		req.EpisodeType = "episode"
	}
	switch req.EpisodeType {
	case "episode", "special", "ova", "opening", "ending", "trailer", "collection":
	default:
		return "", errors.New("文件识别提案包含不支持的剧集类型")
	}
	if req.EpisodeEnd == 0 {
		req.EpisodeEnd = req.Episode
	}
	if req.EpisodeEnd < req.Episode {
		return "", errors.New("文件识别提案的结束集数不能小于开始集数")
	}
	meta := currentAIToolMeta(ctx)
	fingerprint, err := filenameProposalFingerprint(req.LocalAnimeID, req.Path)
	if err != nil {
		return "", err
	}
	organizer, err := newLocalOrganizer()
	if err != nil {
		return "", err
	}
	preview, err := organizer.Preview(strconv.FormatUint(uint64(meta.UserID), 10), service.LocalOrganizePreviewRequest{
		Selection: service.LocalOrganizeSelection{Mode: service.OrganizeSelectionIDs, AnimeIDs: []uint{req.LocalAnimeID}},
		EpisodeOverrides: []service.LocalOrganizeEpisodeOverride{{
			Path: req.Path, Season: req.Season, Episode: req.Episode, EpisodeEnd: req.EpisodeEnd,
			EpisodeType: req.EpisodeType, AbsoluteEpisode: req.AbsoluteEpisode,
		}},
	})
	if err != nil {
		return "", err
	}
	payload := map[string]any{
		"local_anime_id": req.LocalAnimeID, "path": req.Path, "season": req.Season, "episode": req.Episode,
		"episode_end": req.EpisodeEnd, "episode_type": req.EpisodeType, "absolute_episode": req.AbsoluteEpisode,
		"organize_plan_id": preview.PlanID, "include_anime_ids": []uint{req.LocalAnimeID}, "preview": preview,
	}
	input := service.AIProposalInput{
		UserID: meta.UserID, Type: service.AIProposalTypeFilenameResolution, TargetType: "local_file",
		TargetID: req.Path, Summary: req.Summary, Confidence: req.Confidence,
		Evidence:         append(req.Evidence, "目标路径由 AnimateTool 整理模板生成"),
		Warnings:         req.Warnings,
		Payload:          payload,
		InputFingerprint: fingerprint, ApplyTool: "apply_local_organize_proposal",
		Provider: meta.Provider, Model: meta.Model, Status: service.AIProposalStatusReady,
		ExpiresAt: ptrTime(proposalExpiry(service.AIProposalTypeFilenameResolution)),
	}
	row, err := completeOrCreateToolProposal(meta.ProposalID, input)
	if err != nil {
		return "", err
	}
	return marshalToolResult(map[string]any{
		"proposal_id": row.ID, "status": row.Status, "message": "已创建待确认文件整理提案",
		"review_url": "/local-anime",
	})
}

func proposeMetadataMatchTool(ctx context.Context, raw string) (string, error) {
	var req struct {
		LocalAnimeID uint   `json:"local_anime_id"`
		Source       string `json:"source"`
		SourceID     int    `json:"source_id"`
		Matches      *struct {
			BangumiID int `json:"bangumi_id"`
			TMDBID    int `json:"tmdb_id"`
			AniListID int `json:"anilist_id"`
		} `json:"matches"`
		Query      string   `json:"query"`
		Confidence float64  `json:"confidence"`
		Summary    string   `json:"summary"`
		Evidence   []string `json:"evidence"`
		Warnings   []string `json:"warnings"`
	}
	if err := decodeToolArgs(raw, &req); err != nil || req.LocalAnimeID == 0 {
		return "", errors.New("元数据提案参数无效")
	}
	var anime model.LocalAnime
	if err := db.DB.Preload("Metadata").First(&anime, req.LocalAnimeID).Error; err != nil {
		return "", err
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		query = anime.Title
	}
	source := strings.ToLower(strings.TrimSpace(req.Source))
	if source == "" {
		source = SourceBangumi
	}
	sourceID := req.SourceID
	if req.Matches != nil {
		switch source {
		case SourceBangumi:
			sourceID = req.Matches.BangumiID
		case SourceTMDB:
			sourceID = req.Matches.TMDBID
		case SourceAniList:
			sourceID = req.Matches.AniListID
		}
	}
	matchResult, err := searchMetadataMatchCandidates(ctx, metadataSearchOptions{Query: query, Source: source, SourceID: sourceID})
	if err != nil {
		return "", err
	}
	matches := struct {
		BangumiID int `json:"bangumi_id"`
		TMDBID    int `json:"tmdb_id"`
		AniListID int `json:"anilist_id"`
	}{}
	if req.Matches != nil {
		matches = *req.Matches
	} else {
		switch source {
		case SourceBangumi:
			matches.BangumiID = req.SourceID
		case SourceTMDB:
			matches.TMDBID = req.SourceID
		case SourceAniList:
			matches.AniListID = req.SourceID
		}
	}
	if matches.BangumiID <= 0 && matches.TMDBID <= 0 && matches.AniListID <= 0 {
		return "", errors.New("至少需要一个真实元数据来源 ID")
	}
	var candidate *MetadataMatchCandidate
	for i := range matchResult.Candidates {
		if candidateMatchesIDs(matchResult.Candidates[i], matches.BangumiID, matches.TMDBID, matches.AniListID) {
			candidate = &matchResult.Candidates[i]
			break
		}
	}
	if candidate == nil {
		return "", errors.New("AI 选择的元数据 ID 不在后端三源候选列表中")
	}
	meta := currentAIToolMeta(ctx)
	input := service.AIProposalInput{
		UserID: meta.UserID, Type: service.AIProposalTypeMetadataMatch, TargetType: "local_anime",
		TargetID: strconv.FormatUint(uint64(req.LocalAnimeID), 10), Summary: req.Summary, Confidence: req.Confidence,
		Evidence: append(req.Evidence, "候选由后端三源元数据搜索工具提供"),
		Warnings: req.Warnings,
		Payload: map[string]any{
			"local_anime_id": req.LocalAnimeID, "source": source, "source_id": req.SourceID,
			"matches": matches, "candidate": candidate, "candidates": matchResult.Candidates,
			"source_status": matchResult.SourceStatus, "query": query,
		},
		InputFingerprint: metadataProposalFingerprint(anime),
		ApplyTool:        "apply_metadata_match_proposal", Provider: meta.Provider, Model: meta.Model,
		Status: service.AIProposalStatusReady, ExpiresAt: ptrTime(proposalExpiry(service.AIProposalTypeMetadataMatch)),
	}
	row, err := completeOrCreateToolProposal(meta.ProposalID, input)
	if err != nil {
		return "", err
	}
	return marshalToolResult(map[string]any{"proposal_id": row.ID, "status": row.Status, "review_url": "/local-anime"})
}

func candidateMatchesIDs(candidate MetadataMatchCandidate, bangumiID, tmdbID, aniListID int) bool {
	if bangumiID > 0 && (candidate.Bangumi == nil || candidate.Bangumi.ID != bangumiID) {
		return false
	}
	if tmdbID > 0 && (candidate.TMDB == nil || candidate.TMDB.ID != tmdbID) {
		return false
	}
	if aniListID > 0 && (candidate.AniList == nil || candidate.AniList.ID != aniListID) {
		return false
	}
	return true
}

func proposeHealthRepairTool(ctx context.Context, raw string) (string, error) {
	var req struct {
		TargetType string   `json:"target_type"`
		TargetID   string   `json:"target_id"`
		Action     string   `json:"action"`
		Confidence float64  `json:"confidence"`
		Summary    string   `json:"summary"`
		Evidence   []string `json:"evidence"`
		Warnings   []string `json:"warnings"`
	}
	if err := decodeToolArgs(raw, &req); err != nil || strings.TrimSpace(req.Action) == "" {
		return "", errors.New("健康修复提案参数无效")
	}
	meta := currentAIToolMeta(ctx)
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if !allowedAIRepairAction(action) {
		action = ""
	}
	input := service.AIProposalInput{
		UserID: meta.UserID, Type: service.AIProposalTypeHealthDiagnosis, TargetType: req.TargetType, TargetID: req.TargetID,
		Summary: req.Summary, Confidence: req.Confidence, Evidence: append(req.Evidence, "由健康报告和已有问题工具推导"), Warnings: req.Warnings,
		Payload:  map[string]any{"action": action},
		Provider: meta.Provider, Model: meta.Model, Status: service.AIProposalStatusReady,
		ExpiresAt: ptrTime(proposalExpiry(service.AIProposalTypeHealthDiagnosis)),
	}
	if action != "" {
		input.ApplyTool = "run_confirmed_repair_action"
	}
	row, err := completeOrCreateToolProposal(meta.ProposalID, input)
	if err != nil {
		return "", err
	}
	return marshalToolResult(map[string]any{
		"proposal_id": row.ID, "status": row.Status,
		"review_url": "/health",
	})
}

func proposeLibraryScanTool(ctx context.Context, raw string) (string, error) {
	var req struct {
		Confidence float64  `json:"confidence"`
		Summary    string   `json:"summary"`
		Evidence   []string `json:"evidence"`
		Warnings   []string `json:"warnings"`
	}
	if err := decodeToolArgs(raw, &req); err != nil {
		return "", err
	}
	meta := currentAIToolMeta(ctx)
	input := service.AIProposalInput{
		UserID: meta.UserID, Type: service.AIProposalTypeLibraryScan, TargetType: "library", TargetID: "all",
		Summary: req.Summary, Confidence: req.Confidence, Evidence: append(req.Evidence, "AI 仅创建提案，不会直接启动扫描"), Warnings: req.Warnings,
		Payload: map[string]any{"scope": "all"}, ApplyTool: "run_confirmed_library_scan",
		Provider: meta.Provider, Model: meta.Model, Status: service.AIProposalStatusReady,
		ExpiresAt: ptrTime(proposalExpiry(service.AIProposalTypeLibraryScan)),
	}
	row, err := completeOrCreateToolProposal(meta.ProposalID, input)
	if err != nil {
		return "", err
	}
	return marshalToolResult(map[string]any{
		"proposal_id": row.ID, "status": row.Status,
		"review_url": "/local-anime",
	})
}

func proposeSubscriptionRulesTool(ctx context.Context, raw string) (string, error) {
	var req struct {
		SubscriptionID   uint     `json:"subscription_id"`
		FilterRule       string   `json:"filter_rule"`
		ExcludeRule      string   `json:"exclude_rule"`
		ResolutionFilter string   `json:"resolution_filter"`
		SubtitleLanguage string   `json:"subtitle_language"`
		Confidence       float64  `json:"confidence"`
		Summary          string   `json:"summary"`
		Evidence         []string `json:"evidence"`
		Warnings         []string `json:"warnings"`
	}
	if err := decodeToolArgs(raw, &req); err != nil || req.SubscriptionID == 0 {
		return "", errors.New("订阅规则提案参数无效")
	}
	if err := service.ValidateSubscriptionPattern(req.FilterRule); err != nil {
		return "", fmt.Errorf("包含规则不是有效正则: %w", err)
	}
	if err := service.ValidateSubscriptionPattern(req.ExcludeRule); err != nil {
		return "", fmt.Errorf("排除规则不是有效正则: %w", err)
	}
	resolution, ok := service.NormalizeResolutionFilter(req.ResolutionFilter)
	if !ok {
		return "", errors.New("清晰度筛选无效")
	}
	language, ok := service.NormalizeSubtitleLanguage(req.SubtitleLanguage)
	if !ok {
		return "", errors.New("字幕语言筛选无效")
	}
	var subscription model.Subscription
	if err := db.DB.First(&subscription, req.SubscriptionID).Error; err != nil {
		return "", err
	}
	episodes, _ := newV1MikanClient().ParseContext(ctx, subscription.RSSUrl)
	if len(episodes) > 30 {
		episodes = episodes[:30]
	}
	before := service.EvaluateSubscriptionRules(&subscription, episodes)
	suggested := subscription
	suggested.FilterRule = strings.TrimSpace(req.FilterRule)
	suggested.ExcludeRule = strings.TrimSpace(req.ExcludeRule)
	suggested.ResolutionFilter = resolution
	suggested.SubtitleLanguage = language
	after := service.EvaluateSubscriptionRules(&suggested, episodes)
	meta := currentAIToolMeta(ctx)
	input := service.AIProposalInput{
		UserID: meta.UserID, Type: service.AIProposalTypeSubscriptionRule, TargetType: "subscription",
		TargetID: strconv.FormatUint(uint64(subscription.ID), 10), Summary: req.Summary, Confidence: req.Confidence,
		Evidence: append(req.Evidence, "建议规则已通过服务端正则校验并对近期 RSS 样本重新预览"),
		Warnings: req.Warnings,
		Payload: map[string]any{
			"subscription_id": subscription.ID, "filter_rule": suggested.FilterRule, "exclude_rule": suggested.ExcludeRule,
			"resolution_filter": resolution, "subtitle_language": language, "before_evaluation": before, "after_evaluation": after,
		},
		InputFingerprint: subscriptionProposalFingerprint(subscription), ApplyTool: "apply_subscription_rule_proposal",
		Provider: meta.Provider, Model: meta.Model, Status: service.AIProposalStatusReady,
		ExpiresAt: ptrTime(proposalExpiry(service.AIProposalTypeSubscriptionRule)),
	}
	row, err := completeOrCreateToolProposal(meta.ProposalID, input)
	if err != nil {
		return "", err
	}
	return marshalToolResult(map[string]any{
		"proposal_id": row.ID, "status": service.AIProposalStatusReady,
		"review_url": "/subscriptions",
	})
}

func completeOrCreateToolProposal(proposalID string, input service.AIProposalInput) (*model.AIProposal, error) {
	if strings.TrimSpace(proposalID) == "" {
		return service.CreateAIProposal(input)
	}
	if err := service.CompleteAIProposal(proposalID, input); err != nil {
		return nil, err
	}
	return service.GetAIProposal(input.UserID, proposalID)
}

func filenameProposalFingerprint(localAnimeID uint, path string) (string, error) {
	var anime model.LocalAnime
	if err := db.DB.First(&anime, localAnimeID).Error; err != nil {
		return "", err
	}
	clean := filepath.Clean(path)
	rel, err := filepath.Rel(filepath.Clean(anime.Path), clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("文件路径不属于当前番剧目录")
	}
	info, err := os.Stat(clean)
	if err != nil {
		return "", err
	}
	return service.FingerprintAIInput(map[string]any{
		"anime_id": anime.ID, "anime_updated_at": anime.UpdatedAt.UTC().UnixNano(),
		"path": clean, "size": info.Size(), "mod_time": info.ModTime().UnixNano(),
	}), nil
}

func metadataProposalFingerprint(anime model.LocalAnime) string {
	metadataUpdatedAt := int64(0)
	if anime.Metadata != nil {
		metadataUpdatedAt = anime.Metadata.UpdatedAt.UTC().UnixNano()
	}
	return service.FingerprintAIInput(map[string]any{
		"anime_id": anime.ID, "anime_updated_at": anime.UpdatedAt.UTC().UnixNano(),
		"metadata_id": anime.MetadataID, "metadata_updated_at": metadataUpdatedAt,
	})
}

func subscriptionProposalFingerprint(subscription model.Subscription) string {
	return service.FingerprintAIInput(map[string]any{
		"id": subscription.ID, "updated_at": subscription.UpdatedAt.UTC().UnixNano(),
		"filter_rule": subscription.FilterRule, "exclude_rule": subscription.ExcludeRule,
		"resolution_filter": subscription.ResolutionFilter, "subtitle_language": subscription.SubtitleLanguage,
	})
}

func allowedAIRepairAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "repair_download_logs", "refresh_jellyfin", "scan_library":
		return true
	default:
		return false
	}
}

func applyLocalOrganizeProposalTool(ctx context.Context, raw string) (string, error) {
	var req struct {
		PlanID          string `json:"plan_id"`
		IncludeAnimeIDs []uint `json:"include_anime_ids"`
	}
	if err := decodeToolArgs(raw, &req); err != nil || strings.TrimSpace(req.PlanID) == "" {
		return "", errors.New("整理计划参数无效")
	}
	meta := currentAIToolMeta(ctx)
	taskID, err := startLocalOrganizePlan(strconv.FormatUint(uint64(meta.UserID), 10), req.PlanID, req.IncludeAnimeIDs)
	if err != nil {
		return "", err
	}
	return marshalToolResult(map[string]any{"task_id": taskID, "status": "running"})
}

func applyMetadataMatchProposalTool(ctx context.Context, raw string) (string, error) {
	var req struct {
		LocalAnimeID uint   `json:"local_anime_id"`
		Source       string `json:"source"`
		SourceID     int    `json:"source_id"`
		Matches      *struct {
			BangumiID int `json:"bangumi_id"`
			TMDBID    int `json:"tmdb_id"`
			AniListID int `json:"anilist_id"`
		} `json:"matches"`
	}
	if err := decodeToolArgs(raw, &req); err != nil || req.LocalAnimeID == 0 {
		return "", errors.New("元数据匹配参数无效")
	}
	var err error
	if req.Matches != nil {
		err = service.NewMetadataService().MatchSeriesSources(req.LocalAnimeID, req.Matches.BangumiID, req.Matches.TMDBID, req.Matches.AniListID)
	} else {
		err = service.NewMetadataService().MatchSeries(req.LocalAnimeID, strings.ToLower(strings.TrimSpace(req.Source)), req.SourceID)
	}
	if err != nil {
		return "", err
	}
	return marshalToolResult(map[string]any{"status": "success", "local_anime_id": req.LocalAnimeID})
}

func applySubscriptionRuleProposalTool(ctx context.Context, raw string) (string, error) {
	var req struct {
		SubscriptionID   uint   `json:"subscription_id"`
		FilterRule       string `json:"filter_rule"`
		ExcludeRule      string `json:"exclude_rule"`
		ResolutionFilter string `json:"resolution_filter"`
		SubtitleLanguage string `json:"subtitle_language"`
	}
	if err := decodeToolArgs(raw, &req); err != nil || req.SubscriptionID == 0 {
		return "", errors.New("订阅规则参数无效")
	}
	if err := service.ValidateSubscriptionPattern(req.FilterRule); err != nil {
		return "", fmt.Errorf("包含规则不是有效正则: %w", err)
	}
	if err := service.ValidateSubscriptionPattern(req.ExcludeRule); err != nil {
		return "", fmt.Errorf("排除规则不是有效正则: %w", err)
	}
	resolution, ok := service.NormalizeResolutionFilter(req.ResolutionFilter)
	if !ok {
		return "", errors.New("清晰度筛选无效")
	}
	language, ok := service.NormalizeSubtitleLanguage(req.SubtitleLanguage)
	if !ok {
		return "", errors.New("字幕语言筛选无效")
	}
	var subscription model.Subscription
	if err := db.DB.First(&subscription, req.SubscriptionID).Error; err != nil {
		return "", err
	}
	subscription.FilterRule = strings.TrimSpace(req.FilterRule)
	subscription.ExcludeRule = strings.TrimSpace(req.ExcludeRule)
	subscription.ResolutionFilter = resolution
	subscription.SubtitleLanguage = language
	if err := db.DB.Save(&subscription).Error; err != nil {
		return "", err
	}
	taskID := "ai-subscription-recheck-" + strconv.FormatUint(uint64(subscription.ID), 10)
	taskstate.Global.Start(taskID, "subscription-repair", "AI 订阅规则复核", "正在按新规则重新检查订阅")
	GoBackground(func(context.Context) {
		if err := runSubscriptionCheck(&subscription, "ai-rule"); err != nil {
			taskstate.Global.Fail(taskID, err)
			return
		}
		taskstate.Global.Complete(taskID, "订阅规则已保存并完成重新检查")
	})
	return marshalToolResult(map[string]any{"task_id": taskID, "status": "running"})
}

func runConfirmedLibraryScanTool(ctx context.Context, raw string) (string, error) {
	if runtimejournal.RecoveryBlocked() {
		return "", runtimejournal.ErrRecoveryBlocked
	}
	if runtimejournal.RecoveryInProgress() {
		return "", runtimejournal.ErrRecoveryInProgress
	}
	taskID := "ai-library-scan-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	taskstate.Global.Start(taskID, "scan", "AI 确认的本地扫描", "正在扫描本地媒体库")
	GoBackground(func(appCtx context.Context) {
		if err := service.NewScannerService().ScanAllWithProgressContext(appCtx, nil); err != nil {
			taskstate.Global.Fail(taskID, err)
			return
		}
		service.NewAgentService().RunAgentForLibrary()
		if err := service.RequestJellyfinLibraryRefresh(appCtx); err != nil && !errors.Is(err, service.ErrJellyfinNotConfigured) {
			taskstate.Global.Fail(taskID, err)
			return
		}
		taskstate.Global.Complete(taskID, "本地媒体库扫描完成")
	})
	return marshalToolResult(map[string]any{"task_id": taskID, "status": "running"})
}

func runConfirmedRepairActionTool(ctx context.Context, raw string) (string, error) {
	var req struct {
		Action   string `json:"action"`
		TargetID string `json:"target_id"`
	}
	if err := decodeToolArgs(raw, &req); err != nil {
		return "", err
	}
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "repair_download_logs":
		result, err := service.RepairDownloadLogsFromLocalLibrary(6 * time.Hour)
		if err != nil {
			return "", err
		}
		return marshalToolResult(result)
	case "refresh_jellyfin":
		if err := service.RequestJellyfinLibraryRefresh(ctx); err != nil {
			return "", err
		}
		return marshalToolResult(map[string]any{"status": "success"})
	case "scan_library":
		return runConfirmedLibraryScanTool(ctx, "{}")
	default:
		return "", fmt.Errorf("不支持的健康修复动作 %q", req.Action)
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
