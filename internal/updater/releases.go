package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/safeio"
)

const (
	releaseCatalogPageSize = 100
	releaseCatalogCacheTTL = 5 * time.Minute
)

type ReleaseChannel string

const (
	ReleaseChannelStable ReleaseChannel = "stable"
	ReleaseChannelBeta   ReleaseChannel = "beta"
)

type ReleaseInfo struct {
	Version          string     `json:"version"`
	Prerelease       bool       `json:"prerelease"`
	PublishedAt      *time.Time `json:"published_at,omitempty"`
	ReleaseURL       string     `json:"release_url"`
	AssetName        string     `json:"asset_name,omitempty"`
	AssetAvailable   bool       `json:"asset_available"`
	NewerThanCurrent bool       `json:"newer_than_current"`
}

type ReleaseCatalog struct {
	Channel        ReleaseChannel `json:"channel"`
	CurrentVersion string         `json:"current_version"`
	LatestVersion  string         `json:"latest_version,omitempty"`
	Items          []ReleaseInfo  `json:"items"`
}

func ParseReleaseChannel(raw string) (ReleaseChannel, error) {
	switch ReleaseChannel(strings.ToLower(strings.TrimSpace(raw))) {
	case "", ReleaseChannelStable:
		return ReleaseChannelStable, nil
	case ReleaseChannelBeta:
		return ReleaseChannelBeta, nil
	default:
		return "", errors.New("更新通道只支持 stable 或 beta")
	}
}

func ValidReleaseVersion(raw string) bool {
	value := strings.TrimSpace(raw)
	if value == "" || strings.ContainsAny(value, "/\\ \t\r\n") {
		return false
	}
	value = strings.TrimPrefix(value, "v")
	if value == "" {
		return false
	}
	if buildIndex := strings.IndexByte(value, '+'); buildIndex >= 0 {
		build := value[buildIndex+1:]
		if !validVersionIdentifiers(build) {
			return false
		}
		value = value[:buildIndex]
	}
	prerelease := ""
	hasPrerelease := false
	if prereleaseIndex := strings.IndexByte(value, '-'); prereleaseIndex >= 0 {
		hasPrerelease = true
		prerelease = value[prereleaseIndex+1:]
		value = value[:prereleaseIndex]
	}
	parts := strings.Split(value, ".")
	if len(parts) < 3 || len(parts) > maxVersionParts {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		if _, err := strconv.ParseUint(part, 10, 32); err != nil {
			return false
		}
	}
	if !hasPrerelease {
		return true
	}
	return validVersionIdentifiers(prerelease)
}

func validVersionIdentifiers(value string) bool {
	if value == "" {
		return false
	}
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" {
			return false
		}
		for _, r := range identifier {
			if (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && r != '-' {
				return false
			}
		}
	}
	return true
}

func releaseIsPrerelease(release githubRelease) bool {
	if release.Prerelease {
		return true
	}
	version := strings.TrimPrefix(strings.TrimSpace(release.TagName), "v")
	if buildIndex := strings.IndexByte(version, '+'); buildIndex >= 0 {
		version = version[:buildIndex]
	}
	return strings.Contains(version, "-")
}

func ListReleases(channel ReleaseChannel) (ReleaseCatalog, error) {
	Start()
	cfg := loadSettings()
	releases, _, err := manager.fetchPublishedReleasesCached(cfg.RepoOwner, cfg.RepoName)
	if err != nil {
		return ReleaseCatalog{}, err
	}
	return buildReleaseCatalog(releases, channel, normalizeVersion(currentVersion())), nil
}

