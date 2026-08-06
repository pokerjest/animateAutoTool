package api

import (
	"fmt"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

func TestSubscriptionCandidateIndexNarrowsLargeLibrary(t *testing.T) {
	localAnimes := make([]model.LocalAnime, 0, 10001)
	for i := 0; i < 10000; i++ {
		anime := model.LocalAnime{
			Title: fmt.Sprintf("Unrelated Catalog Series %05d", i),
			Path:  fmt.Sprintf("/library/Unrelated Catalog Series %05d", i),
		}
		anime.ID = uint(i + 1)
		localAnimes = append(localAnimes, anime)
	}
	target := model.LocalAnime{Title: "Shared Show Season 2", Path: "/library/Shared Show Season 2"}
	target.ID = 20000
	localAnimes = append(localAnimes, target)
	index := &subscriptionLibraryIndex{
		localAnimes:           localAnimes,
		candidateIndexesByKey: make(map[string][]int),
	}
	buildSubscriptionCandidateIndex(index)

	sub := &model.Subscription{Title: "Shared Show 2026"}
	candidates := subscriptionLocalAnimeCandidates(sub, index)
	if len(candidates) > 20 {
		t.Fatalf("candidate index returned %d of %d local series", len(candidates), len(localAnimes))
	}
	matches := filterSubscriptionLocalAnimes(sub, candidates)
	if len(matches) != 1 || matches[0].ID != target.ID {
		t.Fatalf("indexed strong match = %+v, want local anime %d", matches, target.ID)
	}
}

func TestSubscriptionCandidateIndexKeepsShortTitleFallback(t *testing.T) {
	anime := model.LocalAnime{Title: "猫", Path: "/library/猫"}
	anime.ID = 1
	index := &subscriptionLibraryIndex{localAnimes: []model.LocalAnime{anime}, candidateIndexesByKey: make(map[string][]int)}
	buildSubscriptionCandidateIndex(index)
	matches := filterSubscriptionLocalAnimes(&model.Subscription{Title: "猫"}, subscriptionLocalAnimeCandidates(&model.Subscription{Title: "猫"}, index))
	if len(matches) != 1 || matches[0].ID != anime.ID {
		t.Fatalf("short-title fallback lost match: %+v", matches)
	}
}

func TestBackfillSubscriptionMetadataRequiresUpdatedRow(t *testing.T) {
	firstMetadata := model.AnimeMetadata{Title: "Shared Show A"}
	secondMetadata := model.AnimeMetadata{Title: "Shared Show B"}
	if err := db.DB.Create(&firstMetadata).Error; err != nil {
		t.Fatalf("create first metadata: %v", err)
	}
	if err := db.DB.Create(&secondMetadata).Error; err != nil {
		t.Fatalf("create second metadata: %v", err)
	}
	anime := model.LocalAnime{Title: "Shared Show", Path: "/library/Shared Show", MetadataID: &firstMetadata.ID}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("create local anime: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DB.Unscoped().Delete(&anime).Error
		_ = db.DB.Unscoped().Delete(&firstMetadata).Error
		_ = db.DB.Unscoped().Delete(&secondMetadata).Error
	})

	staleCopy := anime
	staleCopy.MetadataID = nil
	sub := &model.Subscription{Title: "Shared Show", MetadataID: &secondMetadata.ID}
	result := backfillSubscriptionLocalAnimeMetadata(sub, []model.LocalAnime{staleCopy})
	if result[0].MetadataID != nil {
		t.Fatalf("zero-row update was reported as successful metadata backfill: %d", *result[0].MetadataID)
	}
	var stored model.LocalAnime
	if err := db.DB.First(&stored, anime.ID).Error; err != nil {
		t.Fatalf("reload local anime: %v", err)
	}
	if stored.MetadataID == nil || *stored.MetadataID != firstMetadata.ID {
		t.Fatalf("stored metadata changed after rejected backfill: %+v", stored.MetadataID)
	}
}

func TestBackfillSubscriptionMetadataRequiresIndependentIdentity(t *testing.T) {
	metadata := model.AnimeMetadata{
		Title:     "目标订阅",
		BangumiID: 590786,
		TMDBID:    302051,
	}
	if err := db.DB.Create(&metadata).Error; err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	sub := &model.Subscription{
		Title:      "目标订阅",
		MetadataID: &metadata.ID,
	}
	anime := &model.LocalAnime{
		Title: "完全无关番剧",
		Path:  "/library/完全无关番剧",
	}
	if err := db.DB.Create(anime).Error; err != nil {
		t.Fatalf("create local anime: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DB.Unscoped().Delete(anime).Error
		_ = db.DB.Unscoped().Delete(&metadata).Error
	})

	result := backfillSubscriptionLocalAnimeMetadata(sub, []model.LocalAnime{*anime})
	if result[0].MetadataID != nil {
		t.Fatalf("unrelated local anime must not receive subscription metadata: %d", *result[0].MetadataID)
	}
	var stored model.LocalAnime
	if err := db.DB.First(&stored, anime.ID).Error; err != nil {
		t.Fatalf("reload local anime: %v", err)
	}
	if stored.MetadataID != nil {
		t.Fatalf("unrelated local anime metadata was persisted: %d", *stored.MetadataID)
	}
}
