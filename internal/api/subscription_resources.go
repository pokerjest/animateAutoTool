package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"gorm.io/gorm"
)

func V1SubscriptionResourcesHandler(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		v1Error(c, http.StatusBadRequest, "invalid_id", "订阅 ID 无效")
		return
	}
	sub, err := subscriptionByID(uint(id))
	if err != nil {
		v1Error(c, http.StatusNotFound, "subscription_not_found", "未找到对应订阅")
		return
	}
	var resources []model.SubscriptionResource
	if db.DB == nil {
		v1Error(c, http.StatusInternalServerError, "database_unavailable", "数据库未初始化")
		return
	}
	if err := db.DB.Where("subscription_id = ?", sub.ID).
		Order("season_val ASC, episode ASC, selected DESC, candidate_rank ASC, id ASC").
		Limit(500).
		Find(&resources).Error; err != nil {
		v1Error(c, http.StatusInternalServerError, "resources_load_failed", err.Error())
		return
	}
	rss, canonical, confirmed, downloading, completed, failed, unresolved := service.ResourceSummary(resources)
	needsAttention := false
	for _, resource := range resources {
		if service.ResourceNeedsAttention(resource) {
			needsAttention = true
			break
		}
	}
	v1Data(c, http.StatusOK, gin.H{
		"subscription": sub,
		"items":        resources,
		"summary": gin.H{
			"rss_count":               rss,
			"canonical_episode_count": canonical,
			"confirmed_count":         confirmed,
			"downloading_count":       downloading,
			"completed_count":         completed,
			"failed_count":            failed,
			"unresolved_count":        unresolved,
			"needs_attention":         needsAttention,
		},
	})
}

func V1SubscriptionResourceActionHandler(c *gin.Context) {
	action := strings.TrimSpace(strings.ToLower(c.Param("action")))
	if action != "retry" && action != "upgrade" {
		v1Error(c, http.StatusBadRequest, "invalid_resource_action", "不支持的资源操作")
		return
	}
	subscriptionID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		v1Error(c, http.StatusBadRequest, "invalid_id", "订阅 ID 无效")
		return
	}
	resourceID, err := strconv.ParseUint(c.Param("resource_id"), 10, 32)
	if err != nil {
		v1Error(c, http.StatusBadRequest, "invalid_resource_id", "资源 ID 无效")
		return
	}
	sub, err := subscriptionByID(uint(subscriptionID))
	if err != nil {
		v1Error(c, http.StatusNotFound, "subscription_not_found", "未找到对应订阅")
		return
	}
	var resource model.SubscriptionResource
	if db.DB == nil || db.DB.Where("id = ? AND subscription_id = ?", resourceID, sub.ID).First(&resource).Error != nil {
		v1Error(c, http.StatusNotFound, "resource_not_found", "未找到对应资源")
		return
	}

	switch action {
	case "retry":
		switch resource.State {
		case service.SubscriptionResourceStateFailed,
			service.SubscriptionResourceStateUnknown,
			service.SubscriptionResourceStateUnresolved,
			service.SubscriptionResourceStateSuperseded:
		default:
			v1Error(c, http.StatusConflict, "resource_not_retryable", "当前资源无需重试；如需更换版本请使用升级")
			return
		}
		if err := resetResourceForExplicitAction(&resource, false); err != nil {
			v1Error(c, http.StatusInternalServerError, "resource_retry_prepare_failed", err.Error())
			return
		}
	case "upgrade":
		if resource.CanonicalKey == "" {
			v1Error(c, http.StatusConflict, "resource_not_upgradeable", "该资源没有稳定的集数身份")
			return
		}
		var active []model.SubscriptionResource
		if err := db.DB.Where("subscription_id = ? AND canonical_key = ? AND id <> ? AND state IN ?",
			sub.ID, resource.CanonicalKey, resource.ID,
			[]string{service.SubscriptionResourceStateDownloading, service.SubscriptionResourceStatePending}).Find(&active).Error; err != nil {
			v1Error(c, http.StatusInternalServerError, "resource_upgrade_check_failed", err.Error())
			return
		}
		if len(active) > 0 {
			v1Error(c, http.StatusConflict, "resource_upgrade_busy", "同集已有下载任务，请先等待完成或手动处理")
			return
		}
		if err := resetResourceForExplicitAction(&resource, true); err != nil {
			v1Error(c, http.StatusInternalServerError, "resource_upgrade_prepare_failed", err.Error())
			return
		}
	}

	if err := runSubscriptionCheck(sub, "manual"); err != nil {
		v1Error(c, http.StatusBadGateway, "resource_action_failed", fmt.Sprintf("qBittorrent 检查失败: %v", err))
		return
	}
	updated, err := loadSubscriptionCard(sub.ID)
	if err != nil {
		v1Error(c, http.StatusInternalServerError, "subscription_reload_failed", err.Error())
		return
	}
	v1Message(c, http.StatusOK, map[string]string{
		"retry":   "资源已加入重试检查",
		"upgrade": "已切换到该候选版本并重新检查",
	}[action], updated)
}

func resetResourceForExplicitAction(resource *model.SubscriptionResource, upgrade bool) error {
	if resource == nil || db.DB == nil {
		return gorm.ErrInvalidDB
	}
	now := time.Now().UTC()
	rs := store.NewSubscriptionResourceStore(db.DB)
	resourceIDs := []uint{resource.ID}
	if upgrade {
		var siblings []model.SubscriptionResource
		if err := db.DB.Where("subscription_id = ? AND canonical_key = ?", resource.SubscriptionID, resource.CanonicalKey).
			Find(&siblings).Error; err != nil {
			return err
		}
		for _, sibling := range siblings {
			resourceIDs = append(resourceIDs, sibling.ID)
		}
		if err := db.DB.Model(&model.SubscriptionResource{}).
			Where("subscription_id = ? AND canonical_key = ? AND id <> ?", resource.SubscriptionID, resource.CanonicalKey, resource.ID).
			Updates(map[string]any{
				"state":        service.SubscriptionResourceStateSuperseded,
				"state_reason": "用户显式选择其他版本",
				"selected":     false,
			}).Error; err != nil {
			return err
		}
	}
	if err := rs.UpdateByID(resource.ID, map[string]any{
		"state":           service.SubscriptionResourceStateSeen,
		"state_reason":    "用户显式确认后重新提交",
		"selected":        true,
		"last_error":      "",
		"retry_after":     nil,
		"last_attempt_at": nil,
		"submitted_at":    nil,
		"completed_at":    nil,
		"last_seen_at":    &now,
	}); err != nil {
		return err
	}
	// Existing log rows remain for audit, but are archived out of the
	// compatibility duplicate projection so the explicit action can submit
	// exactly once on the next manager pass.
	query := db.DB.Model(&model.DownloadLog{}).
		Where("subscription_id = ?", resource.SubscriptionID)
	if upgrade {
		query = query.Where(
			"resource_id IN ? OR (season_val = ? AND episode = ?)",
			resourceIDs, resource.SeasonVal, resource.Episode,
		)
	} else {
		query = query.Where("resource_id = ? AND status IN ?", resource.ID, []string{"failed", "archived"})
	}
	return query.Update("status", "archived").Error
}
