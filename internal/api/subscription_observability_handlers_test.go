package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/scheduler"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/stretchr/testify/assert"
)

func TestSubscriptionCardEndpointReturnsLatestRunState(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	sub := model.Subscription{
		Title:               "Observable Show",
		RSSUrl:              "https://example.test/observable",
		IsActive:            true,
		LastRunStatus:       "warning",
		LastRunSummary:      "新增 1 集，另有 1 集加入下载失败",
		LastError:           "Episode 02: qb offline",
		LastNewDownloads:    1,
		LastDownloadedTitle: "[Group] Observable Show - 01",
	}
	now := time.Now()
	sub.LastCheckAt = &now
	sub.LastSuccessAt = &now
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/subscriptions/%d/card", sub.ID), nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected card endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "新增 1 集，另有 1 集加入下载失败")
	assert.Contains(t, w.Body.String(), "Episode 02: qb offline")
	assert.Contains(t, w.Body.String(), "[Group] Observable Show - 01")
	assert.Contains(t, w.Body.String(), `data-title="Observable Show"`)
	assert.Contains(t, w.Body.String(), `data-status-label="部分失败"`)
}

func TestNormalizeSubscriptionRunSummaryClarifiesLegacyCounts(t *testing.T) {
	tests := map[string]string{
		"检查到 3 集，但都已经在下载记录中":                       "本次 RSS 返回 3 条资源，均已存在于历史下载记录",
		"检查到 3 集，但都已经在下载记录中；主 RSS 暂时不可用，已使用备用 RSS": "本次 RSS 返回 3 条资源，均已存在于历史下载记录；主 RSS 暂时不可用，已使用备用 RSS",
		"检查到 4 集，但都被过滤规则跳过":                        "本次 RSS 返回 4 条资源，均被过滤规则跳过",
		"未发现新剧集（过滤 2，已存在 3）":                       "本次 RSS 返回 5 条资源（过滤 2，已存在 3），未发现新增",
		"新增 1 集待下载": "新增 1 集待下载",
	}

	for input, want := range tests {
		if got := normalizeSubscriptionRunSummary(input); got != want {
			t.Fatalf("normalizeSubscriptionRunSummary(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSchedulerStatusEndpointRendersLatestSummary(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	scheduler.GlobalRunStatus.Skip("", "")
	t.Cleanup(func() {
		scheduler.GlobalRunStatus.Skip("", "")
	})

	scheduler.GlobalRunStatus.Begin("auto", 4)
	scheduler.GlobalRunStatus.Finish(2, 1, 1, 4, "auto", "qb unavailable")

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/subscriptions/scheduler-status", nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected scheduler status endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "最近一轮共检查 4 个订阅")
	assert.Contains(t, w.Body.String(), "qb unavailable")
}

func TestSubscriptionTrendsEndpointRendersRecentLeaders(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	now := time.Now()
	recentCheck := now.Add(-2 * time.Hour)

	subs := []model.Subscription{
		{
			Title:            "Stable Show",
			RSSUrl:           "https://example.test/stable",
			IsActive:         true,
			LastRunStatus:    "success",
			LastRunSummary:   "新增 3 集待下载",
			LastNewDownloads: 3,
			LastCheckAt:      &recentCheck,
			LastSuccessAt:    &recentCheck,
		},
		{
			Title:          "Flaky Show",
			RSSUrl:         "https://example.test/flaky",
			IsActive:       true,
			LastRunStatus:  "error",
			LastRunSummary: "RSS 解析失败",
			LastError:      "rss unavailable",
			LastCheckAt:    &recentCheck,
		},
	}
	for i := range subs {
		if err := db.DB.Create(&subs[i]).Error; err != nil {
			t.Fatalf("failed to create subscription %s: %v", subs[i].Title, err)
		}
	}

	logs := []model.DownloadLog{
		{SubscriptionID: subs[0].ID, Title: "[Group] Stable Show - 01", Status: "completed"},
		{SubscriptionID: subs[0].ID, Title: "[Group] Stable Show - 02", Status: "downloading"},
	}
	if err := db.DB.Create(&logs).Error; err != nil {
		t.Fatalf("failed to seed trend logs: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/subscriptions/trends", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected trends endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "订阅趋势")
	assert.Contains(t, w.Body.String(), "Stable Show")
	assert.Contains(t, w.Body.String(), "Flaky Show")
	assert.Contains(t, w.Body.String(), "最近最不稳")
	assert.Contains(t, w.Body.String(), "最近最活跃")
}

func TestSubscriptionProcessFlowUpdatesTrendEndpoint(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	sub := model.Subscription{
		Title:    "Integrated Show",
		RSSUrl:   "https://example.test/integrated",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	manager := &service.SubscriptionManager{
		RSSParser: integrationRSSParser{
			episodes: []parser.Episode{
				{Title: "[Group] Integrated Show - 01", EpisodeNum: "01", TorrentURL: "magnet:?xt=urn:btih:integrated-1"},
				{Title: "[Group] Integrated Show - 02", EpisodeNum: "02", TorrentURL: "magnet:?xt=urn:btih:integrated-2"},
			},
		},
		Downloader: integrationDownloader{},
		DB:         db.DB,
	}
	manager.ProcessSubscription(&sub)

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/subscriptions/trends", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected trends endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "Integrated Show")
	assert.Contains(t, w.Body.String(), "新增 2 集待下载")
	assert.Contains(t, w.Body.String(), "+2")
}

func TestToggleSubscriptionReturnsUpdatedCard(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	sub := model.Subscription{
		Title:      "Toggle Me",
		RSSUrl:     "https://example.test/toggle",
		IsActive:   true,
		FilterRule: "字幕组A",
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", fmt.Sprintf("/api/subscriptions/%d/toggle", sub.ID), nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected toggle endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), `data-subscription-card="true"`)
	assert.Contains(t, w.Body.String(), `data-active="false"`)
	assert.Contains(t, w.Body.String(), "已暂停")
}

func TestSubscriptionHistoryEndpointRendersRecentRuns(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	now := time.Now()
	sub := model.Subscription{
		Title:          "History Show",
		RSSUrl:         "https://example.test/history",
		IsActive:       true,
		LastRunStatus:  "warning",
		LastRunSummary: "新增 2 集，另有 1 集加入下载失败",
		LastError:      "Episode 03: qb timeout",
	}
	sub.LastCheckAt = &now
	sub.LastSuccessAt = &now
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	logs := []model.DownloadLog{
		{
			SubscriptionID: sub.ID,
			Title:          "[Group] History Show - 02",
			Episode:        "02",
			SeasonVal:      "S01",
			Status:         "downloading",
		},
		{
			SubscriptionID: sub.ID,
			Title:          "[Group] History Show - 01",
			Episode:        "01",
			SeasonVal:      "S01",
			Status:         "completed",
			TargetFile:     "/downloads/history-show/01.mkv",
		},
	}
	if err := db.DB.Create(&logs).Error; err != nil {
		t.Fatalf("failed to create download logs: %v", err)
	}
	runLogs := []model.SubscriptionRunLog{
		{
			SubscriptionID:      sub.ID,
			CheckedAt:           now,
			TriggerSource:       "auto",
			Status:              "warning",
			Summary:             "新增 2 集，另有 1 集加入下载失败",
			Error:               "Episode 03: qb timeout",
			TotalEpisodes:       4,
			FilteredCount:       1,
			DuplicateCount:      0,
			NewDownloads:        2,
			FailedDownloads:     1,
			LastDownloadedTitle: "[Group] History Show - 02",
		},
	}
	if err := db.DB.Create(&runLogs).Error; err != nil {
		t.Fatalf("failed to create subscription run logs: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/subscriptions/%d/history", sub.ID), nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected history endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "新增 2 集，另有 1 集加入下载失败")
	assert.Contains(t, w.Body.String(), "Episode 03: qb timeout")
	assert.Contains(t, w.Body.String(), "逐次运行日志")
	assert.Contains(t, w.Body.String(), "自动调度")
	assert.Contains(t, w.Body.String(), "RSS 4 条")
	assert.Contains(t, w.Body.String(), "[Group] History Show - 02")
	assert.Contains(t, w.Body.String(), "/downloads/history-show/01.mkv")
}
