package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
)

const releaseManifestAssetName = "animate-release-manifest.json"

type ReleaseManifest struct {
	FormatVersion            int    `json:"format_version"`
	Version                  string `json:"version"`
	Channel                  string `json:"channel"`
	SchemaVersion            string `json:"schema_version"`
	DatabaseFormat           int    `json:"database_format,omitempty"`
	SchemaFormat             int    `json:"schema_format,omitempty"`
	MinUpgradeFrom           string `json:"min_upgrade_from"`
	MinReadableSchema        string `json:"min_readable_schema"`
	MaxReadableSchema        string `json:"max_readable_schema"`
	SwitchableFromPrerelease bool   `json:"switchable_from_prerelease"`
	RollbackSupported        bool   `json:"rollback_supported"`
	RollbackScope            string `json:"rollback_scope,omitempty"`
}

func (m ReleaseManifest) validForRelease(version string) error {
	if m.FormatVersion != 1 {
		return errors.New("release manifest format is unsupported")
	}
	if !ValidReleaseVersion(m.Version) || normalizeVersion(m.Version) != normalizeVersion(version) {
		return errors.New("release manifest version does not match release tag")
	}
	if m.Channel != string(ReleaseChannelStable) && m.Channel != string(ReleaseChannelBeta) {
		return errors.New("release manifest channel is invalid")
	}
	versionPrerelease := parseSemVer(normalizeVersion(version)).Prerelease != ""
	if versionPrerelease != (m.Channel == string(ReleaseChannelBeta)) {
		return errors.New("release manifest channel does not match release version")
	}
	if !validSchemaVersion(m.SchemaVersion) {
		return errors.New("release manifest schema version is missing")
	}
	if m.DatabaseFormat <= 0 || m.SchemaFormat <= 0 {
		return errors.New("release manifest database/schema format is missing")
	}
	if m.DatabaseFormat != db.DatabaseFormat || m.SchemaFormat != db.SchemaFormat {
		return errors.New("release manifest database/schema format is unsupported")
	}
	if m.RollbackSupported && strings.TrimSpace(m.RollbackScope) != "bundle_snapshot" {
		return errors.New("rollback-supported releases must declare bundle_snapshot scope")
	}
	if scope := strings.TrimSpace(m.RollbackScope); scope != "" && scope != "bundle_snapshot" && scope != "database_only" {
		return errors.New("release manifest rollback scope is invalid")
	}
	if !validSchemaVersion(m.MinReadableSchema) || !validSchemaVersion(m.MaxReadableSchema) {
		return errors.New("release manifest readable schema range is invalid")
	}
	if schemaNumber(m.MinReadableSchema) > schemaNumber(m.MaxReadableSchema) {
		return errors.New("release manifest readable schema range is reversed")
	}
	if strings.TrimSpace(m.MinUpgradeFrom) != "" && !ValidReleaseVersion(m.MinUpgradeFrom) {
		return errors.New("release manifest minimum upgrade version is invalid")
	}
	return nil
}

func (m ReleaseManifest) allows(currentVersion, currentSchema string, targetVersion string) (bool, string) {
	currentVersion = normalizeVersion(currentVersion)
	targetVersion = normalizeVersion(targetVersion)
	if m.DatabaseFormat != db.DatabaseFormat || m.SchemaFormat != db.SchemaFormat {
		return false, "目标版本 database/schema format 与当前程序不兼容"
	}
	if !schemaInRange(currentSchema, m.MinReadableSchema, m.MaxReadableSchema) {
		return false, "当前数据库 schema 不在目标版本可读取范围内"
	}
	if compareVersions(currentVersion, targetVersion) < 0 {
		if strings.TrimSpace(m.MinUpgradeFrom) != "" && compareVersions(currentVersion, m.MinUpgradeFrom) < 0 {
			return false, fmt.Sprintf("至少需要从 %s 升级", m.MinUpgradeFrom)
		}
		return true, ""
	}
	if parseSemVer(currentVersion).Prerelease == "" {
		// Stable releases are never offered as arbitrary downgrades.
		return false, "不允许从稳定版降级"
	}
	if parseSemVer(targetVersion).Prerelease != "" || m.Channel != string(ReleaseChannelStable) {
		return false, "只允许从测试版回切到兼容清单声明支持的稳定版"
	}
	if !m.SwitchableFromPrerelease {
		return false, "该稳定版未声明支持测试版回切"
	}
	return true, ""
}

func validSchemaVersion(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && schemaNumber(value) >= 0
}

func schemaInRange(current, min, max string) bool {
	value := schemaNumber(current)
	if value < 0 {
		return false
	}
	if min != "" && value < schemaNumber(min) {
		return false
	}
	if max != "" && value > schemaNumber(max) {
		return false
	}
	return true
}

func schemaNumber(value string) int {
	value = strings.TrimSpace(value)
	if index := strings.IndexByte(value, '_'); index >= 0 {
		value = value[:index]
	}
	if index := strings.IndexByte(value, '-'); index >= 0 {
		value = value[:index]
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return number
}

func currentSchemaVersion() string {
	if db.DB == nil {
		return ""
	}
	return db.CurrentSchemaVersion(db.DB)
}

func findManifestAsset(release *githubRelease) (*releaseAsset, error) {
	if release == nil {
		return nil, errors.New("release is nil")
	}
	for i := range release.Assets {
		if strings.EqualFold(strings.TrimSpace(release.Assets[i].Name), releaseManifestAssetName) {
			return &release.Assets[i], nil
		}
	}
	return nil, errors.New("release compatibility manifest is missing")
}

func fetchReleaseManifest(release *githubRelease) (ReleaseManifest, error) {
	asset, err := findManifestAsset(release)
	if err != nil {
		return ReleaseManifest{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return ReleaseManifest{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", defaultUserAgent)
	resp, err := updaterHTTPClient(httpTimeout).Do(req)
	if err != nil {
		return ReleaseManifest{}, err
	}
	defer safeio.Close(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return ReleaseManifest{}, fmt.Errorf("manifest download returned HTTP %d", resp.StatusCode)
	}
	var manifest ReleaseManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return ReleaseManifest{}, err
	}
	if err := manifest.validForRelease(release.TagName); err != nil {
		return ReleaseManifest{}, err
	}
	return manifest, nil
}

func compatibilityForRelease(release githubRelease, current string) (ReleaseManifest, bool, string) {
	manifest, err := fetchReleaseManifest(&release)
	if err != nil {
		return ReleaseManifest{}, false, "缺少有效的版本兼容清单，禁止自动更新"
	}
	allowed, reason := manifest.allows(current, currentSchemaVersion(), release.TagName)
	return manifest, allowed, reason
}
