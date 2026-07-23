package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/httpx"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
	"golang.org/x/sync/singleflight"
)

const maxCalendarPosterBytes = 12 << 20

const (
	maxCalendarPosterCacheBytes   = 64 << 20
	maxCalendarPosterCacheEntries = 192
)

type calendarPosterImageCache struct {
	mu      sync.Mutex
	entries map[string][]byte
	order   []string
	size    int
}

func (c *calendarPosterImageCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.entries[key]
	return data, ok
}

func (c *calendarPosterImageCache) put(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; exists {
		return
	}
	c.entries[key] = data
	c.order = append(c.order, key)
	c.size += len(data)
	for len(c.order) > maxCalendarPosterCacheEntries || c.size > maxCalendarPosterCacheBytes {
		oldest := c.order[0]
		c.order = c.order[1:]
		c.size -= len(c.entries[oldest])
		delete(c.entries, oldest)
	}
}

type calendarPosterSourceCache struct {
	mu      sync.RWMutex
	sources map[int][]string
}

var (
	calendarPosterSources   = calendarPosterSourceCache{sources: make(map[int][]string)}
	calendarPosterOriginals = calendarPosterImageCache{entries: make(map[string][]byte)}
	calendarPosterFetches   singleflight.Group
	loadCalendarPosterImage = fetchCalendarPosterImage
)

func rememberCalendarPosterSources(calendar []bangumi.CalendarItem) {
	next := make(map[int][]string)
	for _, day := range calendar {
		for _, item := range day.Items {
			if item.ID <= 0 {
				continue
			}
			seen := make(map[string]struct{})
			for _, candidate := range []string{item.Images.Large, item.Images.Common, item.Images.Medium, item.Images.Small, item.Images.Grid} {
				source := strings.TrimSpace(candidate)
				if source == "" {
					continue
				}
				if _, exists := seen[source]; exists {
					continue
				}
				seen[source] = struct{}{}
				next[item.ID] = append(next[item.ID], source)
			}
		}
	}
	calendarPosterSources.mu.Lock()
	calendarPosterSources.sources = next
	calendarPosterSources.mu.Unlock()
}

func calendarPosterSourcesFor(subjectID int) []string {
	calendarPosterSources.mu.RLock()
	sources := append([]string(nil), calendarPosterSources.sources[subjectID]...)
	calendarPosterSources.mu.RUnlock()
	return sources
}

func V1CalendarPosterHandler(c *gin.Context) {
	subjectID, err := strconv.Atoi(c.Param("id"))
	if err != nil || subjectID <= 0 {
		c.Status(http.StatusBadRequest)
		return
	}

	var lastErr error
	for _, source := range calendarPosterSourcesFor(subjectID) {
		data, fetchErr := loadCalendarPosterImage(c.Request.Context(), source)
		if fetchErr == nil && len(data) > 0 {
			servePosterImage(c, data)
			return
		}
		lastErr = fetchErr
	}
	if lastErr != nil {
		log.Printf("calendar poster %d unavailable: %v", subjectID, lastErr)
	}
	c.Status(http.StatusNotFound)
}

func fetchCalendarPosterImage(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := validateBangumiPosterURL(rawURL)
	if err != nil {
		return nil, err
	}
	cacheKey := parsed.String()
	if data, ok := calendarPosterOriginals.get(cacheKey); ok {
		return data, nil
	}

	value, err, _ := calendarPosterFetches.Do(cacheKey, func() (any, error) {
		if data, ok := calendarPosterOriginals.get(cacheKey); ok {
			return data, nil
		}
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, cacheKey, nil)
		if requestErr != nil {
			return nil, requestErr
		}
		request.Header.Set("Accept", "image/jpeg,image/png,image/*;q=0.8")
		request.Header.Set("User-Agent", httpx.DefaultUserAgent)

		client := httpx.NewHTTPClientWithProxy(12*time.Second, configuredProxyURL(model.ConfigKeyProxyBangumi))
		client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
			_, redirectErr := validateBangumiPosterURL(req.URL.String())
			return redirectErr
		}
		response, responseErr := client.Do(request)
		if responseErr != nil {
			return nil, responseErr
		}
		defer safeio.Close(response.Body)
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("image server returned HTTP %d", response.StatusCode)
		}
		if response.ContentLength > maxCalendarPosterBytes {
			return nil, fmt.Errorf("calendar poster exceeds size limit")
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, maxCalendarPosterBytes+1))
		if readErr != nil {
			return nil, readErr
		}
		if len(data) == 0 || len(data) > maxCalendarPosterBytes {
			return nil, fmt.Errorf("calendar poster is empty or too large")
		}
		contentType := http.DetectContentType(data)
		if contentType != posterJPEGContentType && contentType != "image/png" {
			return nil, fmt.Errorf("calendar poster response is not an image")
		}
		calendarPosterOriginals.put(cacheKey, data)
		return data, nil
	})
	if err != nil {
		return nil, err
	}
	return value.([]byte), nil
}

func validateBangumiPosterURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("invalid calendar poster URL: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(parsed.Hostname(), "lain.bgm.tv") {
		return nil, fmt.Errorf("calendar poster host is not allowed")
	}
	if parsed.User != nil || (parsed.Port() != "" && parsed.Port() != "443") {
		return nil, fmt.Errorf("calendar poster URL contains unsupported authority")
	}
	return parsed, nil
}
