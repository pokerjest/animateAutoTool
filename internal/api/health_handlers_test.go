package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestHealthPageAndReportHandlers(t *testing.T) {
	resetAuthFixtures(t)

	if err := db.DB.Exec("DELETE FROM subscriptions").Error; err != nil {
		t.Fatalf("failed to clear subscriptions: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM download_logs").Error; err != nil {
		t.Fatalf("failed to clear download logs: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM local_animes").Error; err != nil {
		t.Fatalf("failed to clear local_animes: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM local_episodes").Error; err != nil {
		t.Fatalf("failed to clear local_episodes: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM library_issues").Error; err != nil {
		t.Fatalf("failed to clear library_issues: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM global_configs").Error; err != nil {
		t.Fatalf("failed to clear global_configs: %v", err)
	}

	seedConfigs := []model.GlobalConfig{
		{Key: model.ConfigKeyJellyfinUrl, Value: "http://jellyfin.test"},
		{Key: model.ConfigKeyJellyfinApiKey, Value: "secret"},
		{Key: model.ConfigKeyQBUrl, Value: "http://127.0.0.1:7603"},
	}
	for _, item := range seedConfigs {
		if err := db.DB.Create(&item).Error; err != nil {
			t.Fatalf("failed to seed config %s: %v", item.Key, err)
		}
	}

	lastSuccessAt := time.Now().Add(-96 * time.Hour)
	sub := model.Subscription{
		Title:             "Health Demo",
		RSSUrl:            "https://example.test/rss",
		IsActive:          true,
		StaleAfterHours:   24,
		LastSuccessAt:     &lastSuccessAt,
		ExpectedEpisodes:  12,
		AutoDisableOnDone: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}
	if err := db.DB.Create(&model.DownloadLog{SubscriptionID: sub.ID, Title: "Health Demo - 01", Status: "downloading"}).Error; err != nil {
		t.Fatalf("failed to create downloading log: %v", err)
	}
	if err := db.DB.Create(&model.DownloadLog{SubscriptionID: sub.ID, Title: "Health Demo - 02", Status: "failed"}).Error; err != nil {
		t.Fatalf("failed to create failed log: %v", err)
	}
	localAnime := model.LocalAnime{Title: "Health Demo"}
	if err := db.DB.Create(&localAnime).Error; err != nil {
		t.Fatalf("failed to create local anime: %v", err)
	}
	localAnimeID := localAnime.ID
	if err := db.DB.Create(&model.LibraryIssue{LocalAnimeID: &localAnimeID, IssueType: "missing_cover", Status: "open"}).Error; err != nil {
		t.Fatalf("failed to create library issue: %v", err)
	}

	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")

	pageRecorder := httptest.NewRecorder()
	pageReq, _ := http.NewRequest("GET", "/health", nil)
	pageReq.Header.Set("Cookie", cookie)
	markLocalRequest(pageReq)
	r.ServeHTTP(pageRecorder, pageReq)
	assert.Equal(t, http.StatusOK, pageRecorder.Code)
	assert.Contains(t, pageRecorder.Body.String(), "系统健康")
	assert.Contains(t, pageRecorder.Body.String(), "下载链路仍有阻塞或失败记录")

	partialRecorder := httptest.NewRecorder()
	partialReq, _ := http.NewRequest("GET", "/health", nil)
	partialReq.Header.Set("HX-Request", "true")
	partialReq.Header.Set("Cookie", cookie)
	markLocalRequest(partialReq)
	r.ServeHTTP(partialRecorder, partialReq)
	assert.Equal(t, http.StatusOK, partialRecorder.Code)
	assert.Contains(t, partialRecorder.Body.String(), "系统健康")
	assert.NotContains(t, partialRecorder.Body.String(), "🚪 退出登录")
	assert.NotContains(t, partialRecorder.Body.String(), "AnimateTool")

	reportRecorder := httptest.NewRecorder()
	reportReq, _ := http.NewRequest("GET", "/api/health/report", nil)
	reportReq.Header.Set("Cookie", cookie)
	markLocalRequest(reportReq)
	r.ServeHTTP(reportRecorder, reportReq)
	assert.Equal(t, http.StatusOK, reportRecorder.Code)

	var report HealthReport
	if err := json.Unmarshal(reportRecorder.Body.Bytes(), &report); err != nil {
		t.Fatalf("failed to decode report: %v", err)
	}
	assert.Equal(t, int64(1), report.SubscriptionTotal)
	assert.Equal(t, int64(1), report.OpenLibraryIssues)
	assert.Equal(t, int64(1), report.DownloadDownloading)
	assert.Equal(t, int64(1), report.DownloadFailed)
	assert.Equal(t, "rose", report.HealthTone)
	assert.NotEmpty(t, report.Recommendations)
}
