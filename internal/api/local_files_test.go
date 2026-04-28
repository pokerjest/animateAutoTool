package api

import (
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

func TestBackfillRenamedDownloadLogUpdatesTargetAndStatus(t *testing.T) {
	if err := db.DB.Exec("DELETE FROM download_logs").Error; err != nil {
		t.Fatalf("failed to clear download_logs: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM subscriptions").Error; err != nil {
		t.Fatalf("failed to clear subscriptions: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM anime_metadata").Error; err != nil {
		t.Fatalf("failed to clear anime_metadata: %v", err)
	}

	meta := model.AnimeMetadata{Title: "轻松熊"}
	if err := db.DB.Create(&meta).Error; err != nil {
		t.Fatalf("failed to create metadata: %v", err)
	}
	sub := model.Subscription{
		Title:      "轻松熊",
		RSSUrl:     "https://example.com/rilakkuma/rss",
		MetadataID: &meta.ID,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}
	logEntry := model.DownloadLog{
		SubscriptionID: sub.ID,
		Title:          "轻松熊 01",
		Episode:        "01",
		SeasonVal:      "S01",
		Status:         "downloading",
	}
	if err := db.DB.Create(&logEntry).Error; err != nil {
		t.Fatalf("failed to create download log: %v", err)
	}

	anime := model.LocalAnime{Title: "轻松熊", MetadataID: &meta.ID}
	episode := model.LocalEpisode{EpisodeNum: 1, SeasonNum: 1}
	backfillRenamedDownloadLog(anime, episode, "/tmp/old.mp4", "/tmp/new.mp4")

	var updated model.DownloadLog
	if err := db.DB.First(&updated, logEntry.ID).Error; err != nil {
		t.Fatalf("failed to reload download log: %v", err)
	}
	if updated.TargetFile != "/tmp/new.mp4" {
		t.Fatalf("expected target file to be updated, got %q", updated.TargetFile)
	}
	if updated.Status != "completed" {
		t.Fatalf("expected status completed, got %q", updated.Status)
	}
}
