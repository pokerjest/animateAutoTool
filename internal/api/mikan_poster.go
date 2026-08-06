package api

import (
	"context"
	"errors"
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

const (
	maxMikanPosterBytes = 12 << 20
	mikanPosterScheme   = "https"
)

var (
	mikanPosterHosts     = []string{"mikanani.me", "mikanime.tv", "mikanani.kas.pub"}
	mikanPosterOriginals = calendarPosterImageCache{entries: make(map[string][]byte)}
	mikanPosterFetches   singleflight.Group
	mikanPosterSlots     = make(chan struct{}, 4)
	loadMikanPosterImage = fetchMikanPosterImage
)

// V1MikanPosterHandler serves Mikan covers through AnimateTool so remote
// browsers do not need direct access to trusted Mikan hosts or the user's proxy.
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
	cacheKey := mikanPosterCacheKey(parsed)
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

		client := httpx.NewHTTPClientWithProxy(15*time.Second, configuredProxyURL(model.ConfigKeyProxyMikan))
		client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
			_, redirectErr := validateMikanPosterURL(req.URL.String())
			return redirectErr
		}
		data, fetchErr := raceMikanPosterCandidates(ctx, mikanPosterCandidates(parsed), func(candidateCtx context.Context, candidate *url.URL) ([]byte, error) {
			return fetchMikanPosterCandidate(candidateCtx, client, candidate)
		})
		if fetchErr != nil {
			return nil, fetchErr
		}
		mikanPosterOriginals.put(cacheKey, data)
		return data, nil
	})
	if err != nil {
		return nil, err
	}
	return value.([]byte), nil
}

func mikanPosterCacheKey(parsed *url.URL) string {
	canonical := *parsed
	canonical.Scheme = mikanPosterScheme
	canonical.Host = mikanPosterHosts[0]
	canonical.User = nil
	canonical.Fragment = ""
	return canonical.String()
}

func mikanPosterCandidates(parsed *url.URL) []*url.URL {
	candidates := make([]*url.URL, 0, len(mikanPosterHosts))
	for _, host := range mikanPosterHosts {
		candidate := *parsed
		candidate.Scheme = mikanPosterScheme
		candidate.Host = host
		candidate.User = nil
		candidate.Fragment = ""
		candidates = append(candidates, &candidate)
	}
	return candidates
}

type mikanPosterRaceResult struct {
	data []byte
	host string
	err  error
}

// raceMikanPosterCandidates returns the first validated image and cancels slower sources.
func raceMikanPosterCandidates(
	ctx context.Context,
	candidates []*url.URL,
	fetch func(context.Context, *url.URL) ([]byte, error),
) ([]byte, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no Mikan poster sources configured")
	}

	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan mikanPosterRaceResult, len(candidates))
	for _, candidate := range candidates {
		candidate := candidate
		go func() {
			data, err := fetch(raceCtx, candidate)
			results <- mikanPosterRaceResult{data: data, host: candidate.Hostname(), err: err}
		}()
	}

	failures := make([]error, 0, len(candidates))
	for range candidates {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result := <-results:
			if result.err == nil && len(result.data) > 0 {
				cancel()
				return result.data, nil
			}
			if result.err == nil {
				result.err = fmt.Errorf("empty image response")
			}
			failures = append(failures, fmt.Errorf("%s: %w", result.host, result.err))
		}
	}
	return nil, fmt.Errorf("all Mikan poster sources failed: %w", errors.Join(failures...))
}

func fetchMikanPosterCandidate(ctx context.Context, client *http.Client, candidate *url.URL) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, candidate.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "image/avif,image/webp,image/jpeg,image/png,image/*;q=0.8")
	request.Header.Set("Referer", "https://"+candidate.Hostname()+"/")
	request.Header.Set("User-Agent", httpx.DefaultUserAgent)

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer safeio.Close(response.Body)
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image server returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxMikanPosterBytes {
		return nil, fmt.Errorf("mikan poster exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxMikanPosterBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxMikanPosterBytes {
		return nil, fmt.Errorf("mikan poster is empty or too large")
	}
	if contentType := http.DetectContentType(data); !strings.HasPrefix(contentType, "image/") {
		return nil, fmt.Errorf("mikan poster response is not an image")
	}
	return data, nil
}

func validateMikanPosterURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("invalid Mikan poster URL: %w", err)
	}
	host := strings.ToLower(parsed.Hostname())
	if !strings.EqualFold(parsed.Scheme, mikanPosterScheme) || !isAllowedMikanPosterHost(host) {
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

func isAllowedMikanPosterHost(host string) bool {
	if host == "www.mikanani.me" {
		return true
	}
	for _, allowed := range mikanPosterHosts {
		if host == allowed {
			return true
		}
	}
	return false
}
