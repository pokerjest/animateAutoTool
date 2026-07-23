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
