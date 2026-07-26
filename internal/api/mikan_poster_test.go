package api

import (
	"bytes"
	"context"
	"errors"
	"image"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestValidateMikanPosterURLRejectsUntrustedTargets(t *testing.T) {
	for _, rawURL := range []string{
		"https://mikanani.me/images/Bangumi/poster.jpg",
		"https://www.mikanani.me/images/Bangumi/poster.jpg",
	} {
		if _, err := validateMikanPosterURL(rawURL); err != nil {
			t.Fatalf("expected official poster URL %q to pass: %v", rawURL, err)
		}
	}
	for _, rawURL := range []string{
		"http://mikanani.me/images/poster.jpg",
		"https://example.com/images/poster.jpg",
		"https://mikanani.me.evil.example/images/poster.jpg",
		"https://user:pass@mikanani.me/images/poster.jpg",
		"https://mikanani.me:8443/images/poster.jpg",
		"https://mikanani.me/",
	} {
		if _, err := validateMikanPosterURL(rawURL); err == nil {
			t.Fatalf("expected %q to be rejected", rawURL)
		}
	}
}

func TestV1MikanPosterHandlerReturnsSameOriginThumbnail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalLoader := loadMikanPosterImage
	var loaded string
	loadMikanPosterImage = func(_ context.Context, source string) ([]byte, error) {
		loaded = source
		return testPosterPNG(t, 600, 900), nil
	}
	t.Cleanup(func() { loadMikanPosterImage = originalLoader })

	source := "https://mikanani.me/images/Bangumi/poster.jpg"
	router := gin.New()
	router.GET("/api/v1/subscriptions/mikan/poster", V1MikanPosterHandler)
	recorder := httptest.NewRecorder()
	requestURL := "/api/v1/subscriptions/mikan/poster?width=160&url=" + url.QueryEscape(source)
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, requestURL, nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if loaded != source {
		t.Fatalf("loaded source = %q, want %q", loaded, source)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(recorder.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if format != testPosterJPEGFormat || config.Width != 160 || config.Height != 240 {
		t.Fatalf("thumbnail = %s %dx%d, want jpeg 160x240", format, config.Width, config.Height)
	}
}

func TestV1MikanPosterHandlerRejectsInvalidAndFailedSources(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/poster", V1MikanPosterHandler)

	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/poster?url="+url.QueryEscape("https://example.com/poster.jpg"), nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid source status = %d, want 400", invalid.Code)
	}

	originalLoader := loadMikanPosterImage
	loadMikanPosterImage = func(context.Context, string) ([]byte, error) {
		return nil, errors.New("upstream timeout")
	}
	t.Cleanup(func() { loadMikanPosterImage = originalLoader })

	failed := httptest.NewRecorder()
	router.ServeHTTP(failed, httptest.NewRequest(http.MethodGet, "/poster?url="+url.QueryEscape("https://mikanani.me/images/poster.jpg"), nil))
	if failed.Code != http.StatusBadGateway {
		t.Fatalf("failed source status = %d, want 502", failed.Code)
	}
}
