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

func TestSyncMetadataToModelsBlocksConflictingProviderPropagation(t *testing.T) {
	withServiceTestDB(t)

	metadata := model.AnimeMetadata{
		Title:        "无职转生 第三季",
		Image:        "new-wrong-poster.jpg",
		Summary:      "new wrong summary",
		BangumiID:    277554,
		BangumiTitle: "无职转生 第三季",
		TMDBID:       94664,
		TMDBTitle:    "From Overshadowed to Overpowered",
	}
	if err := db.DB.Create(&metadata).Error; err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	sub := model.Subscription{
		Title:      "无职转生 第三季",
		RSSUrl:     "https://example.test/conflicting-propagation",
		Image:      "subscription-original.jpg",
		Summary:    "subscription original",
		MetadataID: &metadata.ID,
	}
	anime := model.LocalAnime{
		Title:      "From Overshadowed to Overpowered",
		Path:       "/media/From Overshadowed to Overpowered",
		Image:      "local-original.jpg",
		Summary:    "local original",
		MetadataID: &metadata.ID,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("create local anime: %v", err)
	}

	NewMetadataService().SyncMetadataToModels(&metadata)

	var freshSub model.Subscription
	var freshAnime model.LocalAnime
	if err := db.DB.First(&freshSub, sub.ID).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if err := db.DB.First(&freshAnime, anime.ID).Error; err != nil {
		t.Fatalf("reload local anime: %v", err)
	}
	if freshSub.Image != sub.Image || freshSub.Summary != sub.Summary {
		t.Fatalf("conflicting metadata propagated into subscription: %+v", freshSub)
	}
	if freshAnime.Image != anime.Image || freshAnime.Summary != anime.Summary {
		t.Fatalf("conflicting metadata propagated into local anime: %+v", freshAnime)
	}
}

func TestSyncMetadataToModelsQuarantinesUnrelatedSharedLink(t *testing.T) {
	withServiceTestDB(t)

	metadata := model.AnimeMetadata{
		Title:        "Original Show",
		Image:        "original-show.jpg",
		Summary:      "original summary",
		BangumiID:    777,
		BangumiTitle: "Original Show",
	}
	if err := db.DB.Create(&metadata).Error; err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	matching := model.LocalAnime{
		Title:      "Original Show",
		Path:       "/media/Original Show",
		MetadataID: &metadata.ID,
	}
	unrelated := model.LocalAnime{
		Title:      "Different Series",
		Path:       "/media/Different Series",
		Image:      "keep.jpg",
		Summary:    "keep summary",
		MetadataID: &metadata.ID,
	}
	if err := db.DB.Create(&matching).Error; err != nil {
		t.Fatalf("create matching local anime: %v", err)
	}
	if err := db.DB.Create(&unrelated).Error; err != nil {
		t.Fatalf("create unrelated local anime: %v", err)
	}

	NewMetadataService().SyncMetadataToModels(&metadata)

	var freshMatching, freshUnrelated model.LocalAnime
	if err := db.DB.First(&freshMatching, matching.ID).Error; err != nil {
		t.Fatalf("reload matching local anime: %v", err)
	}
	if err := db.DB.First(&freshUnrelated, unrelated.ID).Error; err != nil {
		t.Fatalf("reload unrelated local anime: %v", err)
	}
	if freshMatching.MetadataID == nil || *freshMatching.MetadataID != metadata.ID ||
		freshMatching.Image != metadata.Image || freshMatching.Summary != metadata.Summary {
		t.Fatalf("matching link was not preserved and synchronized: %+v", freshMatching)
	}
	if freshUnrelated.MetadataID != nil {
		t.Fatalf("unrelated shared link was not quarantined: %+v", freshUnrelated.MetadataID)
	}
	if freshUnrelated.Image != unrelated.Image || freshUnrelated.Summary != unrelated.Summary {
		t.Fatalf("unrelated local display fields were overwritten: %+v", freshUnrelated)
	}
}
