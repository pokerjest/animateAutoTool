package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/httpx"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
	"golang.org/x/sync/singleflight"
)

const maxMikanPosterBytes = 12 << 20

var (
	mikanPosterOriginals = calendarPosterImageCache{entries: make(map[string][]byte)}
	mikanPosterFetches   singleflight.Group
	mikanPosterSlots     = make(chan struct{}, 4)
	loadMikanPosterImage = fetchMikanPosterImage
)

// V1MikanPosterHandler serves Mikan covers through AnimateTool so remote
// browsers do not need direct access to mikanani.me or the user's proxy.
func V1MikanPosterHandler(c *gin.Context) {
	rawURL := strings.TrimSpace(c.Query("url"))
	if _, err := validateMikanPosterURL(rawURL); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	data, err := loadMikanPosterImage(c.Request.Context(), rawURL)
	if err != nil {
		log.Printf("Mikan poster unavailable: %v", err)
		c.Status(http.StatusBadGateway)
		return
	}
	servePosterImage(c, data)
}

func fetchMikanPosterImage(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := validateMikanPosterURL(rawURL)
	if err != nil {
		return nil, err
	}
	cacheKey := parsed.String()
	if data, ok := mikanPosterOriginals.get(cacheKey); ok {
		return data, nil
	}

	value, err, _ := mikanPosterFetches.Do(cacheKey, func() (any, error) {
		if data, ok := mikanPosterOriginals.get(cacheKey); ok {
			return data, nil
		}
		select {
		case mikanPosterSlots <- struct{}{}:
			defer func() { <-mikanPosterSlots }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, cacheKey, nil)
		if requestErr != nil {
			return nil, requestErr
		}
		request.Header.Set("Accept", "image/avif,image/webp,image/jpeg,image/png,image/*;q=0.8")
		request.Header.Set("Referer", "https://mikanani.me/")
		request.Header.Set("User-Agent", httpx.DefaultUserAgent)

		client := httpx.NewHTTPClientWithProxy(15*time.Second, configuredProxyURL(model.ConfigKeyProxyMikan))
		client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
			_, redirectErr := validateMikanPosterURL(req.URL.String())
			return redirectErr
		}
		response, responseErr := client.Do(request)
		if responseErr != nil {
			return nil, responseErr
		}
		defer safeio.Close(response.Body)
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("mikan image server returned HTTP %d", response.StatusCode)
		}
		if response.ContentLength > maxMikanPosterBytes {
			return nil, fmt.Errorf("mikan poster exceeds size limit")
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, maxMikanPosterBytes+1))
		if readErr != nil {
			return nil, readErr
		}
		if len(data) == 0 || len(data) > maxMikanPosterBytes {
			return nil, fmt.Errorf("mikan poster is empty or too large")
		}
		contentType := http.DetectContentType(data)
		if !strings.HasPrefix(contentType, "image/") {
			return nil, fmt.Errorf("mikan poster response is not an image")
		}
		mikanPosterOriginals.put(cacheKey, data)
		return data, nil
	})
	if err != nil {
		return nil, err
	}
	return value.([]byte), nil
}

func validateMikanPosterURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("invalid Mikan poster URL: %w", err)
	}
	host := strings.ToLower(parsed.Hostname())
	if !strings.EqualFold(parsed.Scheme, "https") || (host != "mikanani.me" && host != "www.mikanani.me") {
		return nil, fmt.Errorf("mikan poster host is not allowed")
	}
	if parsed.User != nil || (parsed.Port() != "" && parsed.Port() != "443") {
		return nil, fmt.Errorf("mikan poster URL contains unsupported authority")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		return nil, fmt.Errorf("mikan poster path is empty")
	}
	return parsed, nil
}
