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
	"time"

	"github.com/gin-gonic/gin"
)

func TestValidateMikanPosterURLRejectsUntrustedTargets(t *testing.T) {
	for _, rawURL := range []string{
		"https://mikanani.me/images/Bangumi/poster.jpg",
		"https://www.mikanani.me/images/Bangumi/poster.jpg",
		"https://mikanime.tv/images/Bangumi/poster.jpg",
		"https://mikanani.kas.pub/images/Bangumi/poster.jpg",
	} {
		if _, err := validateMikanPosterURL(rawURL); err != nil {
			t.Fatalf("expected trusted poster URL %q to pass: %v", rawURL, err)
		}
	}
	for _, rawURL := range []string{
		"http://mikanani.me/images/poster.jpg",
		"https://example.com/images/poster.jpg",
		"https://mikanani.me.evil.example/images/poster.jpg",
		"https://mikanime.tv.evil.example/images/poster.jpg",
		"https://mikanani.kas.pub.evil.example/images/poster.jpg",
		"https://user:pass@mikanani.me/images/poster.jpg",
		"https://mikanani.me:8443/images/poster.jpg",
		"https://mikanani.me/",
	} {
		if _, err := validateMikanPosterURL(rawURL); err == nil {
			t.Fatalf("expected %q to be rejected", rawURL)
		}
	}
}

func TestMikanPosterCandidatesPreservePathAndShareCache(t *testing.T) {
	parsed, err := validateMikanPosterURL("https://mikanani.me/images/Bangumi/poster.jpg?width=400&format=webp#ignored")
	if err != nil {
		t.Fatalf("validate source: %v", err)
	}
	candidates := mikanPosterCandidates(parsed)
	if len(candidates) != len(mikanPosterHosts) {
		t.Fatalf("candidate count = %d, want %d", len(candidates), len(mikanPosterHosts))
	}
	for i, candidate := range candidates {
		if candidate.Hostname() != mikanPosterHosts[i] {
			t.Fatalf("candidate %d host = %q, want %q", i, candidate.Hostname(), mikanPosterHosts[i])
		}
		if candidate.RequestURI() != parsed.RequestURI() {
			t.Fatalf("candidate %d request URI = %q, want %q", i, candidate.RequestURI(), parsed.RequestURI())
		}
		if candidate.Fragment != "" {
			t.Fatalf("candidate %d retained fragment %q", i, candidate.Fragment)
		}
	}

	mirror, err := validateMikanPosterURL("https://mikanani.kas.pub/images/Bangumi/poster.jpg?width=400&format=webp")
	if err != nil {
		t.Fatalf("validate mirror: %v", err)
	}
	if got, want := mikanPosterCacheKey(mirror), mikanPosterCacheKey(parsed); got != want {
		t.Fatalf("mirror cache key = %q, want %q", got, want)
	}
}

func TestRaceMikanPosterCandidatesReturnsFirstSuccessfulImage(t *testing.T) {
	parsed, err := validateMikanPosterURL("https://mikanani.me/images/Bangumi/poster.jpg")
	if err != nil {
		t.Fatalf("validate source: %v", err)
	}
	candidates := mikanPosterCandidates(parsed)
	started := make(chan string, len(candidates))
	release := make(chan struct{})
	type outcome struct {
		data []byte
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		data, raceErr := raceMikanPosterCandidates(context.Background(), candidates, func(ctx context.Context, candidate *url.URL) ([]byte, error) {
			started <- candidate.Hostname()
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}

			var delay time.Duration
			switch candidate.Hostname() {
			case "mikanime.tv":
				delay = 5 * time.Millisecond
			case "mikanani.kas.pub":
				delay = 10 * time.Millisecond
			default:
				delay = 40 * time.Millisecond
			}
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
			}
			if candidate.Hostname() == "mikanime.tv" {
				return nil, errors.New("redirect did not return an image")
			}
			return []byte(candidate.Hostname()), nil
		})
		done <- outcome{data: data, err: raceErr}
	}()

	for range candidates {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for all poster candidates to start")
		}
	}
	close(release)

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("race failed: %v", result.err)
		}
		if got, want := string(result.data), "mikanani.kas.pub"; got != want {
			t.Fatalf("race winner = %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("poster race did not finish")
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
