package service

import (
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

func TestSetActiveFieldsPrefersMatchingTMDBOverMismatchedBangumi(t *testing.T) {
	svc := &MetadataService{}
	meta := &model.AnimeMetadata{
		BangumiID:    621449,
		BangumiTitle: "迷失 第六季",
		TMDBID:       312383,
		TMDBTitle:    "Candy Caries 蛀在糖糖里",
		AniListID:    0,
	}

	svc.setActiveFields(meta, "Candy Caries 蛀在糖糖里")

	if meta.Title != "Candy Caries 蛀在糖糖里" {
		t.Fatalf("expected TMDB title to win, got %q", meta.Title)
	}
	if meta.DataSource != "tmdb" {
		t.Fatalf("expected datasource tmdb, got %q", meta.DataSource)
	}
}

func TestSetActiveFieldsUsesV1PosterURL(t *testing.T) {
	svc := &MetadataService{}
	meta := &model.AnimeMetadata{Title: "Poster Test"}
	meta.ID = 42

	svc.setActiveFields(meta, "Poster Test")

	if meta.Image != "/api/v1/posters/42" {
		t.Fatalf("expected v1 poster URL, got %q", meta.Image)
	}
}

func TestShouldApplyBangumiSubjectRejectsMismatchWhenOtherSourceExists(t *testing.T) {
	meta := &model.AnimeMetadata{
		TMDBID:    312383,
		TMDBTitle: "Candy Caries 蛀在糖糖里",
	}
	subject := &bangumi.Subject{
		ID:     621449,
		Name:   "Lost Season 6",
		NameCN: "迷失 第六季",
	}

	if shouldApplyBangumiSubject(meta, subject, "Candy Caries 蛀在糖糖里") {
		t.Fatal("expected mismatched Bangumi subject to be rejected")
	}
}

func TestBestBangumiSearchResultSkipsUnrelatedFirstResult(t *testing.T) {
	results := []bangumi.SearchResult{
		{ID: 1, Name: "デジタル・デビル物語 女神転生", NameCN: "数字恶魔物语 女神转生"},
		{ID: 2, Name: "転生したら第七王子だったので", NameCN: "转生重骑士"},
	}

	result, score := bestBangumiSearchResult(results, []string{"转生重骑士"})
	if result == nil || result.ID != 2 {
		t.Fatalf("expected the related second result, got result=%+v score=%d", result, score)
	}
}

func TestBestBangumiSearchResultRejectsUnrelatedResults(t *testing.T) {
	results := []bangumi.SearchResult{{ID: 1, Name: "Lost Season 6", NameCN: "迷失 第六季"}}
	result, score := bestBangumiSearchResult(results, []string{"转生重骑士"})
	if result != nil {
		t.Fatalf("expected no automatic match, got result=%+v score=%d", result, score)
	}
}

func TestBangumiMatchReferencesPreferIndependentProviderTitles(t *testing.T) {
	meta := &model.AnimeMetadata{
		Title:        "数字恶魔物语 女神转生",
		TitleCN:      "数字恶魔物语 女神转生",
		BangumiTitle: "数字恶魔物语 女神转生",
		TMDBID:       123,
		TMDBTitle:    "转生重骑士",
	}

	references := bangumiMatchReferences(meta, meta.TitleCN)
	if len(references) != 1 || references[0] != "转生重骑士" {
		t.Fatalf("expected only the independent TMDB title, got %#v", references)
	}
}

func TestClearMismatchedBangumiSubjectRemovesStaleDateAndDisplayFields(t *testing.T) {
	svc := &MetadataService{}
	subject := &bangumi.Subject{ID: 1, Name: "Wrong", NameCN: "错误条目", Date: "1987-03-25", Summary: "错误简介"}
	meta := &model.AnimeMetadata{
		Title:          "错误条目",
		TitleCN:        "错误条目",
		TitleJP:        "Wrong",
		AirDate:        "1987-03-25",
		Summary:        "错误简介",
		BangumiID:      1,
		BangumiTitle:   "错误条目",
		BangumiSummary: "错误简介",
		DataSource:     metadataSourceBangumi,
	}

	svc.clearMismatchedBangumiSubject(meta, subject)
	if meta.BangumiID != 0 || meta.BangumiTitle != "" || meta.AirDate != "" || meta.Title != "" || meta.TitleCN != "" {
		t.Fatalf("expected stale Bangumi fields to be cleared, got %+v", meta)
	}
}

func TestMetadataRefreshQueryUsesIndependentTitleForConflictingSources(t *testing.T) {
	meta := &model.AnimeMetadata{
		Title:        "数字恶魔物语 女神转生",
		TitleCN:      "数字恶魔物语 女神转生",
		BangumiID:    1,
		BangumiTitle: "数字恶魔物语 女神转生",
		TMDBID:       2,
		TMDBTitle:    "转生重骑士",
	}
	if !metadataSourcesConflict(meta) {
		t.Fatal("expected the unrelated provider titles to be treated as a conflict")
	}
	if query := metadataRefreshQuery(meta); query != "转生重骑士" {
		t.Fatalf("expected the independent TMDB title for repair, got %q", query)
	}
}

func TestMetadataSourcesConflictAllowsRelatedProviderTitles(t *testing.T) {
	meta := &model.AnimeMetadata{
		BangumiID:    1,
		BangumiTitle: "间谍过家家 第三季",
		TMDBID:       2,
		TMDBTitle:    "SPY x FAMILY 间谍家家酒 Season 3",
	}
	if metadataSourcesConflict(meta) {
		t.Fatal("expected related localized titles not to be marked as a conflict")
	}
}

func TestApplyBangumiSubjectReplacesStaleLocalizedTitles(t *testing.T) {
	svc := &MetadataService{}
	meta := &model.AnimeMetadata{
		TitleCN: "英雄不再2 绝望抗争",
		TitleJP: "No More Heroes 2: Desperate Struggle",
		AirDate: "2010-01-26",
	}
	subject := &bangumi.Subject{
		ID:     520633,
		Name:   "リラックマ",
		NameCN: "轻松熊",
		Date:   "2025-04-03",
	}

	svc.applyBangumiSubject(meta, subject)

	if meta.TitleCN != "轻松熊" {
		t.Fatalf("expected TitleCN to be replaced, got %q", meta.TitleCN)
	}
	if meta.TitleJP != "リラックマ" {
		t.Fatalf("expected TitleJP to be replaced, got %q", meta.TitleJP)
	}
	if meta.AirDate != "2025-04-03" {
		t.Fatalf("expected AirDate to be replaced, got %q", meta.AirDate)
	}
}
