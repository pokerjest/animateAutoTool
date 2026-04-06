package updater

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	appversion "github.com/pokerjest/animateAutoTool/internal/version"
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
	result := "error"
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

	release, notModified, retryAfter, err := m.fetchLatestRelease(cfg.RepoOwner, cfg.RepoName)
	if err != nil {
		backoffUntil = m.recordFailure(now, retryAfter)
		result = "error"
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
			result = "error"
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
			result = "error"
			message = "未通过完整性校验前置检查"
			errText = err.Error()
			return finish()
		}
	}

	artifactPath, err := downloadAsset(assetURL, assetName)
	if err != nil {
		result = "error"
		message = "下载更新包失败"
		errText = err.Error()
		return finish()
	}

	if expectedChecksum != "" {
		if err := verifyFileSHA256(artifactPath, expectedChecksum); err != nil {
			result = "error"
			message = "更新包完整性校验失败"
			errText = err.Error()
			return finish()
		}
		checksumVerified = true
	}

	if err := applyUpdateForPlatform(artifactPath); err != nil {
		result = "error"
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

func (m *Manager) fetchLatestRelease(owner, repo string) (*githubRelease, bool, time.Time, error) {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
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
	defer resp.Body.Close()

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

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, false, time.Time{}, err
	}
	if strings.TrimSpace(release.TagName) == "" {
		return nil, false, time.Time{}, errors.New("latest release tag is empty")
	}

	m.mu.Lock()
	m.cachedRelease = &release
	m.cachedRepoOwner = owner
	m.cachedRepoName = repo
	if etag := strings.TrimSpace(resp.Header.Get("ETag")); etag != "" {
		m.etag = etag
	}
	m.mu.Unlock()

	return &release, false, time.Time{}, nil
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

func currentVersion() string {
	v := strings.TrimSpace(appversion.AppVersion)
	if isBuildVersionUnset(v) {
		if fileVersion, err := readVersionFile(); err == nil && fileVersion != "" {
			v = fileVersion
		}
	}
	if v == "" {
		v = "v0.0.0"
	}
	return normalizeVersion(v)
}

func isBuildVersionUnset(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return v == "" || v == "dev" || v == "development" || v == "unknown"
}

func readVersionFile() (string, error) {
	candidates := []string{
		filepath.Join(config.RootDir(), "VERSION"),
		filepath.Join(filepath.Dir(config.ConfigFilePath()), "VERSION"),
	}

	for _, path := range candidates {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		v := strings.TrimSpace(string(content))
		if v != "" {
			return v, nil
		}
	}

	return "", errors.New("VERSION file not found")
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "v0.0.0"
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

type semVer struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
	Valid      bool
}

func compareVersions(a, b string) int {
	av := parseSemVer(a)
	bv := parseSemVer(b)
	if !av.Valid || !bv.Valid {
		ap := parseVersionParts(a)
		bp := parseVersionParts(b)
		for i := 0; i < maxVersionParts; i++ {
			if ap[i] < bp[i] {
				return -1
			}
			if ap[i] > bp[i] {
				return 1
			}
		}
		return 0
	}

	if av.Major != bv.Major {
		if av.Major < bv.Major {
			return -1
		}
		return 1
	}
	if av.Minor != bv.Minor {
		if av.Minor < bv.Minor {
			return -1
		}
		return 1
	}
	if av.Patch != bv.Patch {
		if av.Patch < bv.Patch {
			return -1
		}
		return 1
	}
	return comparePrerelease(av.Prerelease, bv.Prerelease)
}

func parseVersionParts(v string) [maxVersionParts]int {
	v = normalizeVersion(v)
	v = strings.TrimPrefix(v, "v")
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		v = v[:idx]
	}
	if idx := strings.IndexByte(v, '+'); idx >= 0 {
		v = v[:idx]
	}

	parts := strings.Split(v, ".")
	var out [maxVersionParts]int
	for i := 0; i < maxVersionParts && i < len(parts); i++ {
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil || n < 0 {
			continue
		}
		out[i] = n
	}
	return out
}

func parseSemVer(v string) semVer {
	v = normalizeVersion(v)
	v = strings.TrimPrefix(v, "v")
	if idx := strings.IndexByte(v, '+'); idx >= 0 {
		v = v[:idx]
	}

	pr := ""
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		pr = v[idx+1:]
		v = v[:idx]
	}

	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return semVer{}
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	patch, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil || major < 0 || minor < 0 || patch < 0 {
		return semVer{}
	}

	return semVer{Major: major, Minor: minor, Patch: patch, Prerelease: pr, Valid: true}
}

