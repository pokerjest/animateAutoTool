package service

import (
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

func TestRecalculateSubscriptionProgressIgnoresArchivedAndFailedLogs(t *testing.T) {
	withServiceTestDB(t)

	sub := model.Subscription{
		Title:    "Progress Show",
		RSSUrl:   "https://example.test/progress",
		IsActive: true,
		LastEp:   8,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	logs := []model.DownloadLog{
		{SubscriptionID: sub.ID, Title: "Progress Show - 01", Episode: "01", Status: downloadLogStatusCompleted},
		{SubscriptionID: sub.ID, Title: "Progress Show - 02", Episode: "02", Status: downloadLogStatusDownloading},
		{SubscriptionID: sub.ID, Title: "Progress Show - 06", Episode: "06", Status: downloadLogStatusFailed},
		{SubscriptionID: sub.ID, Title: "Progress Show - 08", Episode: "08", Status: downloadLogStatusArchived},
	}
	if err := db.DB.Create(&logs).Error; err != nil {
		t.Fatalf("create download logs: %v", err)
	}

	updated, err := recalculateSubscriptionProgress([]model.Subscription{sub})
	if err != nil {
		t.Fatalf("recalculate progress: %v", err)
	}
	if updated != 1 {
		t.Fatalf("expected one updated subscription, got %d", updated)
	}

	var got model.Subscription
	if err := db.DB.First(&got, sub.ID).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if got.LastEp != 2 {
		t.Fatalf("expected last episode 2 from active history, got %d", got.LastEp)
	}
}
