package service

import (
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/model"
)

func TestSelectMetadataSourcePrefersHigherPriorityOnTie(t *testing.T) {
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
	}
	if selected.name != "tmdb" {
		t.Fatalf("expected tmdb to win tie-breaker, got %s", selected.name)
	}
}

func TestFallbackSummaryForSource(t *testing.T) {
	metadata := &model.AnimeMetadata{
		TMDBSummary:    "tmdb summary",
		AniListSummary: "anilist summary",
		BangumiSummary: "bangumi summary",
	}

	if got := fallbackSummaryForSource("bangumi", metadata); got != "tmdb summary" {
		t.Fatalf("expected bangumi fallback to use tmdb summary, got %q", got)
	}
	if got := fallbackSummaryForSource("tmdb", metadata); got != "anilist summary" {
		t.Fatalf("expected tmdb fallback to use anilist summary, got %q", got)
	}
	if got := fallbackSummaryForSource("anilist", metadata); got != "tmdb summary" {
		t.Fatalf("expected anilist fallback to use tmdb summary, got %q", got)
	}
}