func comparePrerelease(a, b string) int {
	if a == "" && b == "" {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}

	ai := strings.Split(a, ".")
	bi := strings.Split(b, ".")
	maxLen := len(ai)
	if len(bi) > maxLen {
		maxLen = len(bi)
	}

	for i := 0; i < maxLen; i++ {
		if i >= len(ai) {
			return -1
		}
		if i >= len(bi) {
			return 1
		}

		x := ai[i]
		y := bi[i]
		xn, xNum := parseNumericIdentifier(x)
		yn, yNum := parseNumericIdentifier(y)

		switch {
		case xNum && yNum:
			if xn < yn {
				return -1
			}
			if xn > yn {
				return 1
			}
		case xNum && !yNum:
			return -1
		case !xNum && yNum:
			return 1
		default:
			if x < y {
				return -1
			}
			if x > y {
				return 1
			}
		}
	}

	return 0
}

func parseNumericIdentifier(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

func pickAssetForCurrentPlatform(release *githubRelease) (*releaseAsset, error) {
	if release == nil {
		return nil, errors.New("release is nil")
	}

	suffix, ok := platformAssetSuffix()
	if !ok {
		return nil, fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	for i := range release.Assets {
		asset := &release.Assets[i]
		if strings.HasSuffix(strings.ToLower(asset.Name), strings.ToLower(suffix)) && strings.TrimSpace(asset.BrowserDownloadURL) != "" {
			return asset, nil
		}
	}

	return nil, fmt.Errorf("asset suffix %q not found in release %s", suffix, release.TagName)
}

func platformAssetSuffix() (string, bool) {
	switch runtime.GOOS {
	case "windows":
		return fmt.Sprintf("_windows_%s.exe", runtime.GOARCH), true
	case "darwin":
		return fmt.Sprintf("_darwin_%s.dmg", runtime.GOARCH), true
	default:
		return "", false
	}
}

func fetchExpectedChecksum(release *githubRelease, targetAssetName string) (string, error) {
	candidates, err := pickChecksumCandidates(release, targetAssetName)
	if err != nil {
		return "", err
	}

	var failures []string
	for _, candidate := range candidates {
		text, err := downloadSmallTextAsset(candidate.Asset.BrowserDownloadURL)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s(download): %v", candidate.Asset.Name, err))
			continue
		}

		hash, err := parseChecksumTextWithPolicy(text, targetAssetName, candidate.AllowSingleHash)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s(parse): %v", candidate.Asset.Name, err))
			continue
		}
		return hash, nil
	}

	if len(failures) == 0 {
		return "", fmt.Errorf("checksum for %s not found", targetAssetName)
	}
	if len(failures) > 3 {
		failures = failures[:3]
	}
	return "", fmt.Errorf("checksum for %s not found (%s)", targetAssetName, strings.Join(failures, "; "))
}

type checksumCandidate struct {
	Asset           *releaseAsset
	AllowSingleHash bool
}

func pickChecksumCandidates(release *githubRelease, targetAssetName string) ([]checksumCandidate, error) {
	if release == nil {
		return nil, errors.New("release is nil")
	}
	targetAssetName = filepath.Base(strings.TrimSpace(targetAssetName))
	if targetAssetName == "" {
		return nil, errors.New("target asset name is empty")
	}

	targetLower := strings.ToLower(targetAssetName)
	preferred := map[string]struct{}{
		targetLower + ".sha256":     {},
		targetLower + ".sha256sum":  {},
		targetLower + ".sha256.txt": {},
	}

	var exact []checksumCandidate
	var generic []checksumCandidate
	seen := map[string]struct{}{}
	for i := range release.Assets {
		a := &release.Assets[i]
		name := strings.ToLower(strings.TrimSpace(a.Name))
		if name == "" || strings.TrimSpace(a.BrowserDownloadURL) == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		if _, ok := preferred[name]; ok {
			seen[name] = struct{}{}
			exact = append(exact, checksumCandidate{
				Asset:           a,
				AllowSingleHash: true,
			})
			continue
		}
		if strings.Contains(name, targetLower) && isChecksumLikeFile(name) {
			seen[name] = struct{}{}
			exact = append(exact, checksumCandidate{
				Asset:           a,
				AllowSingleHash: true,
			})
			continue
		}
		if isChecksumLikeFile(name) {
			seen[name] = struct{}{}
			generic = append(generic, checksumCandidate{
				Asset:           a,
				AllowSingleHash: false,
			})
			continue
		}
	}

	if len(exact) == 0 && len(generic) == 0 {
		return nil, fmt.Errorf("checksum asset for %s not found", targetAssetName)
	}
	return append(exact, generic...), nil
}

