package updater

import (
	"runtime"
	"testing"
)

func releaseWithCurrentPlatformAsset(version string, prerelease bool) githubRelease {
	candidates := platformAssetCandidates(runtime.GOOS, runtime.GOARCH, false)
	assets := []releaseAsset{}
	if len(candidates) > 0 {
		assets = append(assets, releaseAsset{
			Name:               "AnimateAutoTool_" + normalizeVersion(version) + candidates[0],
			BrowserDownloadURL: "https://example.test/" + version,
		})
	}
	return githubRelease{
		TagName:     version,
		HTMLURL:     "https://example.test/releases/" + version,
		PublishedAt: "2026-07-29T00:00:00Z",
		Prerelease:  prerelease,
		Assets:      assets,
	}
}

func TestParseReleaseChannel(t *testing.T) {
	t.Parallel()

	if channel, err := ParseReleaseChannel(""); err != nil || channel != ReleaseChannelStable {
		t.Fatalf("empty channel = %q, %v", channel, err)
	}
	if channel, err := ParseReleaseChannel("BETA"); err != nil || channel != ReleaseChannelBeta {
		t.Fatalf("beta channel = %q, %v", channel, err)
	}
	if _, err := ParseReleaseChannel("nightly"); err == nil {
		t.Fatal("expected unsupported channel to fail")
	}
}

func TestValidReleaseVersion(t *testing.T) {
	t.Parallel()

	valid := []string{"v0.9.8", "0.9.8-beta.1", "v0.9.2.1", "v1.2.3-rc.2+build.7"}
	for _, version := range valid {
		if !ValidReleaseVersion(version) {
			t.Errorf("expected %q to be valid", version)
		}
	}
	invalid := []string{"", "latest", "v1.2", "v1.2.3/asset", "v1.2.3 beta", "v1.2.3-", "v1.2.3+@"}
	for _, version := range invalid {
		if ValidReleaseVersion(version) {
			t.Errorf("expected %q to be invalid", version)
		}
	}
}

func TestBuildReleaseCatalogFiltersAndSortsChannels(t *testing.T) {
	t.Parallel()

	releases := []githubRelease{
		releaseWithCurrentPlatformAsset("v0.9.8-beta.1", true),
		releaseWithCurrentPlatformAsset("v0.9.7", false),
		releaseWithCurrentPlatformAsset("v0.9.9", false),
		{TagName: "v1.0.0", Draft: true},
	}

	stable := buildReleaseCatalog(releases, ReleaseChannelStable, "v0.9.7")
	if len(stable.Items) != 2 {
		t.Fatalf("stable items = %#v", stable.Items)
	}
	if stable.Items[0].Version != "v0.9.9" || !stable.Items[0].NewerThanCurrent {
		t.Fatalf("unexpected stable order: %#v", stable.Items)
	}

	beta := buildReleaseCatalog(releases, ReleaseChannelBeta, "v0.9.7")
	if len(beta.Items) != 1 {
		t.Fatalf("beta items = %#v", beta.Items)
	}
	if beta.Items[0].Version != "v0.9.8-beta.1" || !beta.Items[0].Prerelease {
		t.Fatalf("unexpected beta catalog: %#v", beta.Items)
	}
}

func TestFindPublishedReleaseByVersionIsExact(t *testing.T) {
	t.Parallel()

	releases := []githubRelease{
		releaseWithCurrentPlatformAsset("v0.9.8-beta.1", true),
		releaseWithCurrentPlatformAsset("v0.9.8", false),
		{TagName: "v0.9.9", Draft: true},
	}
	release, err := findPublishedReleaseByVersion(releases, "0.9.8-beta.1")
	if err != nil || release.TagName != "v0.9.8-beta.1" {
		t.Fatalf("exact release = %#v, %v", release, err)
	}
	if _, err := findPublishedReleaseByVersion(releases, "v0.9.9"); err == nil {
		t.Fatal("expected draft release to be rejected")
	}
}
