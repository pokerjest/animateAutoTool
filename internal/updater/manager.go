package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
)

const (
	defaultIntervalMinutes = 30
	minIntervalMinutes     = 1
	maxIntervalMinutes     = 24 * 60
	checkLoopInterval      = time.Minute

	defaultRepoOwner = "pokerjest"
	defaultRepoName  = "animateAutoTool"

	httpTimeout      = 60 * time.Second
	downloadTimeout  = 20 * time.Minute
	restartDelay     = 1200 * time.Millisecond
	maxVersionParts  = 3
	maxBackoffDelay  = 30 * time.Minute
	defaultUserAgent = "AnimateAutoTool-Updater/1.0"
	resultError      = "error"
	versionZero      = "v0.0.0"
	goosDarwin       = "darwin"
	goosLinux        = "linux"
)

type settings struct {
	Enabled         bool
	AutoApplyEnable bool
	RequireChecksum bool
	IntervalMinutes int
	Interval        time.Duration
	RepoOwner       string
	RepoName        string
}

type Status struct {
	Enabled         bool
	AutoApplyEnable bool
	RequireChecksum bool
	IntervalMinutes int
	RepoOwner       string
	RepoName        string

	Running bool

	CurrentVersion   string
	LatestVersion    string
	HasUpdate        bool
	ChecksumVerified bool

	ReleaseURL         string
	ReleasePublishedAt time.Time
	AssetName          string
	AssetURL           string
	BackoffUntil       time.Time

	LastCheckAt  time.Time
	LastUpdateAt time.Time
	LastResult   string
	LastMessage  string
	LastError    string
	LastSource   string
}

type Manager struct {
	mu                  sync.RWMutex
	status              Status
	quit                chan struct{}
	etag                string
	cachedRelease       *githubRelease
	cachedRepoOwner     string
	cachedRepoName      string
	consecutiveFailures int
	backoffUntil        time.Time
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type githubRelease struct {
	HTMLURL     string         `json:"html_url"`
	TagName     string         `json:"tag_name"`
	PublishedAt string         `json:"published_at"`
	Draft       bool           `json:"draft"`
	Prerelease  bool           `json:"prerelease"`
	Assets      []releaseAsset `json:"assets"`
}

var (
	startOnce sync.Once
	manager   *Manager
)

func Start() {
	startOnce.Do(func() {
		manager = &Manager{quit: make(chan struct{})}
		manager.setDisabledHintIfNeeded()
		go manager.loop()
	})
}

func Snapshot() Status {
	if manager == nil {
		return Status{LastResult: "not_started", LastMessage: "更新服务尚未启动"}
	}

	cfg := loadSettings()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.applySettingsLocked(cfg)
	manager.status.BackoffUntil = manager.backoffUntil
	return manager.status
}

func CheckNow(triggerSource string) Status {
	Start()
	return manager.runCheck(triggerSource, false)
}

func CheckAndPullNow(triggerSource string) Status {
	Start()
	return manager.runCheck(triggerSource, true)
}

func (m *Manager) setDisabledHintIfNeeded() {
	cfg := loadSettings()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.applySettingsLocked(cfg)
	if !cfg.Enabled {
		m.status.LastResult = "disabled"
		m.status.LastMessage = "自动检查未启用，可手动点击“立即检查”"
	}
}

func (m *Manager) loop() {
	m.runPeriodicCheck("startup")

	ticker := time.NewTicker(checkLoopInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.runPeriodicCheck("auto")
		case <-m.quit:
			return
		}
	}
}

