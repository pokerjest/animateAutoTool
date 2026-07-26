package service

import (
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
)

func TestSubscriptionRuleSetMatchesSubtitleGroupWhenTitleLacksGroupName(t *testing.T) {
	rules := buildSubscriptionRuleSet(&model.Subscription{
		Title:         "诈欺游戏",
		SubtitleGroup: "ANi",
		FilterRule:    "ANi",
	})

	episode := parser.Episode{
		Title:    "诈欺游戏 - 01 [1080P]",
		SubGroup: "ANi",
	}

	if !rules.allows(episode) {
		t.Fatal("expected subtitle-group rule to allow episode even when title lacks group name")
	}
}

func TestSubscriptionRuleSetDoesNotMatchAStoredGroupAgainstAnotherGroupsRelease(t *testing.T) {
	rules := buildSubscriptionRuleSet(&model.Subscription{
		Title:         "Test Show",
		SubtitleGroup: "ANi",
		FilterRule:    "ANi",
	})

	episode := parser.Episode{
		Title:    "[Other] Test Show - 01 [1080P]",
		SubGroup: "Other",
	}

	if rules.allows(episode) {
		t.Fatal("expected aggregate fallback release from another group to be rejected")
	}
}

func TestSubscriptionRuleSetFallsBackToLiteralMatchForInvalidRegex(t *testing.T) {
	rules := buildSubscriptionRuleSet(&model.Subscription{
		Title:      "Test Show",
		FilterRule: "[ANi",
	})

	episode := parser.Episode{
		Title: "[ANi] Test Show - 01",
	}

	if !rules.allows(episode) {
		t.Fatal("expected invalid regex to fall back to literal title match")
	}
}

func TestSubscriptionRuleSetExcludeStillWins(t *testing.T) {
	rules := buildSubscriptionRuleSet(&model.Subscription{
		Title:         "Test Show",
		SubtitleGroup: "ANi",
		FilterRule:    "ANi",
		ExcludeRule:   "1080P",
	})

	episode := parser.Episode{
		Title:    "Test Show - 01 [1080P]",
		SubGroup: "ANi",
	}

	if rules.allows(episode) {
		t.Fatal("expected exclude rule to reject the episode")
	}
}

func TestSubscriptionRuleSetAppliesResolutionAndSubtitleLanguage(t *testing.T) {
	tests := []struct {
		name       string
		resolution string
		language   string
		episode    parser.Episode
		want       bool
	}{
		{name: "1080p simplified", resolution: "1080p", language: "chs", episode: parser.Episode{Title: "[ANi] Test - 01 [1080P][CHS]"}, want: true},
		{name: "resolution mismatch", resolution: "1080p", language: "chs", episode: parser.Episode{Title: "[ANi] Test - 01 [720P][CHS]"}, want: false},
		{name: "language mismatch", resolution: "1080p", language: "cht", episode: parser.Episode{Title: "[ANi] Test - 01 [1080P][简体中文]"}, want: false},
		{name: "traditional token", resolution: "1080p", language: "cht", episode: parser.Episode{Title: "[ANi] Test - 01 [1080P][BIG5]"}, want: true},
		{name: "bilingual chinese", language: "chs_cht", episode: parser.Episode{Title: "[LoliHouse] Test - 01 [简繁内封字幕]"}, want: true},
		{name: "bilingual tokens", language: "chs_cht", episode: parser.Episode{Title: "[Group] Test - 01 [CHS&CHT]"}, want: true},
		{name: "file size is not a GB language token", language: "chs", episode: parser.Episode{Title: "[Group] Test - 01 [1.5GB]"}, want: false},
		{name: "spaced file size is not a GB language token", language: "chs", episode: parser.Episode{Title: "[Group] Test - 01 [1.5 GB]"}, want: false},
		{name: "bracketed GB language token", language: "chs", episode: parser.Episode{Title: "[Group] Test - 01 [GB]"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := buildSubscriptionRuleSet(&model.Subscription{
				ResolutionFilter: tt.resolution,
				SubtitleLanguage: tt.language,
			})
			if got := rules.allows(tt.episode); got != tt.want {
				t.Fatalf("allows() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeSubscriptionReleaseFilters(t *testing.T) {
	if got, ok := NormalizeResolutionFilter("4K"); !ok || got != "2160p" {
		t.Fatalf("NormalizeResolutionFilter(4K) = %q, %v", got, ok)
	}
	if _, ok := NormalizeResolutionFilter("1440p"); ok {
		t.Fatal("expected unsupported resolution to be rejected")
	}
	if got, ok := NormalizeSubtitleLanguage("bilingual"); !ok || got != "chs_cht" {
		t.Fatalf("NormalizeSubtitleLanguage(bilingual) = %q, %v", got, ok)
	}
}
