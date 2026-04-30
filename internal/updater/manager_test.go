package updater

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const updaterVersionZero = "v0.0.0"
const updaterResultRunning = "running"

func TestCompareVersions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		a    string
		b    string
		want int
	}{
		{a: "v0.4.4", b: "v0.4.4", want: 0},
		{a: "0.4.3", b: "v0.4.4", want: -1},
		{a: "v0.5.2", b: "v0.5.1", want: 1},
		{a: "v1.0.0-beta.1", b: "v1.0.0", want: -1},
		{a: "v1.0.0", b: "v1.0.0-beta.1", want: 1},
		{a: "v2.1", b: "v2.1.0", want: 0},
	}

	for _, tc := range cases {
		got := compareVersions(tc.a, tc.b)
		if got != tc.want {
			t.Fatalf("compareVersions(%q, %q)=%d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestNormalizeVersion(t *testing.T) {
	t.Parallel()

	if got := normalizeVersion("0.1.2"); got != "v0.1.2" {
		t.Fatalf("normalizeVersion failed: %q", got)
	}
	if got := normalizeVersion(""); got != updaterVersionZero {
		t.Fatalf("normalizeVersion empty failed: %q", got)
	}
}

func TestPickAssetForCurrentPlatform(t *testing.T) {
	t.Parallel()

	candidates := platformAssetCandidates(runtime.GOOS, runtime.GOARCH, false)
	if len(candidates) == 0 {
		t.Skip("current platform not supported by updater")
	}

	release := &githubRelease{
		TagName: "v0.4.4",
		Assets: []releaseAsset{
			{Name: "unrelated_asset.zip", BrowserDownloadURL: "https://example.com/unrelated"},
			{Name: "animate-server_v0.4.4" + candidates[0], BrowserDownloadURL: "https://example.com/matched"},
		},
	}

	asset, err := pickAssetForCurrentPlatform(release)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if asset == nil || asset.BrowserDownloadURL != "https://example.com/matched" {
		t.Fatalf("unexpected selected asset: %#v", asset)
	}
}

func TestPlatformAssetCandidates(t *testing.T) {
	t.Parallel()

	if got := platformAssetCandidates("linux", "amd64", false); len(got) != 1 || got[0] != "_linux_amd64.tar.gz" {
		t.Fatalf("unexpected linux candidates: %#v", got)
	}
	if got := platformAssetCandidates("windows", "amd64", false); len(got) != 1 || got[0] != "_windows_amd64.exe" {
		t.Fatalf("unexpected windows candidates: %#v", got)
	}
	if got := platformAssetCandidates("darwin", "arm64", false); len(got) != 2 || got[0] != "_darwin_arm64.tar.gz" || got[1] != "_darwin_arm64.dmg" {
		t.Fatalf("unexpected darwin archive-first candidates: %#v", got)
	}
	if got := platformAssetCandidates("darwin", "arm64", true); len(got) != 2 || got[0] != "_darwin_arm64.dmg" || got[1] != "_darwin_arm64.tar.gz" {
		t.Fatalf("unexpected darwin app-bundle candidates: %#v", got)
	}
}

func TestPickLatestPublishedRelease(t *testing.T) {
	t.Parallel()

	release, err := pickLatestPublishedRelease([]githubRelease{
		{TagName: "v1.2.0-beta.1", Draft: true},
		{TagName: "v1.2.0-beta.2", Prerelease: true},
		{TagName: "v1.1.9"},
	}, false)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if release == nil || release.TagName != "v1.1.9" {
		t.Fatalf("unexpected release picked: %#v", release)
	}
}

func TestPickLatestPublishedReleaseIncludePrerelease(t *testing.T) {
	t.Parallel()

	release, err := pickLatestPublishedRelease([]githubRelease{
		{TagName: "v1.2.0-beta.2", Prerelease: true},
		{TagName: "v1.1.9"},
	}, true)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if release == nil || release.TagName != "v1.2.0-beta.2" {
		t.Fatalf("unexpected release picked: %#v", release)
	}
}

func TestCurrentVersionWantsPrerelease(t *testing.T) {
	t.Parallel()

	if !currentVersionWantsPrerelease("v1.2.0-beta.2") {
		t.Fatal("expected prerelease version to opt into prerelease updates")
	}
	if currentVersionWantsPrerelease("v1.2.0") {
		t.Fatal("did not expect stable version to opt into prerelease updates")
	}
}

func TestExtractBinaryFromTarGz(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	archivePath := filepath.Join(dir, "artifact.tar.gz")
	destPath := filepath.Join(dir, "animate-server")
	payload := []byte("binary-content")

	file, err := os.Create(filepath.Clean(archivePath)) //nolint:gosec // archivePath is created under t.TempDir().
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	header := &tar.Header{
		Name: "animate-server_v0.5.2_linux_amd64/bin/animate-server",
		Mode: 0o755,
		Size: int64(len(payload)),
	}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}

	if err := extractBinaryFromTarGz(archivePath, "animate-server", destPath); err != nil {
		t.Fatalf("extractBinaryFromTarGz: %v", err)
	}

	got, err := os.ReadFile(filepath.Clean(destPath)) //nolint:gosec // destPath is extracted under t.TempDir().
	if err != nil {
		t.Fatalf("read extracted binary: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("unexpected extracted content: %q", string(got))
	}
}

func TestParseChecksumText(t *testing.T) {
	t.Parallel()

	asset := "animate-server_v0.4.4_windows_amd64.exe"
	hash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	text := hash + "  *" + asset + "\n"
	got, err := parseChecksumText(text, asset)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != hash {
		t.Fatalf("expected %s, got %s", hash, got)
	}
}

func TestParseChecksumTextSingleHash(t *testing.T) {
	t.Parallel()

	hash := "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"
	got, err := parseChecksumText(hash+"\n", "some_asset")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != hash {
		t.Fatalf("expected %s, got %s", hash, got)
	}
}

func TestParseChecksumTextWithPolicyDisallowSingleHash(t *testing.T) {
	t.Parallel()

	hash := "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"
	_, err := parseChecksumTextWithPolicy(hash+"\n", "some_asset", false)
	if err == nil {
		t.Fatal("expected error when single-hash fallback is disabled")
	}
}

func TestPickChecksumCandidatesPriority(t *testing.T) {
	t.Parallel()

	target := "animate-server_v0.4.4_windows_amd64.exe"
	release := &githubRelease{
		Assets: []releaseAsset{
			{Name: "SHA256SUMS.txt", BrowserDownloadURL: "https://example.com/generic"},
			{Name: target + ".sha256", BrowserDownloadURL: "https://example.com/exact"},
			{Name: "other.exe.sha256", BrowserDownloadURL: "https://example.com/other"},
		},
	}

	candidates, err := pickChecksumCandidates(release, target)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(candidates) < 2 {
		t.Fatalf("expected at least 2 candidates, got %d", len(candidates))
	}
	if candidates[0].Asset.BrowserDownloadURL != "https://example.com/exact" {
		t.Fatalf("unexpected first candidate: %#v", candidates[0].Asset)
	}
	if !candidates[0].AllowSingleHash {
		t.Fatal("expected exact checksum candidate to allow single hash")
	}
}

func TestApplySettingsLockedClearsRepoScopedCache(t *testing.T) {
	t.Parallel()

	m := &Manager{
		status: Status{
			RepoOwner: "owner-a",
			RepoName:  "repo-a",
		},
		etag:            "etag-value",
		cachedRelease:   &githubRelease{TagName: "v1.0.0"},
		cachedRepoOwner: "owner-a",
		cachedRepoName:  "repo-a",
	}

	m.applySettingsLocked(settings{
		RepoOwner: "owner-b",
		RepoName:  "repo-b",
	})

	if m.etag != "" {
		t.Fatalf("expected etag to be cleared, got %q", m.etag)
	}
	if m.cachedRelease != nil {
		t.Fatal("expected cached release to be cleared")
	}
	if m.cachedRepoOwner != "" || m.cachedRepoName != "" {
		t.Fatalf("expected cached repo identity reset, got %q/%q", m.cachedRepoOwner, m.cachedRepoName)
	}
}

func TestBeginRunInitialProgress(t *testing.T) {
	t.Parallel()

	m := &Manager{}
	cfg := settings{
		Enabled:         true,
		AutoApplyEnable: true,
		RequireChecksum: true,
		IntervalMinutes: defaultIntervalMinutes,
		RepoOwner:       defaultRepoOwner,
		RepoName:        defaultRepoName,
	}

	snapshot, started := m.beginRun(cfg, "manual-pull", true)
	if !started {
		t.Fatal("expected beginRun to start")
	}
	if !snapshot.Running {
		t.Fatal("expected running status")
	}
	if snapshot.ProgressPhase != "准备下载更新" {
		t.Fatalf("unexpected phase: %q", snapshot.ProgressPhase)
	}
	if snapshot.LastResult != updaterResultRunning {
		t.Fatalf("unexpected result: %q", snapshot.LastResult)
	}
	if snapshot.ProgressPercent != 0 {
		t.Fatalf("expected zero percent, got %d", snapshot.ProgressPercent)
	}
}
