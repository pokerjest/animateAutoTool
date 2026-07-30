package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
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
		{Model: gorm.Model{ID: 5}, BangumiID: 0, Title: " 另一番剧 "},
	}

	items := deduplicateLibraryMetadata(metadata)
	if len(items) != 3 {
		t.Fatalf("expected 3 catalog items after deduplication, got %d: %+v", len(items), items)
	}
	if items[0].ID != 1 || items[1].ID != 3 || items[2].ID != 4 {
		t.Fatalf("unexpected catalog identity order: %+v", items)
	}
}

func TestV1LibraryHandlerPaginatesSearchesAndFiltersWholeCatalog(t *testing.T) {
	resetAuthFixtures(t)

	metadata := make([]model.AnimeMetadata, 105)
	for i := range metadata {
		metadata[i] = model.AnimeMetadata{
			Title:     fmt.Sprintf("Catalog Show %03d", i+1),
			TitleCN:   fmt.Sprintf("图鉴番剧 %03d", i+1),
			BangumiID: 810000 + i,
		}
	}
	metadata[0].Title = "100% Hero"
	if err := db.DB.CreateInBatches(&metadata, 50).Error; err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	t.Cleanup(func() {
		ids := make([]uint, 0, len(metadata))
		for _, item := range metadata {
			ids = append(ids, item.ID)
		}
		_ = db.DB.Unscoped().Where("id IN ?", ids).Delete(&model.AnimeMetadata{}).Error
	})

	subscription := model.Subscription{
		Title:      metadata[100].Title,
		RSSUrl:     "https://example.test/catalog-pagination",
		MetadataID: &metadata[100].ID,
	}
	if err := db.DB.Create(&subscription).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	local := model.LocalAnime{
		Title:      metadata[101].Title,
		Path:       "/library/catalog-pagination",
		MetadataID: &metadata[101].ID,
	}
	if err := db.DB.Create(&local).Error; err != nil {
		t.Fatalf("seed local anime: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DB.Unscoped().Delete(&model.Subscription{}, subscription.ID).Error
		_ = db.DB.Unscoped().Delete(&model.LocalAnime{}, local.ID).Error
	})

	request := func(rawQuery string) struct {
		Data struct {
			Items []LibraryItem `json:"items"`
		} `json:"data"`
		Meta struct {
			Page     int   `json:"page"`
			PageSize int   `json:"page_size"`
			Total    int64 `json:"total"`
		} `json:"meta"`
	} {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/library?"+rawQuery, nil)
		V1LibraryHandler(c)
		if w.Code != http.StatusOK {
			t.Fatalf("library request failed: %d %s", w.Code, w.Body.String())
		}
		var payload struct {
			Data struct {
				Items []LibraryItem `json:"items"`
			} `json:"data"`
			Meta struct {
				Page     int   `json:"page"`
				PageSize int   `json:"page_size"`
				Total    int64 `json:"total"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode library response: %v", err)
		}
		return payload
	}

	secondPage := request("page=2&page_size=100")
	if secondPage.Meta.Total < 105 || len(secondPage.Data.Items) < 5 {
		t.Fatalf("expected full catalog pagination, got meta=%+v items=%d", secondPage.Meta, len(secondPage.Data.Items))
	}

	search := request("page=1&page_size=20&q=" + url.QueryEscape(metadata[104].TitleCN))
	if search.Meta.Total != 1 || len(search.Data.Items) != 1 || search.Data.Items[0].ID != metadata[104].ID {
		t.Fatalf("expected full-catalog search match, got %+v", search)
	}

	literalWildcard := request("page=1&page_size=20&q=" + url.QueryEscape("%"))
	if literalWildcard.Meta.Total != 1 || len(literalWildcard.Data.Items) != 1 || literalWildcard.Data.Items[0].ID != metadata[0].ID {
		t.Fatalf("expected literal wildcard search match, got %+v", literalWildcard)
	}

	subscribed := request("page=1&page_size=20&status=subscribed&q=" + url.QueryEscape(subscription.Title))
	if subscribed.Meta.Total != 1 || len(subscribed.Data.Items) != 1 || !subscribed.Data.Items[0].IsSubscribed {
		t.Fatalf("expected subscribed filter match, got %+v", subscribed)
	}

	localOnly := request("page=1&page_size=20&status=local&q=" + url.QueryEscape(local.Title))
	if localOnly.Meta.Total != 1 || len(localOnly.Data.Items) != 1 || !localOnly.Data.Items[0].IsLocal {
		t.Fatalf("expected local filter match, got %+v", localOnly)
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
