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
