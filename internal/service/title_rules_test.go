package service

import (
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/model"
)

func TestTitlesLookRelatedMatchesLocalizedVariants(t *testing.T) {
	if !titlesLookRelated("SPY x FAMILY 间谍家家酒 Season 3", "间谍过家家 第三季") {
		t.Fatal("expected localized title variants to be related")
	}
}

func TestTitlesStronglyRelatedRejectsSharedGenreWord(t *testing.T) {
	if titlesStronglyRelated(
		"遭到流放的转生重骑士凭借游戏知识大开无双",
		"转生成猫的大叔",
	) {
		t.Fatal("a shared genre word must not prove series identity")
	}
	if !titlesStronglyRelated("Repair Show Season 1", "Repair Show") {
		t.Fatal("season suffix should preserve a strong identity match")
	}
}

func TestNormalizedRuleTitleRemovesSeasonNoise(t *testing.T) {
	got := normalizedRuleTitle("[ANi] Candy Caries / CANDY CARIES 蛀在糖糖里 Season 1")
	if got == "" {
		t.Fatal("expected normalized title to keep useful content")
	}
	if got == normalizedRuleTitle("Season 1") {
		t.Fatal("expected normalization to preserve series title, not just season marker")
	}
}

func TestSubscriptionLocalTitleMatchScorePrefersExactLocalizedTitle(t *testing.T) {
	sub := &model.Subscription{Title: "与奔驰于透明之夜的你，谈一场看不见的恋爱。"}
	exact := &model.LocalAnime{Title: "与奔驰于透明之夜的你，谈一场看不见的恋爱。"}
	traditional := &model.LocalAnime{Title: "與奔馳於透明之夜的你，談一場看不見的戀愛。"}

	if got := SubscriptionLocalTitleMatchScore(sub, exact); got != 100 {
		t.Fatalf("exact title score = %d, want 100", got)
	}
	if got := SubscriptionLocalTitleMatchScore(sub, traditional); got >= 100 {
		t.Fatalf("traditional variant score = %d, must not tie the exact title", got)
	}
}