func isChecksumLikeFile(assetName string) bool {
	name := strings.ToLower(strings.TrimSpace(filepath.Base(assetName)))
	if name == "" {
		return false
	}
	if strings.Contains(name, "sha256") || strings.Contains(name, "checksum") {
		return true
	}
	return strings.HasSuffix(name, ".sha256") || strings.HasSuffix(name, ".sha256sum")
}

func parseChecksumText(text, assetName string) (string, error) {
	return parseChecksumTextWithPolicy(text, assetName, true)
}

func parseChecksumTextWithPolicy(text, assetName string, allowSingleHash bool) (string, error) {
	assetName = filepath.Base(strings.TrimSpace(assetName))
	if assetName == "" {
		return "", errors.New("asset name is empty")
	}

	var singleHash string
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 1 && isHexSHA256(fields[0]) {
			if singleHash == "" {
				singleHash = strings.ToLower(fields[0])
			}
			continue
		}
		if len(fields) < 2 || !isHexSHA256(fields[0]) {
			continue
		}

		nameField := fields[len(fields)-1]
		nameField = strings.TrimPrefix(nameField, "*")
		nameField = strings.TrimPrefix(nameField, "./")
		nameField = filepath.Base(nameField)
		if strings.EqualFold(nameField, assetName) {
			return strings.ToLower(fields[0]), nil
		}
	}

	if allowSingleHash && singleHash != "" {
		return singleHash, nil
	}
	return "", fmt.Errorf("checksum for %s not found", assetName)
}

