package api

import (
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

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
	t.Cleanup(func() {
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
