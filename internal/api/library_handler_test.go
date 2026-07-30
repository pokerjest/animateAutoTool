package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

func TestDeduplicateLibraryMetadataMatchesCatalogIdentityRules(t *testing.T) {
	metadata := []model.AnimeMetadata{
		{Model: gorm.Model{ID: 1}, BangumiID: 100, Title: "同一番剧"},
		{Model: gorm.Model{ID: 2}, BangumiID: 100, Title: "同一番剧（重复 ID）"},
		{Model: gorm.Model{ID: 3}, BangumiID: 200, Title: "同一番剧"},
		{Model: gorm.Model{ID: 4}, BangumiID: 0, Title: "另一番剧"},
	}

	items := deduplicateLibraryMetadata(metadata)
	if len(items) != 2 {
		t.Fatalf("expected 2 catalog items after deduplication, got %d: %+v", len(items), items)
	}
	if items[0].ID != 1 || items[1].ID != 4 {
		t.Fatalf("unexpected catalog identity order: %+v", items)
	}
}

func TestBangumiMetadataSearchResultsPreservesEveryMatch(t *testing.T) {
	results := []bangumi.SearchResult{
		{ID: 101, Name: "First", NameCN: "第一部"},
		{ID: 202, Name: "Second", NameCN: "第二部"},
	}
	results[0].Images.Large = "https://example.test/first.jpg"

	items := bangumiMetadataSearchResults(results)
	if len(items) != 2 {
		t.Fatalf("expected two search results, got %d", len(items))
	}
	if items[0].ID != 101 || items[0].NameCN != "第一部" || items[0].Images.Large != "https://example.test/first.jpg" {
		t.Fatalf("unexpected first search result: %+v", items[0])
	}
	if items[1].ID != 202 || items[1].NameCN != "第二部" {
		t.Fatalf("unexpected second search result: %+v", items[1])
	}
}

func TestGetRandomBackgroundHandlerUsesAniListImageColumn(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	meta := model.AnimeMetadata{
		BangumiID:       999999,
		AniListID:       888888,
		Title:           "AniList Only",
		AniListTitle:    "AniList Only",
		AniListImageRaw: []byte("fake-image"),
	}
	if err := db.DB.Create(&meta).Error; err != nil {
		t.Fatalf("failed to seed anime metadata: %v", err)
	}

	t.Cleanup(func() {
		_ = db.DB.Unscoped().Delete(&model.AnimeMetadata{}, meta.ID).Error
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/ui/background/random", nil)
	markLocalRequest(req)
	cookie, _ := loginCookie(t, r, "admin")
	req.Header.Set("Cookie", cookie)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var payload struct {
		Success bool   `json:"success"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !payload.Success {
		t.Fatalf("expected success response, got body %s", w.Body.String())
	}
	expected := "/api/v1/posters/" + strconv.FormatUint(uint64(meta.ID), 10) + "?source=anilist"
	if payload.URL != expected {
		t.Fatalf("expected poster url %q, got %q", expected, payload.URL)
	}

	v1Recorder := httptest.NewRecorder()
	v1Request, _ := http.NewRequest("GET", "/api/v1/ui/background/random", nil)
	markLocalRequest(v1Request)
	v1Request.Header.Set("Cookie", cookie)
	r.ServeHTTP(v1Recorder, v1Request)

	if v1Recorder.Code != http.StatusOK {
		t.Fatalf("expected v1 status 200, got %d: %s", v1Recorder.Code, v1Recorder.Body.String())
	}

	var v1Payload struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(v1Recorder.Body.Bytes(), &v1Payload); err != nil {
		t.Fatalf("failed to decode v1 response: %v", err)
	}
	if v1Payload.Data.URL != expected {
		t.Fatalf("expected v1 poster url %q, got %q", expected, v1Payload.Data.URL)
	}
}