func buildReleaseCatalog(releases []githubRelease, channel ReleaseChannel, current string) ReleaseCatalog {
	current = normalizeVersion(current)
	filtered := make([]githubRelease, 0, len(releases))
	for _, release := range releases {
		if release.Draft || strings.TrimSpace(release.TagName) == "" || !ValidReleaseVersion(release.TagName) {
			continue
		}
		if channel == ReleaseChannelStable && releaseIsPrerelease(release) {
			continue
		}
		filtered = append(filtered, release)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		comparison := compareVersions(filtered[i].TagName, filtered[j].TagName)
		if comparison != 0 {
			return comparison > 0
		}
		return filtered[i].PublishedAt > filtered[j].PublishedAt
	})

	catalog := ReleaseCatalog{
		Channel:        channel,
		CurrentVersion: current,
		Items:          make([]ReleaseInfo, 0, len(filtered)),
	}
	for i := range filtered {
		release := &filtered[i]
		info := ReleaseInfo{
			Version:          normalizeVersion(release.TagName),
			Prerelease:       releaseIsPrerelease(*release),
			ReleaseURL:       strings.TrimSpace(release.HTMLURL),
			NewerThanCurrent: compareVersions(current, release.TagName) < 0,
		}
		if publishedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(release.PublishedAt)); err == nil {
			info.PublishedAt = &publishedAt
		}
		if asset, err := pickAssetForCurrentPlatform(release); err == nil {
			info.AssetAvailable = true
			info.AssetName = asset.Name
		}
		if catalog.LatestVersion == "" && info.AssetAvailable && info.NewerThanCurrent {
			catalog.LatestVersion = info.Version
		}
		catalog.Items = append(catalog.Items, info)
	}
	return catalog
}

func findPublishedReleaseByVersion(releases []githubRelease, version string) (*githubRelease, error) {
	if !ValidReleaseVersion(version) {
		return nil, errors.New("指定的版本号格式无效")
	}
	target := normalizeVersion(version)
	for i := range releases {
		release := &releases[i]
		if release.Draft || !ValidReleaseVersion(release.TagName) {
			continue
		}
		if strings.EqualFold(normalizeVersion(release.TagName), target) {
			return release, nil
		}
	}
	return nil, fmt.Errorf("指定版本 %s 不存在、仍是草稿或已被下架", target)
}

func (m *Manager) fetchReleaseByVersion(owner, repo, version string) (*githubRelease, time.Time, error) {
	releases, retryAfter, err := m.fetchPublishedReleases(owner, repo)
	if err != nil {
		return nil, retryAfter, err
	}
	release, err := findPublishedReleaseByVersion(releases, version)
	return release, retryAfter, err
}

func (m *Manager) fetchPublishedReleasesCached(owner, repo string) ([]githubRelease, time.Time, error) {
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	m.mu.RLock()
	if sameRepo(owner, repo, m.releaseCatalogOwner, m.releaseCatalogName) &&
		!m.releaseCatalogAt.IsZero() &&
		time.Since(m.releaseCatalogAt) < releaseCatalogCacheTTL {
		releases := cloneReleases(m.releaseCatalogCache)
		m.mu.RUnlock()
		return releases, time.Time{}, nil
	}
	m.mu.RUnlock()
	return m.fetchPublishedReleases(owner, repo)
}

func (m *Manager) fetchPublishedReleases(owner, repo string) ([]githubRelease, time.Time, error) {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases?per_page=%d", owner, repo, releaseCatalogPageSize)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, time.Time{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", defaultUserAgent)

	resp, err := updaterHTTPClient(httpTimeout).Do(req)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer safeio.Close(resp.Body)

	retryAfter := parseRetryAfter(resp)
	if resp.StatusCode == http.StatusForbidden && strings.TrimSpace(resp.Header.Get("X-RateLimit-Remaining")) == "0" {
		if reset := strings.TrimSpace(resp.Header.Get("X-RateLimit-Reset")); reset != "" {
			if unixTS, parseErr := strconv.ParseInt(reset, 10, 64); parseErr == nil {
				retryAfter = time.Unix(unixTS, 0)
			}
		}
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, retryAfter, fmt.Errorf("github api returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, time.Time{}, err
	}
	m.mu.Lock()
	m.releaseCatalogCache = cloneReleases(releases)
	m.releaseCatalogAt = time.Now()
	m.releaseCatalogOwner = owner
	m.releaseCatalogName = repo
	m.mu.Unlock()
	return releases, retryAfter, nil
}

func cloneReleases(releases []githubRelease) []githubRelease {
	cloned := make([]githubRelease, len(releases))
	for i := range releases {
		cloned[i] = releases[i]
		cloned[i].Assets = append([]releaseAsset(nil), releases[i].Assets...)
	}
	return cloned
}
