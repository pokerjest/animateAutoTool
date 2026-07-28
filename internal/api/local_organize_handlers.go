package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/pokerjest/animateAutoTool/internal/taskstate"
)

var (
	localOrganizeRunMu   sync.Mutex
	localOrganizeRunning bool
	newLocalOrganizer    = service.NewConfiguredLocalOrganizer
)

type localOrganizeApplyRequest struct {
	PlanID          string `json:"plan_id"`
	IncludeAnimeIDs []uint `json:"include_anime_ids,omitempty"`
}

func V1PreviewLocalOrganizeHandler(c *gin.Context) {
	var request service.LocalOrganizePreviewRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		v1Error(c, http.StatusBadRequest, "invalid_organize_request", "整理预览请求格式不正确")
		return
	}
	organizer, err := newLocalOrganizer()
	if err != nil {
		v1Error(c, http.StatusBadGateway, "organizer_unavailable", err.Error())
		return
	}
	preview, err := organizer.Preview(localOrganizeOwner(c), request)
	if err != nil {
		v1Error(c, http.StatusBadRequest, "organize_preview_failed", err.Error())
		return
	}
	v1Data(c, http.StatusOK, preview)
}

func V1ApplyLocalOrganizeHandler(c *gin.Context) {
	var request localOrganizeApplyRequest
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.PlanID) == "" {
		v1Error(c, http.StatusBadRequest, "invalid_organize_plan", "请选择有效的整理预览计划")
		return
	}

	taskID, err := startLocalOrganizePlan(localOrganizeOwner(c), request.PlanID, request.IncludeAnimeIDs)
	if errors.Is(err, errLocalOrganizeInProgress) {
		v1Error(c, http.StatusConflict, "organize_in_progress", "已有整理任务正在运行，请等待完成后再试")
		return
	}
	if errors.Is(err, service.ErrOrganizePlanNotFound) {
		v1Error(c, http.StatusGone, "organize_plan_expired", "整理预览已过期，请重新生成预览")
		return
	}
	if errors.Is(err, errInvalidOrganizeSelection) {
		v1Error(c, http.StatusBadRequest, "invalid_organize_selection", "执行范围不属于当前预览计划")
		return
	}
	if err != nil {
		v1Error(c, http.StatusBadGateway, "organizer_unavailable", err.Error())
		return
	}
	v1Message(c, http.StatusAccepted, "本地番剧整理已经启动", gin.H{"task_id": taskID, "status": "running"})
}

var (
	errLocalOrganizeInProgress  = errors.New("local organize already running")
	errInvalidOrganizeSelection = errors.New("invalid local organize selection")
)

func startLocalOrganizePlan(owner, planID string, includedIDs []uint) (string, error) {
	localOrganizeRunMu.Lock()
	if localOrganizeRunning {
		localOrganizeRunMu.Unlock()
		return "", errLocalOrganizeInProgress
	}
	organizer, err := newLocalOrganizer()
	if err != nil {
		localOrganizeRunMu.Unlock()
		return "", err
	}
	plan, err := service.GlobalLocalOrganizePlans.Take(planID, owner)
	if err != nil {
		localOrganizeRunMu.Unlock()
		return "", err
	}
	if !validOrganizeIncludes(plan, includedIDs) {
		localOrganizeRunMu.Unlock()
		return "", errInvalidOrganizeSelection
	}
	localOrganizeRunning = true
	localOrganizeRunMu.Unlock()

	taskID := "local-organize-" + shortOrganizePlanID(plan.PlanID)
	taskstate.Global.Start(taskID, "organize", "整理本地番剧", "正在复核文件并准备整理")
	go runLocalOrganizeTask(taskID, organizer, plan, includedIDs)
	return taskID, nil
}

func runLocalOrganizeTask(taskID string, organizer *service.LocalOrganizer, plan *service.LocalOrganizePreview, included []uint) {
	defer func() {
		localOrganizeRunMu.Lock()
		localOrganizeRunning = false
		localOrganizeRunMu.Unlock()
	}()
	result, err := organizer.Execute(context.Background(), plan, included, func(message string, current, total int64) {
		taskstate.Global.Progress(taskID, message, current, total)
	})
	if err != nil {
		taskstate.Global.Fail(taskID, err)
		return
	}

	summary := fmt.Sprintf("整理完成：移动 %d 项，跳过 %d 项，失败 %d 项", result.Moved, result.Skipped+result.Unchanged, result.Failed)
	if result.Moved > 0 {
		current, _ := taskstate.Global.Get(taskID)
		taskstate.Global.Progress(taskID, "文件整理完成，正在重新扫描本地媒体", current.Total, current.Total)
		if scanErr := service.NewScannerService().ScanAll(); scanErr != nil {
			taskstate.Global.Fail(taskID, fmt.Errorf("%s；重新扫描失败: %w", summary, scanErr))
			return
		}
		if refreshErr := service.RequestJellyfinLibraryRefresh(context.Background()); refreshErr != nil && !errors.Is(refreshErr, service.ErrJellyfinNotConfigured) {
			log.Printf("Local organize Jellyfin refresh failed: %v", refreshErr)
			summary += "；Jellyfin 刷新请求失败，可稍后手动刷新"
		}
	}
	taskstate.Global.Complete(taskID, summary)
}

func localOrganizeOwner(c *gin.Context) string {
	if id, err := currentSessionUserID(c); err == nil && id != 0 {
		return strconv.FormatUint(uint64(id), 10)
	}
	return "authenticated"
}

func validOrganizeIncludes(plan *service.LocalOrganizePreview, included []uint) bool {
	if plan == nil {
		return false
	}
	if len(included) == 0 {
		return len(plan.Items) > 0
	}
	allowed := make(map[uint]struct{}, len(plan.Items))
	for _, item := range plan.Items {
		allowed[item.AnimeID] = struct{}{}
	}
	seen := map[uint]struct{}{}
	for _, id := range included {
		if id == 0 {
			return false
		}
		if _, ok := allowed[id]; !ok {
			return false
		}
		seen[id] = struct{}{}
	}
	return len(seen) > 0
}

func shortOrganizePlanID(planID string) string {
	planID = strings.TrimSpace(planID)
	if len(planID) > 12 {
		return planID[:12]
	}
	return planID
}
