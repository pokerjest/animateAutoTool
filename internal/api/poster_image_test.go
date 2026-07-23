package api

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func testPosterPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 180, A: 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		t.Fatalf("encode test poster: %v", err)
	}
	return output.Bytes()
}

func TestServePosterImageThumbnailAndConditionalCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := testPosterPNG(t, 900, 1350)
	router := gin.New()
	router.GET("/poster", func(c *gin.Context) { servePosterImage(c, original) })

	first := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodGet, "/poster?width=320&v=123", nil)
	router.ServeHTTP(first, firstRequest)
	if first.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", first.Code)
	}
	if got := first.Header().Get("Content-Type"); !strings.HasPrefix(got, "image/jpeg") {
		t.Fatalf("expected JPEG thumbnail, got %q", got)
	}
	if got := first.Header().Get("Cache-Control"); got != versionedPosterCacheControl {
		t.Fatalf("expected immutable cache policy, got %q", got)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(first.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	if format != "jpeg" || config.Width != 320 || config.Height != 480 {
		t.Fatalf("unexpected thumbnail: format=%s size=%dx%d", format, config.Width, config.Height)
	}

	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag")
	}
	second := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodGet, "/poster?width=320&v=123", nil)
	secondRequest.Header.Set("If-None-Match", etag)
	router.ServeHTTP(second, secondRequest)
	if second.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Fatalf("expected empty 304 body, got %d bytes", second.Body.Len())
	}
}

func TestServePosterImageKeepsOriginalWithoutWidth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := testPosterPNG(t, 20, 30)
	router := gin.New()
	router.GET("/poster", func(c *gin.Context) { servePosterImage(c, original) })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/poster", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "image/png") {
		t.Fatalf("expected original PNG, got %q", got)
	}
	if !bytes.Equal(recorder.Body.Bytes(), original) {
		t.Fatal("expected original image bytes")
	}
	if got := recorder.Header().Get("Cache-Control"); got != defaultPosterCacheControl {
		t.Fatalf("expected revalidating cache policy, got %q", got)
	}
}

func TestParsePosterWidthBounds(t *testing.T) {
	if got := parsePosterWidth("12"); got != 64 {
		t.Fatalf("expected minimum 64, got %d", got)
	}
	if got := parsePosterWidth("9999"); got != maxPosterThumbnailWidth {
		t.Fatalf("expected maximum %d, got %d", maxPosterThumbnailWidth, got)
	}
	if got := parsePosterWidth("invalid"); got != 0 {
		t.Fatalf("expected invalid width to disable resize, got %d", got)
	}
}