func (m *Manager) runPeriodicCheck(source string) {
	cfg := loadSettings()
	now := time.Now()

	m.mu.Lock()
	m.applySettingsLocked(cfg)
	m.status.BackoffUntil = m.backoffUntil

	if m.status.Running {
		m.mu.Unlock()
		return
	}
	if !cfg.Enabled {
		if m.status.LastResult == "" || m.status.LastResult == "not_started" {
			m.status.LastResult = "disabled"
			m.status.LastMessage = "自动检查未启用，可手动点击“立即检查”"
		}
		m.mu.Unlock()
		return
	}

	if !m.backoffUntil.IsZero() && now.Before(m.backoffUntil) {
		m.status.LastResult = "backoff"
		m.status.LastMessage = fmt.Sprintf("自动检查退避中，将在 %s 后恢复", m.backoffUntil.Local().Format("2006-01-02 15:04:05"))
		m.mu.Unlock()
		return
	}

	last := m.status.LastCheckAt
	if !last.IsZero() && time.Since(last) < cfg.Interval {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	m.runCheck(source, cfg.AutoApplyEnable)
}

func (m *Manager) runCheck(source string, applyWhenBehind bool) Status {
	cfg := loadSettings()
	now := time.Now()

	m.mu.Lock()
	if m.status.Running {
		snapshot := m.status
		m.mu.Unlock()
		return snapshot
	}
	m.applySettingsLocked(cfg)
	m.status.BackoffUntil = m.backoffUntil
	m.status.Running = true
	m.status.LastSource = source
	m.status.LastError = ""
	m.mu.Unlock()

	current := normalizeVersion(currentVersion())
	result := resultError
	message := "检查失败"
	errText := ""
	latest := ""
	releaseURL := ""
	assetName := ""
	assetURL := ""
	hasUpdate := false
	checksumVerified := false
	publishedAt := time.Time{}
	backoffUntil := time.Time{}
	var lastUpdate time.Time

	finish := func() Status {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.status.CurrentVersion = current
		m.status.LatestVersion = latest
		m.status.HasUpdate = hasUpdate
		m.status.ChecksumVerified = checksumVerified
		m.status.ReleaseURL = releaseURL
		m.status.ReleasePublishedAt = publishedAt
		m.status.AssetName = assetName
		m.status.AssetURL = assetURL
		m.status.LastResult = result
		m.status.LastMessage = message
		m.status.LastError = strings.TrimSpace(errText)
		m.status.LastCheckAt = now
		if !lastUpdate.IsZero() {
			m.status.LastUpdateAt = lastUpdate
		}
		if !backoffUntil.IsZero() {
			m.backoffUntil = backoffUntil
		}
		m.status.BackoffUntil = m.backoffUntil
		m.status.Running = false
		return m.status
	}

	release, notModified, retryAfter, err := m.fetchLatestRelease(cfg.RepoOwner, cfg.RepoName, currentVersionWantsPrerelease(current))
	if err != nil {
		backoffUntil = m.recordFailure(now, retryAfter)
		result = resultError
		message = "获取最新 Release 失败"
		if !backoffUntil.IsZero() {
			message = fmt.Sprintf("获取最新 Release 失败，自动重试时间：%s", backoffUntil.Local().Format("2006-01-02 15:04:05"))
		}
		errText = err.Error()
		return finish()
	}

	if notModified {
		release = m.getCachedRelease(cfg.RepoOwner, cfg.RepoName)
		if release == nil {
			backoffUntil = m.recordFailure(now, time.Time{})
			result = resultError
			message = "远端返回未修改，但本地无缓存可用"
			errText = "release cache is empty"
			return finish()
		}
	}

	m.clearFailures()
	latest = normalizeVersion(release.TagName)
	releaseURL = strings.TrimSpace(release.HTMLURL)
	if t, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(release.PublishedAt)); parseErr == nil {
		publishedAt = t
	}

	asset, assetErr := pickAssetForCurrentPlatform(release)
	if assetErr != nil {
		result = "unsupported"
		message = "找不到当前平台可用的安装包"
		errText = assetErr.Error()
		return finish()
	}
	assetName = asset.Name
	assetURL = asset.BrowserDownloadURL

	cmp := compareVersions(current, latest)
	if cmp >= 0 {
		hasUpdate = false
		result = "up_to_date"
		message = "当前已是最新版本"
		return finish()
	}

	hasUpdate = true
	result = "behind"
	message = fmt.Sprintf("检测到新版本 %s", latest)

	if !applyWhenBehind {
		return finish()
	}

	expectedChecksum := ""
	if cfg.RequireChecksum {
		expectedChecksum, err = fetchExpectedChecksum(release, assetName)
		if err != nil {
			result = resultError
			message = "未通过完整性校验前置检查"
			errText = err.Error()
			return finish()
		}
	}

	artifactPath, err := downloadAsset(assetURL, assetName)
	if err != nil {
		result = resultError
		message = "下载更新包失败"
		errText = err.Error()
		return finish()
	}

	if expectedChecksum != "" {
		if err := verifyFileSHA256(artifactPath, expectedChecksum); err != nil {
			result = resultError
			message = "更新包完整性校验失败"
			errText = err.Error()
			return finish()
		}
		checksumVerified = true
	}

	if err := applyUpdateForPlatform(artifactPath); err != nil {
		result = resultError
		message = "应用更新失败"
		errText = err.Error()
		return finish()
	}

	result = "restarting"
	message = "更新包已应用，正在重启到新版本"
	lastUpdate = now
	return finish()
}

func (m *Manager) applySettingsLocked(cfg settings) {
	if !sameRepo(m.status.RepoOwner, m.status.RepoName, cfg.RepoOwner, cfg.RepoName) {
		m.etag = ""
		m.cachedRelease = nil
		m.cachedRepoOwner = ""
		m.cachedRepoName = ""
	}
	m.status.Enabled = cfg.Enabled
	m.status.AutoApplyEnable = cfg.AutoApplyEnable
	m.status.RequireChecksum = cfg.RequireChecksum
	m.status.IntervalMinutes = cfg.IntervalMinutes
	m.status.RepoOwner = cfg.RepoOwner
	m.status.RepoName = cfg.RepoName
}

func (m *Manager) getCachedRelease(owner, repo string) *githubRelease {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !sameRepo(owner, repo, m.cachedRepoOwner, m.cachedRepoName) {
		return nil
	}
	if m.cachedRelease == nil {
		return nil
	}
	cp := *m.cachedRelease
	cp.Assets = append([]releaseAsset(nil), m.cachedRelease.Assets...)
	return &cp
}

