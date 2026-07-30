package service

import (
	"encoding/json"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/stretchr/testify/require"
)

func TestSelectMetadataSourceUsesConfiguredPriorityOnTie(t *testing.T) {
	withServiceTestDB(t)
	metadata := &model.AnimeMetadata{
		TMDBID:       1,
		TMDBTitle:    "Spy x Family",
		AniListID:    2,
		AniListTitle: "Spy x Family",
		BangumiID:    3,
		BangumiTitle: "Spy x Family",
	}

	selected := selectMetadataSource("Spy x Family", metadata)
	if selected == nil {
		t.Fatal("expected a selected source")
		return
	}
	if selected.name != "bangumi" {
		t.Fatalf("expected default Bangumi priority to win tie-breaker, got %s", selected.name)
	}
}

func TestFallbackSummaryForSource(t *testing.T) {
	withServiceTestDB(t)
	metadata := &model.AnimeMetadata{
		TMDBSummary:    "tmdb summary",
		AniListSummary: "anilist summary",
		BangumiSummary: "bangumi summary",
	}

	if got := fallbackSummaryForSource("bangumi", metadata); got != "tmdb summary" {
		t.Fatalf("expected bangumi fallback to use tmdb summary, got %q", got)
	}
	if got := fallbackSummaryForSource("tmdb", metadata); got != "bangumi summary" {
		t.Fatalf("expected tmdb fallback to follow configured order, got %q", got)
	}
	if got := fallbackSummaryForSource("anilist", metadata); got != "bangumi summary" {
		t.Fatalf("expected anilist fallback to follow configured order, got %q", got)
	}
}

func TestSetActiveFieldsPreservesLocalNFOAndUsesConfiguredFieldOrder(t *testing.T) {
	withServiceTestDB(t)
	require.NoError(t, db.DB.Create(&model.GlobalConfig{
		Key: model.ConfigKeyMetadataSourceOrder, Value: "anilist,tmdb,bangumi",
	}).Error)

	metadata := &model.AnimeMetadata{
		Title:          "本地手工标题",
		Summary:        "本地手工简介",
		BangumiID:      1,
		BangumiTitle:   "Bangumi title",
		BangumiImage:   "bangumi.jpg",
		BangumiSummary: "bangumi summary",
		TMDBID:         2,
		TMDBTitle:      "TMDB title",
		TMDBImage:      "tmdb.jpg",
		TMDBSummary:    "tmdb summary",
		AniListID:      3,
		AniListTitle:   "AniList title",
		AniListImage:   "anilist.jpg",
		AniListSummary: "anilist summary",
		FieldSources:   `{"title":"local-nfo","summary":"local-nfo"}`,
	}

	NewMetadataService().setActiveFields(metadata, "AniList title")
	require.Equal(t, "本地手工标题", metadata.Title)
	require.Equal(t, "本地手工简介", metadata.Summary)
	require.Equal(t, "anilist.jpg", metadata.Image)

	sources := map[string]string{}
	require.NoError(t, json.Unmarshal([]byte(metadata.FieldSources), &sources))
	require.Equal(t, "local-nfo", sources["title"])
	require.Equal(t, "local-nfo", sources["summary"])
	require.Equal(t, "anilist", sources["image"])
}
