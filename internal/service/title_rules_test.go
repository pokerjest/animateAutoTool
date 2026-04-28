package service

import "testing"

func TestTitlesLookRelatedMatchesLocalizedVariants(t *testing.T) {
	if !titlesLookRelated("SPY x FAMILY 间谍家家酒 Season 3", "间谍过家家 第三季") {
		t.Fatal("expected localized title variants to be related")
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
