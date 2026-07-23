package api

import (
	"bytes"
	"context"
	"errors"
	"image"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
)

func TestRememberCalendarPosterSourcesKeepsOrderedFallbacks(t *testing.T) {
	var day bangumi.CalendarItem
	day.Items = []bangumi.Subject{{
		ID: 99,
		Images: bangumi.Images{
			Large:  "https://lain.bgm.tv/pic/cover/l/99.jpg",
			Common: "https://lain.bgm.tv/pic/cover/c/99.jpg",
			Medium: "https://lain.bgm.tv/pic/cover/c/99.jpg",
		},
	}}

	rememberCalendarPosterSources([]bangumi.CalendarItem{day})
	want := []string{
		"https://lain.bgm.tv/pic/cover/l/99.jpg",
		"https://lain.bgm.tv/pic/cover/c/99.jpg",
	}
	if got := calendarPosterSourcesFor(99); !reflect.DeepEqual(got, want) {
		t.Fatalf("sources = %#v, want %#v", got, want)
	}
}

func TestValidateBangumiPosterURLRejectsUntrustedTargets(t *testing.T) {
	if _, err := validateBangumiPosterURL("https://lain.bgm.tv/pic/cover/l/99.jpg"); err != nil {
		t.Fatalf("expected official poster URL to pass: %v", err)
	}
	for _, rawURL := range []string{
		"http://lain.bgm.tv/pic/cover/l/99.jpg",
		"https://example.com/pic/cover/l/99.jpg",
		"https://lain.bgm.tv.evil.example/pic/cover/l/99.jpg",
		"https://user:pass@lain.bgm.tv/pic/cover/l/99.jpg",
	} {
		if _, err := validateBangumiPosterURL(rawURL); err == nil {
			t.Fatalf("expected %q to be rejected", rawURL)
		}
	}
}

func TestV1CalendarPosterHandlerUsesFallbackAndThumbnailCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var day bangumi.CalendarItem
	day.Items = []bangumi.Subject{{
		ID: 99,
		Images: bangumi.Images{
			Large:  "https://lain.bgm.tv/pic/cover/l/99.jpg",
			Common: "https://lain.bgm.tv/pic/cover/c/99.jpg",
		},
	}}
	rememberCalendarPosterSources([]bangumi.CalendarItem{day})

	originalLoader := loadCalendarPosterImage
	var loaded []string
	loadCalendarPosterImage = func(_ context.Context, source string) ([]byte, error) {
		loaded = append(loaded, source)
		if len(loaded) == 1 {
			return nil, errors.New("large image unavailable")
		}
		return testPosterPNG(t, 600, 900), nil
	}
	t.Cleanup(func() { loadCalendarPosterImage = originalLoader })

	router := gin.New()
	router.GET("/api/v1/calendar/posters/:id", V1CalendarPosterHandler)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/calendar/posters/99?width=160", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded sources = %#v, want large then common", loaded)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(recorder.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if format != "jpeg" || config.Width != 160 || config.Height != 240 {
		t.Fatalf("thumbnail = %s %dx%d, want jpeg 160x240", format, config.Width, config.Height)
	}
}
