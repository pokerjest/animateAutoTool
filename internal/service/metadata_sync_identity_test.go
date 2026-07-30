package service

import (
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

func TestSyncMetadataToModelsPreservesSourceIdentityTitles(t *testing.T) {
	withServiceTestDB(t)

	metadata := model.AnimeMetadata{
		Title:   "网络元数据标题",
		Image:   "poster.jpg",
		Summary: "summary",
		AirDate: "2026-07-30",
	}
	if err := db.DB.Create(&metadata).Error; err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	sub := model.Subscription{
		Title:      "RSS 原始番剧标题",
		RSSUrl:     "https://example.test/source-title",
		MetadataID: &metadata.ID,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	anime := model.LocalAnime{
		Title:      "本地目录标题",
		Path:       "/media/本地目录标题",
		MetadataID: &metadata.ID,
	}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("create local anime: %v", err)
	}

	NewMetadataService().SyncMetadataToModels(&metadata)

	var freshSub model.Subscription
	if err := db.DB.First(&freshSub, sub.ID).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if freshSub.Title != "RSS 原始番剧标题" {
		t.Fatalf("metadata sync must not overwrite RSS identity title, got %q", freshSub.Title)
	}
	if freshSub.Image != metadata.Image || freshSub.Summary != metadata.Summary {
		t.Fatalf("display fields were not synchronized: %+v", freshSub)
	}

	var freshAnime model.LocalAnime
	if err := db.DB.First(&freshAnime, anime.ID).Error; err != nil {
		t.Fatalf("reload local anime: %v", err)
	}
	if freshAnime.Title != "本地目录标题" {
		t.Fatalf("metadata sync must not overwrite directory identity title, got %q", freshAnime.Title)
	}
	if freshAnime.Image != metadata.Image || freshAnime.Summary != metadata.Summary || freshAnime.AirDate != metadata.AirDate {
		t.Fatalf("local display fields were not synchronized: %+v", freshAnime)
	}
}
