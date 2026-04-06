package updater

import "testing"

func TestCompareVersions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		a    string
		b    string
		want int
	}{
		{a: "v0.4.4", b: "v0.4.4", want: 0},
		{a: "0.4.3", b: "v0.4.4", want: -1},
		{a: "v0.5.0", b: "v0.4.9", want: 1},
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
	if got := normalizeVersion(""); got != "v0.0.0" {
		t.Fatalf("normalizeVersion empty failed: %q", got)
	}
}

func TestPickAssetForCurrentPlatform(t *testing.T) {
	t.Parallel()

	suffix, ok := platformAssetSuffix()
	if !ok {
		t.Skip("current platform not supported by updater")
	}

	release := &githubRelease{
		TagName: "v0.4.4",
		Assets: []releaseAsset{
			{Name: "unrelated_asset.zip", BrowserDownloadURL: "https://example.com/unrelated"},
			{Name: "animate-server_v0.4.4" + suffix, BrowserDownloadURL: "https://example.com/matched"},
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
