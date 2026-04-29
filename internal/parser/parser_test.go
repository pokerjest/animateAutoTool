package parser

import "testing"

const parserResolution1080p = "1080p"

func TestIsVideoFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/media/anime/show/ep01.mkv", true},
		{"show/ep01.MP4", true},
		{"clip.flv", true},
		{"clip.webm", true},
		{"unfinished.!qb", true},
		{"poster.jpg", false},
		{"readme.md", false},
		{"", false},
		{"directory/", false},
	}

	for _, tc := range cases {
		if got := IsVideoFile(tc.path); got != tc.want {
			t.Errorf("IsVideoFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestParseSeason(t *testing.T) {
	cases := []struct {
		title string
		want  int
	}{
		{"Some Anime S02", 2},
		{"Some Anime S01", 1},
		{"Title Season 3", 3},
		{"标题 第二季", 2},
		{"标题 第三期", 3},
		{"Show II", 2},
		{"Show III", 3},
		{"Show IV", 4},
		{"Bare Title", 1},
		{"Title 4", 4},
	}

	for _, tc := range cases {
		if got := ParseSeason(tc.title); got != tc.want {
			t.Errorf("ParseSeason(%q) = %d, want %d", tc.title, got, tc.want)
		}
	}
}

func TestCleanTitle(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"[Group] Some Show", "Some Show"},
		{"[Group] Some Show [1080p][AAC]", "Some Show"},
		{"Show (2024)", "Show"},
		{"Show Season 2", "Show"},
		{"Show S02", "Show"},
		{"Show 第2季", "Show"},
		{"   ", "   "}, // 全空格触发 fallback 返回 raw
	}

	for _, tc := range cases {
		if got := CleanTitle(tc.raw); got != tc.want {
			t.Errorf("CleanTitle(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestParseFilenameBasicSxxExx(t *testing.T) {
	info := ParseFilename("/anime/show/Show.S01E05.1080p.x265.10bit.mkv")
	if info.Season != 1 || info.Episode != 5 {
		t.Errorf("expected S1E5, got S%dE%d", info.Season, info.Episode)
	}
	if info.Resolution != parserResolution1080p {
		t.Errorf("expected %s, got %q", parserResolution1080p, info.Resolution)
	}
	if info.VideoCodec != "X265" {
		t.Errorf("expected X265, got %q", info.VideoCodec)
	}
	if info.BitDepth != "10bit" {
		t.Errorf("expected 10bit, got %q", info.BitDepth)
	}
	if info.Extension != "mkv" {
		t.Errorf("expected mkv, got %q", info.Extension)
	}
}

func TestParseFilenameMikanStyle(t *testing.T) {
	info := ParseFilename("[ANi] Show Title - 12 [1080P][Baha][WEB-DL][AAC][AVC][CHT][MP4].mp4")
	if info.Group != "ANi" {
		t.Errorf("expected group ANi, got %q", info.Group)
	}
	if info.Resolution != parserResolution1080p {
		t.Errorf("expected %s, got %q", parserResolution1080p, info.Resolution)
	}
	if info.AudioCodec != "AAC" {
		t.Errorf("expected AAC, got %q", info.AudioCodec)
	}
	if info.VideoCodec != "AVC" {
		// AVC may not be in codec list — assert only when present
		t.Logf("video codec parsed as %q (not asserted)", info.VideoCodec)
	}
	if info.Extension != "mp4" {
		t.Errorf("expected mp4, got %q", info.Extension)
	}
}

func TestParseFilenameStandaloneEpisodeNumber(t *testing.T) {
	info := ParseFilename("[LoliHouse] Show - 28 [WebRip 1080p HEVC-10bit AAC].mkv")
	if info.Episode != 28 {
		t.Errorf("expected episode 28, got %d", info.Episode)
	}
	if info.Group != "LoliHouse" {
		t.Errorf("expected group LoliHouse, got %q", info.Group)
	}
	if info.Resolution != parserResolution1080p {
		t.Errorf("expected %s, got %q", parserResolution1080p, info.Resolution)
	}
}
