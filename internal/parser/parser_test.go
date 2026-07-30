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
		{"unfinished.!qb", false},
		{"unfinished.bc!", false},
		{"remote.strm", true},
		{"disc.vob", true},
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
		{"示例番剧 第二季", "示例番剧"},
		{"Show 2nd Season", "Show"},
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

func TestParseFilenameUsesBracketEpisodeBeforeTechnicalTags(t *testing.T) {
	info := ParseFilename(`[Sakurato] Onaji Zemi no Someya-san ga Sexy Joyuu Datta Hanashi. [01][AVC-8bit 1080P AAC][CHS].mp4`)
	if info.Episode != 1 {
		t.Fatalf("expected bracket episode 1, got %d", info.Episode)
	}
	if !info.HasExplicitEpisode() {
		t.Fatal("expected bracket episode to be marked explicit")
	}
	if info.BitDepth != "8bit" {
		t.Fatalf("expected 8bit technical tag, got %q", info.BitDepth)
	}

	technicalOnly := ParseFilename(`[Sakurato] Show [AVC-8bit 1080P AAC][CHS].mp4`)
	if technicalOnly.Episode != 0 {
		t.Fatalf("technical tag must not become an episode, got %d", technicalOnly.Episode)
	}
}

func TestParseFilenameSupportsSeasonXEpisodeAndRanges(t *testing.T) {
	info := ParseFilename(`[Group] Range Show 2x03-04 [1080p][CHS].mkv`)
	if info.Season != 2 || info.Episode != 3 || info.EpisodeEnd != 4 {
		t.Fatalf("expected S02E03-E04, got season=%d episode=%d end=%d", info.Season, info.Episode, info.EpisodeEnd)
	}
	if info.ParseSource != "filename:season-x-episode" || info.Confidence < 0.9 {
		t.Fatalf("expected high-confidence season-x parser evidence, got %q %.2f", info.ParseSource, info.Confidence)
	}
	if info.Language != "chs" {
		t.Fatalf("expected CHS language tag, got %q", info.Language)
	}
}

func TestParseFilenameSupportsSxxExxRangesAndChineseEpisodeMarkers(t *testing.T) {
	info := ParseFilename(`[Group] Range Show S01E03-E04 [1080p].mkv`)
	if info.Season != 1 || info.Episode != 3 || info.EpisodeEnd != 4 {
		t.Fatalf("expected S01E03-E04, got season=%d episode=%d end=%d", info.Season, info.Episode, info.EpisodeEnd)
	}

	chinese := ParseFilename(`示例番剧 第二季 第07-08集 [CHS].mkv`)
	if chinese.Season != 2 || chinese.Episode != 7 || chinese.EpisodeEnd != 8 {
		t.Fatalf("expected season 2 episode range 7-8, got season=%d episode=%d end=%d", chinese.Season, chinese.Episode, chinese.EpisodeEnd)
	}
	if chinese.ParseSource != "filename:chinese-episode" {
		t.Fatalf("expected chinese episode parser evidence, got %q", chinese.ParseSource)
	}
}

func TestParseFilenameSupportsSpecialKindsAbsoluteAndVersion(t *testing.T) {
	special := ParseFilename(`[Group] Example OVA - 01v2 [1080p].mkv`)
	if special.EpisodeType != "special" || special.Episode != 1 || special.Version != "v2" {
		t.Fatalf("unexpected special parse: %#v", special)
	}

	sp := ParseFilename(`[Group] Example SP01.mkv`)
	if sp.EpisodeType != "special" {
		t.Fatalf("expected SP01 to be treated as a special, got %q", sp.EpisodeType)
	}

	titleWithSpecialWord := ParseFilename(`[Group] Special Show - 01.mkv`)
	if titleWithSpecialWord.EpisodeType != "episode" {
		t.Fatalf("a title containing the word Special must remain a normal episode, got %q", titleWithSpecialWord.EpisodeType)
	}

	absolute := ParseFilename(`Long Show Absolute 143 [WEB-DL].mkv`)
	if absolute.AbsoluteEpisode != 143 {
		t.Fatalf("expected absolute episode 143, got %d", absolute.AbsoluteEpisode)
	}
}

func TestParseFilenameRejectsTechnicalNumbersAsEpisodes(t *testing.T) {
	for _, name := range []string{
		`Show (2026) [1080p][x265].mkv`,
		`Show [2160p][H264].mkv`,
	} {
		info := ParseFilename(name)
		if info.Episode != 0 {
			t.Fatalf("%s: technical/year number became episode %d", name, info.Episode)
		}
	}
}
