package api

import (
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

func TestPopulateSubscriptionLibraryStateFindsStrongNonExactTitle(t *testing.T) {
	sub := model.Subscription{
		Title:  "从后面来的神威先生",
		RSSUrl: "https://example.test/kanui-san",
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	anime := model.LocalAnime{
		Title:            "从后面来的神威先生 第一季",
		Path:             "/library/从后面来的神威先生 第一季",
		JellyfinSeriesID: "kanui-san-series",
	}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("create local anime: %v", err)
	}
	episode := model.LocalEpisode{
		LocalAnimeID:   anime.ID,
		Title:          "第 1 集",
		EpisodeNum:     1,
		SeasonNum:      1,
		Path:           "/library/从后面来的神威先生 第一季/S01E01.mkv",
		JellyfinItemID: "kanui-san-episode-1",
	}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatalf("create local episode: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DB.Unscoped().Delete(&episode).Error
		_ = db.DB.Unscoped().Delete(&anime).Error
		_ = db.DB.Unscoped().Delete(&sub).Error
	})

	populateSubscriptionLibraryState(&sub)

	if sub.LocalAnimeID != anime.ID {
		t.Fatalf("expected local anime %d, got %d", anime.ID, sub.LocalAnimeID)
	}
	if sub.LibraryEpisodeCount != 1 {
		t.Fatalf("expected one library episode, got %d", sub.LibraryEpisodeCount)
	}
	if !sub.Playable {
		t.Fatal("expected matched local series to be playable from the subscription card")
	}
}

func TestFindSubscriptionLocalAnimesRejectsMetadataOnlyCollision(t *testing.T) {
	metadata := model.AnimeMetadata{
		Title:   "转生成猫的大叔",
		TitleCN: "转生成猫的大叔",
	}
	if err := db.DB.Create(&metadata).Error; err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	sub := model.Subscription{
		Title:      "遭到流放的转生重骑士凭借游戏知识大开无双",
		RSSUrl:     "https://example.test/heavy-knight",
		MetadataID: &metadata.ID,
		Metadata:   &metadata,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	anime := model.LocalAnime{
		Title:            "转生成猫的大叔",
		Path:             "/library/转生成猫的大叔 (2024)",
		MetadataID:       &metadata.ID,
		JellyfinSeriesID: "wrong-series",
	}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("create local anime: %v", err)
	}
	episode := model.LocalEpisode{
		LocalAnimeID: anime.ID,
		Title:        "第 1 集",
		EpisodeNum:   1,
		SeasonNum:    1,
		Path:         "/library/转生成猫的大叔 (2024)/S01E01.mkv",
	}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatalf("create local episode: %v", err)
	}
	t.Cleanup(func() {
		_ = db.DB.Unscoped().Delete(&episode).Error
		_ = db.DB.Unscoped().Delete(&anime).Error
		_ = db.DB.Unscoped().Delete(&sub).Error
		_ = db.DB.Unscoped().Delete(&metadata).Error
	})

	matches, err := findSubscriptionLocalAnimes(&sub)
	if err != nil {
		t.Fatalf("find local matches: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("metadata-only collision must not make another series playable: %+v", matches)
	}
}

func TestPopulateSubscriptionStatsSharesPreloadedLibraryIndexAcrossNonExactTitles(t *testing.T) {
	anime := model.LocalAnime{
		Title:            "Shared Show Season 2",
		Path:             "/library/Shared Show Season 2",
		JellyfinSeriesID: "shared-show-series",
	}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("create local anime: %v", err)
	}
	episodes := []model.LocalEpisode{
		{
			LocalAnimeID:   anime.ID,
			Title:          "Shared Show - 01",
			EpisodeNum:     1,
			SeasonNum:      2,
			Path:           "/library/Shared Show Season 2/S02E01.mkv",
			JellyfinItemID: "shared-show-episode-1",
		},
		{
			LocalAnimeID: anime.ID,
			Title:        "Shared Show - 02",
			EpisodeNum:   2,
			SeasonNum:    2,
			Path:         "/library/Shared Show Season 2/S02E02.mkv",
		},
	}
	for i := range episodes {
		if err := db.DB.Create(&episodes[i]).Error; err != nil {
			t.Fatalf("create local episode %d: %v", i, err)
		}
	}
	subs := []model.Subscription{
		{Title: "Shared Show", RSSUrl: "https://example.test/shared-show"},
		{Title: "Shared Show 2026", RSSUrl: "https://example.test/shared-show-2026"},
	}
	for i := range subs {
		if err := db.DB.Create(&subs[i]).Error; err != nil {
			t.Fatalf("create subscription %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		_ = db.DB.Unscoped().Where("subscription_id IN ?", []uint{subs[0].ID, subs[1].ID}).Delete(&model.SubscriptionResource{}).Error
		_ = db.DB.Unscoped().Delete(&subs[0]).Error
		_ = db.DB.Unscoped().Delete(&subs[1]).Error
		_ = db.DB.Unscoped().Where("local_anime_id = ?", anime.ID).Delete(&model.LocalEpisode{}).Error
		_ = db.DB.Unscoped().Delete(&anime).Error
	})

	libraryIndex, err := loadSubscriptionLibraryIndex()
	if err != nil {
		t.Fatalf("load shared library index: %v", err)
	}
	if _, ok := libraryIndex.statsByAnime[anime.ID]; !ok {
		t.Fatalf("local anime %d was not included in the shared library index", anime.ID)
	}
	for i := range subs {
		populateSubscriptionStatWithLibraryIndex(&subs[i], libraryIndex)
		if subs[i].LocalAnimeID != anime.ID {
			t.Fatalf("subscription %d matched local anime %d, want %d", i, subs[i].LocalAnimeID, anime.ID)
		}
		if subs[i].LibraryEpisodeCount != 2 {
			t.Fatalf("subscription %d episode count = %d, want 2", i, subs[i].LibraryEpisodeCount)
		}
		if !subs[i].Playable {
			t.Fatalf("subscription %d should be playable", i)
		}
	}
}
