package service

import "testing"

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