func (m *Manager) clearFailures() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.consecutiveFailures = 0
	m.backoffUntil = time.Time{}
}

func (m *Manager) recordFailure(now, explicitRetry time.Time) time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.consecutiveFailures++
	if explicitRetry.After(now) {
		m.backoffUntil = explicitRetry
		return m.backoffUntil
	}

	delay := time.Minute
	for i := 1; i < m.consecutiveFailures; i++ {
		delay *= 2
		if delay >= maxBackoffDelay {
			delay = maxBackoffDelay
			break
		}
	}
	candidate := now.Add(delay)
	if candidate.After(m.backoffUntil) {
		m.backoffUntil = candidate
	}
	return m.backoffUntil
}

func (m *Manager) fetchLatestRelease(owner, repo string, includePrerelease bool) (*githubRelease, bool, time.Time, error) {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases?per_page=10", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, time.Time{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", defaultUserAgent)

	m.mu.RLock()
	e := ""
	if sameRepo(owner, repo, m.cachedRepoOwner, m.cachedRepoName) {
		e = strings.TrimSpace(m.etag)
	}
	m.mu.RUnlock()
	if e != "" {
		req.Header.Set("If-None-Match", e)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false, time.Time{}, err
	}
	defer safeio.Close(resp.Body)

	if resp.StatusCode == http.StatusNotModified {
		return nil, true, time.Time{}, nil
	}

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
		return nil, false, retryAfter, fmt.Errorf("github api returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, false, time.Time{}, err
	}
	release, err := pickLatestPublishedRelease(releases, includePrerelease)
	if err != nil {
		return nil, false, time.Time{}, err
	}

	m.mu.Lock()
	cp := *release
	cp.Assets = append([]releaseAsset(nil), release.Assets...)
	m.cachedRelease = &cp
	m.cachedRepoOwner = owner
	m.cachedRepoName = repo
	if etag := strings.TrimSpace(resp.Header.Get("ETag")); etag != "" {
		m.etag = etag
	}
	m.mu.Unlock()

	return release, false, time.Time{}, nil
}

func pickLatestPublishedRelease(releases []githubRelease, includePrerelease bool) (*githubRelease, error) {
	for i := range releases {
		release := &releases[i]
		if release.Draft {
			continue
		}
		if release.Prerelease && !includePrerelease {
			continue
		}
		if strings.TrimSpace(release.TagName) == "" {
			continue
		}
		return release, nil
	}
	return nil, errors.New("latest published release tag is empty")
}

func currentVersionWantsPrerelease(v string) bool {
	return parseSemVer(v).Prerelease != ""
}

func parseRetryAfter(resp *http.Response) time.Time {
	if resp == nil {
		return time.Time{}
	}
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return time.Time{}
	}
	if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
		return time.Now().Add(time.Duration(secs) * time.Second)
	}
	if t, err := http.ParseTime(raw); err == nil {
		return t
	}
	return time.Time{}
}

func sameRepo(aOwner, aRepo, bOwner, bRepo string) bool {
	return strings.EqualFold(strings.TrimSpace(aOwner), strings.TrimSpace(bOwner)) &&
		strings.EqualFold(strings.TrimSpace(aRepo), strings.TrimSpace(bRepo))
}

func loadSettings() settings {
	cfg := settings{
		Enabled:         false,
		AutoApplyEnable: false,
		RequireChecksum: true,
		IntervalMinutes: defaultIntervalMinutes,
		RepoOwner:       defaultRepoOwner,
		RepoName:        defaultRepoName,
	}

	cfg.Enabled = parseBool(readGlobalConfig(model.ConfigKeyRepoUpdateEnabled), false)
	cfg.AutoApplyEnable = parseBool(readGlobalConfig(model.ConfigKeyRepoAutoPullEnabled), false)
	cfg.RequireChecksum = parseBool(readGlobalConfig(model.ConfigKeyRepoRequireChecksum), true)

	if raw := strings.TrimSpace(readGlobalConfig(model.ConfigKeyRepoUpdateIntervalMinutes)); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			cfg.IntervalMinutes = clampInt(n, minIntervalMinutes, maxIntervalMinutes)
		}
	}

	if owner := strings.TrimSpace(readGlobalConfig(model.ConfigKeyRepoUpdateOwner)); owner != "" {
		cfg.RepoOwner = owner
	}
	if repo := strings.TrimSpace(readGlobalConfig(model.ConfigKeyRepoUpdateName)); repo != "" {
		cfg.RepoName = repo
	}

	cfg.Interval = time.Duration(cfg.IntervalMinutes) * time.Minute
	return cfg
}

func readGlobalConfig(key string) string {
	if db.DB == nil {
		return ""
	}

	var val string
	if err := db.DB.Model(&model.GlobalConfig{}).Where("key = ?", key).Select("value").Scan(&val).Error; err != nil {
		return ""
	}
	return strings.TrimSpace(val)
}

func parseBool(raw string, fallback bool) bool {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return fallback
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
