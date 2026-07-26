package parser

import (
	"testing"
)

func TestParseTitle(t *testing.T) {
	cases := []struct {
		Title              string
		ExpectedEp         string
		ExpectedGrp        string
		ExpectedResolution string
	}{
		{
			Title:              "[LoliHouse] 葬送的芙莉莲 / Sousou no Frieren - 28 [WebRip 1080p HEVC-10bit AAC][简繁内封字幕]",
			ExpectedEp:         "28",
			ExpectedGrp:        "LoliHouse",
			ExpectedResolution: "1080p",
		},
		{
			Title:              "[ANi] 迷宮飯 - 13 [1080P][Baha][WEB-DL][AAC][AVC][CHT][MP4]",
			ExpectedEp:         "13",
			ExpectedGrp:        "ANi",
			ExpectedResolution: "1080p",
		},
		{
			Title:              "[Moozzi2] Fate/stay night [Unlimited Blade Works] - 25 (BD 1920x1080 x264 Flac) TV-rip",
			ExpectedEp:         "25",
			ExpectedGrp:        "Moozzi2",
			ExpectedResolution: "1080p",
		},
	}

	for _, c := range cases {
		ep := ParseTitle(c.Title)
		if ep.SubGroup != c.ExpectedGrp {
			t.Errorf("Group mismatch for %s: expected %s, got %s", c.Title, c.ExpectedGrp, ep.SubGroup)
		}
		// 启用集数检查
		if ep.EpisodeNum != c.ExpectedEp {
			t.Errorf("Episode mismatch for %s: expected %s, got %s", c.Title, c.ExpectedEp, ep.EpisodeNum)
		}
		if ep.Resolution != c.ExpectedResolution {
			t.Errorf("Resolution mismatch for %s: expected %s, got %s", c.Title, c.ExpectedResolution, ep.Resolution)
		}
	}
}
