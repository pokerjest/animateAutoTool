package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

func TestV1SubscriptionResourcesHandlerReturnsLedgerAndSummary(t *testing.T) {
	sub := model.Subscription{
		Title:    "Resource API Show",
		RSSUrl:   "https://example.test/resource-api",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DB.Unscoped().Where("subscription_id = ?", sub.ID).Delete(&model.SubscriptionResource{}).Error
		_ = db.DB.Unscoped().Delete(&model.Subscription{}, sub.ID).Error
	})
	if err := db.DB.Create(&[]model.SubscriptionResource{
		{
			SubscriptionID: sub.ID,
			CanonicalKey:   "episode:1:1",
			Fingerprint:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Title:          "Resource API Show - 01",
			Episode:        "01",
			SeasonVal:      "S01",
			State:          service.SubscriptionResourceStateCompleted,
			Selected:       true,
			Current:        true,
		},
		{
			SubscriptionID: sub.ID,
			CanonicalKey:   "episode:1:1",
			Fingerprint:    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Title:          "Resource API Show - 01 [V2]",
			Episode:        "01",
			SeasonVal:      "S01",
			VersionTag:     "V2",
			State:          service.SubscriptionResourceStateSuperseded,
			Selected:       false,
			Current:        true,
		},
		{
			SubscriptionID: sub.ID,
			CanonicalKey:   "episode:1:2",
			Fingerprint:    "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			Title:          "Resource API Show - 02",
			Episode:        "02",
			SeasonVal:      "S01",
			State:          service.SubscriptionResourceStateFailed,
			Selected:       true,
			Current:        true,
		},
	}).Error; err != nil {
		t.Fatalf("create resources: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/subscriptions/:id/resources", V1SubscriptionResourcesHandler)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/subscriptions/"+itoa(sub.ID)+"/resources", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected response status %d: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, expected := range []string{`"rss_count":3`, `"canonical_episode_count":2`, `"completed_count":1`, `"failed_count":1`, `"needs_attention":true`} {
		if !containsJSONFragment(body, expected) {
			t.Fatalf("expected %s in response: %s", expected, body)
		}
	}
}

func TestV1SubscriptionResourceActionRejectsCrossSubscriptionResource(t *testing.T) {
	first := model.Subscription{Title: "First Resource API Show", RSSUrl: "https://example.test/resource-api-first", IsActive: true}
	second := model.Subscription{Title: "Second Resource API Show", RSSUrl: "https://example.test/resource-api-second", IsActive: true}
	if err := db.DB.Create(&[]model.Subscription{first, second}).Error; err != nil {
		t.Fatalf("create subscriptions: %v", err)
	}
	resource := model.SubscriptionResource{
		SubscriptionID: second.ID,
		CanonicalKey:   "episode:1:1",
		Fingerprint:    "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		Title:          "Second Resource API Show - 01",
		Episode:        "01",
		SeasonVal:      "S01",
		State:          service.SubscriptionResourceStateFailed,
		Selected:       true,
	}
	if err := db.DB.Create(&resource).Error; err != nil {
		t.Fatalf("create resource: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DB.Unscoped().Delete(&resource).Error
		_ = db.DB.Unscoped().Delete(&[]model.Subscription{first, second}).Error
	})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/subscriptions/:id/resources/:resource_id/:action", V1SubscriptionResourceActionHandler)
	recorder := httptest.NewRecorder()
	path := "/subscriptions/" + itoa(first.ID) + "/resources/" + itoa(resource.ID) + "/retry"
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected cross-subscription resource to be rejected with 404, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestResetResourceForExplicitUpgradeSelectsOnlyRequestedCandidate(t *testing.T) {
	if err := db.DB.Exec("DELETE FROM download_logs").Error; err != nil {
		t.Fatalf("clear download logs: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM subscription_resources").Error; err != nil {
		t.Fatalf("clear resources: %v", err)
	}
	sub := model.Subscription{
		Title:    "Upgrade Handler Show",
		RSSUrl:   "https://example.test/upgrade-handler",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DB.Unscoped().Where("subscription_id = ?", sub.ID).Delete(&model.DownloadLog{}).Error
		_ = db.DB.Unscoped().Where("subscription_id = ?", sub.ID).Delete(&model.SubscriptionResource{}).Error
		_ = db.DB.Unscoped().Delete(&model.Subscription{}, sub.ID).Error
	})
	v1 := model.SubscriptionResource{
		SubscriptionID: sub.ID,
		CanonicalKey:   "episode:1:1",
		Fingerprint:    "1111111111111111111111111111111111111111111111111111111111111111",
		Title:          "Upgrade Handler Show - 01",
		Episode:        "1",
		SeasonVal:      "S1",
		VersionTag:     "V1",
		State:          service.SubscriptionResourceStateCompleted,
		Selected:       true,
	}
	v2 := model.SubscriptionResource{
		SubscriptionID: sub.ID,
		CanonicalKey:   "episode:1:1",
		Fingerprint:    "2222222222222222222222222222222222222222222222222222222222222222",
		Title:          "Upgrade Handler Show - 01 [V2]",
		Episode:        "1",
		SeasonVal:      "S1",
		VersionTag:     "V2",
		State:          service.SubscriptionResourceStateSuperseded,
	}
	if err := db.DB.Create(&v1).Error; err != nil {
		t.Fatalf("create V1: %v", err)
	}
	if err := db.DB.Create(&v2).Error; err != nil {
		t.Fatalf("create V2: %v", err)
	}
	logEntry := model.DownloadLog{
		SubscriptionID: sub.ID,
		ResourceID:     &v1.ID,
		Title:          v1.Title,
		Episode:        "1",
		SeasonVal:      "S1",
		Status:         "completed",
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("create compatibility log: %v", err)
	}

	if err := resetResourceForExplicitAction(&v2, true); err != nil {
		t.Fatalf("prepare explicit upgrade: %v", err)
	}
	var gotV1, gotV2 model.SubscriptionResource
	if err := db.DB.First(&gotV1, v1.ID).Error; err != nil {
		t.Fatalf("reload V1: %v", err)
	}
	if err := db.DB.First(&gotV2, v2.ID).Error; err != nil {
		t.Fatalf("reload V2: %v", err)
	}
	if gotV1.State != service.SubscriptionResourceStateSuperseded || gotV1.Selected {
		t.Fatalf("expected V1 to be retained as superseded, got %+v", gotV1)
	}
	if gotV2.State != service.SubscriptionResourceStateSeen || !gotV2.Selected {
		t.Fatalf("expected V2 to be selected for explicit submission, got %+v", gotV2)
	}
	var gotLog model.DownloadLog
	if err := db.DB.First(&gotLog, logEntry.ID).Error; err != nil {
		t.Fatalf("reload compatibility log: %v", err)
	}
	if gotLog.Status != "archived" {
		t.Fatalf("expected old compatibility log to be archived, got %q", gotLog.Status)
	}
}

func itoa(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}

func containsJSONFragment(body, fragment string) bool {
	return strings.Contains(body, fragment)
}