func downloadSmallTextAsset(url string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", defaultUserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("checksum download failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func isHexSHA256(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

func downloadAsset(url, assetName string) (string, error) {
	assetName = filepath.Base(strings.TrimSpace(assetName))
	if assetName == "" {
		assetName = "update_artifact"
	}

	updateDir := filepath.Join(config.DataDir(), "updates")
	if err := os.MkdirAll(updateDir, 0755); err != nil {
		return "", err
	}

	targetPath := filepath.Join(updateDir, assetName)
	tempPath := targetPath + ".part"
	_ = os.Remove(tempPath)

	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", defaultUserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("download failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	file, err := os.Create(tempPath)
	if err != nil {
		return "", err
	}

	if _, err := io.Copy(file, resp.Body); err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", err
	}

	if err := os.Rename(tempPath, targetPath); err != nil {
		_ = os.Remove(tempPath)
		return "", err
	}

	return targetPath, nil
}

func verifyFileSHA256(path, expectedLowerHex string) error {
	expectedLowerHex = strings.ToLower(strings.TrimSpace(expectedLowerHex))
	if !isHexSHA256(expectedLowerHex) {
		return fmt.Errorf("invalid expected sha256: %q", expectedLowerHex)
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := fmt.Sprintf("%x", h.Sum(nil))
	if actual != expectedLowerHex {
		return fmt.Errorf("sha256 mismatch, expected %s got %s", expectedLowerHex, actual)
	}
	return nil
}

func applyUpdateForPlatform(artifactPath string) error {
	switch runtime.GOOS {
	case "windows":
		return applyWindowsUpdate(artifactPath)
	case "darwin":
		return applyDarwinUpdate(artifactPath)
	default:
		return fmt.Errorf("platform %s is not supported for self-update", runtime.GOOS)
	}
}

func applyWindowsUpdate(downloadedExe string) error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Ext(downloadedExe), ".exe") {
		return fmt.Errorf("expected .exe artifact, got %s", downloadedExe)
	}

	updateDir := filepath.Join(config.DataDir(), "updates")
	if err := os.MkdirAll(updateDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(config.LogsDir(), 0755); err != nil {
		return err
	}

	scriptPath := filepath.Join(updateDir, "apply_update.bat")
	logPath := filepath.Join(config.LogsDir(), "updater.log")
	script := `@echo off
setlocal
set "OLD_PID=%~1"
set "NEW_EXE=%~2"
set "TARGET_EXE=%~3"
set "LOG_FILE=%~4"
set "TMP_EXE=%TARGET_EXE%.new"
set "BAK_EXE=%TARGET_EXE%.bak"

:waitloop
tasklist /FI "PID eq %OLD_PID%" | find "%OLD_PID%" >nul
if %ERRORLEVEL%==0 (
  timeout /t 1 /nobreak >nul
  goto waitloop
)

copy /Y "%NEW_EXE%" "%TMP_EXE%" >nul
if %ERRORLEVEL% neq 0 (
  echo [%DATE% %TIME%] stage copy failed >> "%LOG_FILE%"
  exit /b 1
)

if exist "%BAK_EXE%" del /F /Q "%BAK_EXE%" >nul 2>nul
if exist "%TARGET_EXE%" ren "%TARGET_EXE%" "%~n3%~x3.bak"
if %ERRORLEVEL% neq 0 (
  echo [%DATE% %TIME%] backup rename failed >> "%LOG_FILE%"
  del /F /Q "%TMP_EXE%" >nul 2>nul
  exit /b 1
)

move /Y "%TMP_EXE%" "%TARGET_EXE%" >nul
if %ERRORLEVEL% neq 0 (
  echo [%DATE% %TIME%] promote failed >> "%LOG_FILE%"
  if exist "%BAK_EXE%" move /Y "%BAK_EXE%" "%TARGET_EXE%" >nul 2>nul
  exit /b 1
)

if exist "%BAK_EXE%" del /F /Q "%BAK_EXE%" >nul 2>nul
start "" "%TARGET_EXE%"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return err
	}

	cmd := exec.Command("cmd", "/C", scriptPath, strconv.Itoa(os.Getpid()), downloadedExe, exePath, logPath)
	if err := cmd.Start(); err != nil {
		return err
	}

	time.AfterFunc(restartDelay, func() { os.Exit(0) })
	return nil
}

func applyDarwinUpdate(downloadedDMG string) error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Ext(downloadedDMG), ".dmg") {
		return fmt.Errorf("expected .dmg artifact, got %s", downloadedDMG)
	}

	bundlePath := currentAppBundlePath(exePath)
	if bundlePath == "" {
		return errors.New("current process is not inside .app bundle; cannot auto-apply dmg")
	}

	updateDir := filepath.Join(config.DataDir(), "updates")
	if err := os.MkdirAll(updateDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(config.LogsDir(), 0755); err != nil {
		return err
	}

	scriptPath := filepath.Join(updateDir, "apply_update.sh")
	logPath := filepath.Join(config.LogsDir(), "updater.log")
	targetDir := filepath.Dir(bundlePath)
	appName := filepath.Base(bundlePath)

	script := `#!/bin/bash
set -euo pipefail

OLD_PID="$1"
DMG_PATH="$2"
TARGET_DIR="$3"
APP_NAME="$4"
LOG_FILE="$5"

while kill -0 "$OLD_PID" >/dev/null 2>&1; do
  sleep 1
done

MOUNT_POINT="$(mktemp -d /tmp/animate_update_mount.XXXXXX)"
cleanup() {
  hdiutil detach "$MOUNT_POINT" -quiet >/dev/null 2>&1 || true
  rm -rf "$MOUNT_POINT"
}
trap cleanup EXIT

hdiutil attach "$DMG_PATH" -nobrowse -mountpoint "$MOUNT_POINT" -quiet
SRC_APP="$MOUNT_POINT/$APP_NAME"
if [ ! -d "$SRC_APP" ]; then
  SRC_APP="$(find "$MOUNT_POINT" -maxdepth 1 -name "*.app" | head -n 1)"
fi

if [ -z "$SRC_APP" ] || [ ! -d "$SRC_APP" ]; then
  echo "[$(date)] source app not found in dmg" >> "$LOG_FILE"
  exit 1
fi

TARGET_APP="$TARGET_DIR/$APP_NAME"
STAGE_APP="$TARGET_DIR/.update-$APP_NAME"
BACKUP_APP="$TARGET_DIR/.backup-$APP_NAME"

rm -rf "$STAGE_APP" "$BACKUP_APP" || true
cp -R "$SRC_APP" "$STAGE_APP"

if [ -d "$TARGET_APP" ]; then
  mv "$TARGET_APP" "$BACKUP_APP"
fi

if ! mv "$STAGE_APP" "$TARGET_APP"; then
  echo "[$(date)] promote failed, restoring backup" >> "$LOG_FILE"
  rm -rf "$TARGET_APP" || true
  if [ -d "$BACKUP_APP" ]; then
    mv "$BACKUP_APP" "$TARGET_APP" || true
  fi
  exit 1
fi

rm -rf "$BACKUP_APP" || true
open "$TARGET_APP"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return err
	}

	cmd := exec.Command("/bin/bash", scriptPath, strconv.Itoa(os.Getpid()), downloadedDMG, targetDir, appName, logPath)
	if err := cmd.Start(); err != nil {
		return err
	}

	time.AfterFunc(restartDelay, func() { os.Exit(0) })
	return nil
}

func currentAppBundlePath(executablePath string) string {
	clean := filepath.Clean(executablePath)
	normalized := filepath.ToSlash(clean)
	marker := ".app/Contents/MacOS/"
	idx := strings.Index(normalized, marker)
	if idx < 0 {
		return ""
	}
	return normalized[:idx+4]
}
