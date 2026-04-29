package updater

import (
	"strings"
	"testing"
)

func TestIsHexSHA256(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{strings.Repeat("a", 64), true},
		{strings.Repeat("F", 64), true},
		{"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", true},
		{strings.Repeat("a", 63), false},
		{strings.Repeat("a", 65), false},
		{strings.Repeat("g", 64), false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isHexSHA256(tc.s); got != tc.want {
			t.Errorf("isHexSHA256(len=%d) = %v, want %v", len(tc.s), got, tc.want)
		}
	}
}

func TestIsChecksumLikeFile(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"animate-server_v0.5.3_linux_amd64.tar.gz.sha256", true},
		{"SHA256SUMS.txt", true},
		{"checksums.txt", true},
		{"animate-server.exe", false},
		{"foo.tar.gz", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isChecksumLikeFile(tc.name); got != tc.want {
			t.Errorf("isChecksumLikeFile(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestParseChecksumTextSkipsCommentsAndUnrelated(t *testing.T) {
	text := strings.Join([]string{
		"# header comment",
		"",
		strings.Repeat("a", 64) + "  other.tar.gz",
		strings.Repeat("b", 64) + "  animate-server_v0.5.3_linux_amd64.tar.gz",
	}, "\n")

	hash, err := parseChecksumText(text, "animate-server_v0.5.3_linux_amd64.tar.gz")
	if err != nil {
		t.Fatalf("parseChecksumText: %v", err)
	}
	if hash != strings.Repeat("b", 64) {
		t.Fatalf("got %q", hash)
	}
}

func TestParseChecksumTextStarPrefixAndPathStripped(t *testing.T) {
	text := strings.Repeat("d", 64) + " *./dist/animate-server.tar.gz"
	hash, err := parseChecksumText(text, "animate-server.tar.gz")
	if err != nil {
		t.Fatalf("parseChecksumText: %v", err)
	}
	if hash != strings.Repeat("d", 64) {
		t.Fatalf("got %q", hash)
	}
}

func TestPickAssetForCurrentPlatformNilAndNoMatch(t *testing.T) {
	if _, err := pickAssetForCurrentPlatform(nil); err == nil {
		t.Fatal("expected error on nil release")
	}

	release := &githubRelease{
		TagName: "v0.5.3",
		Assets:  []releaseAsset{{Name: "weird_thing.zip", BrowserDownloadURL: "https://x"}},
	}
	if _, err := pickAssetForCurrentPlatform(release); err == nil {
		t.Fatal("expected error when no candidate matches")
	}
}

func TestNormalizeVersionEmpty(t *testing.T) {
	if normalizeVersion("") != versionZero {
		t.Fatal("expected versionZero on empty")
	}
	if normalizeVersion("0.5.3") != "v0.5.3" {
		t.Fatal("expected v prefix added")
	}
	if normalizeVersion("v0.5.3") != "v0.5.3" {
		t.Fatal("expected v prefix preserved")
	}
}

func TestIsBuildVersionUnset(t *testing.T) {
	cases := map[string]bool{
		"":            true,
		"dev":         true,
		"DEVELOPMENT": true,
		"unknown":     true,
		"v0.5.3":      false,
		"0.5.3":       false,
	}
	for in, want := range cases {
		if got := isBuildVersionUnset(in); got != want {
			t.Errorf("isBuildVersionUnset(%q) = %v, want %v", in, got, want)
		}
	}
}
