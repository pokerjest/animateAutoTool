package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

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
}
