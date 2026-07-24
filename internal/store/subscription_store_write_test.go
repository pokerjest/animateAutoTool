package store

import (
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

func TestSubscriptionStoreWriteNilSafety(t *testing.T) {
	s := NewSubscriptionStore(nil)
	if err := s.Create(&model.Subscription{}); err != gorm.ErrInvalidDB {
		t.Fatalf("Create with nil db: got %v", err)
	}
	if _, _, err := s.FindByRSSURLUnscoped("x"); err != gorm.ErrInvalidDB {
		t.Fatalf("FindByRSSURLUnscoped with nil db: got %v", err)
	}
	if err := s.DeleteCascade(1); err != gorm.ErrInvalidDB {
		t.Fatalf("DeleteCascade with nil db: got %v", err)
	}
}

func TestSubscriptionStoreCreateAndFindByRSSURL(t *testing.T) {
	s := setupSubscriptionStore(t)

	sub := &model.Subscription{Title: "Show A", RSSUrl: "https://example.com/a.xml", IsActive: true}
	if err := s.Create(sub); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sub.ID == 0 {
		t.Fatal("expected non-zero ID after create")
	}

	got, found, err := s.FindByRSSURLUnscoped("https://example.com/a.xml")
	if err != nil {
		t.Fatalf("FindByRSSURLUnscoped: %v", err)
	}
	if !found || got == nil || got.ID != sub.ID {
		t.Fatalf("expected to find created sub, got found=%v sub=%+v", found, got)
	}

	_, found, err = s.FindByRSSURLUnscoped("https://example.com/missing.xml")
	if err != nil {
		t.Fatalf("FindByRSSURLUnscoped missing: %v", err)
	}
	if found {
		t.Fatal("expected not found for missing rss url")
	}
}

func TestSubscriptionStoreFindByRSSURLIncludesSoftDeleted(t *testing.T) {
	s := setupSubscriptionStore(t)

	sub := &model.Subscription{Title: "Show B", RSSUrl: "https://example.com/b.xml", IsActive: true}
	if err := s.Create(sub); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Soft-delete via gorm default (DeletedAt).
	if err := db.DB.Delete(&model.Subscription{}, sub.ID).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	got, found, err := s.FindByRSSURLUnscoped("https://example.com/b.xml")
	if err != nil {
		t.Fatalf("FindByRSSURLUnscoped: %v", err)
	}
	if !found || got == nil {
		t.Fatal("expected to find soft-deleted sub via unscoped lookup")
	}
	if !got.DeletedAt.Valid {
		t.Fatal("expected soft-deleted row to report DeletedAt.Valid")
	}
}

func TestSubscriptionStoreDeleteCascade(t *testing.T) {
	s := setupSubscriptionStore(t)

	sub := &model.Subscription{Title: "Show C", RSSUrl: "https://example.com/c.xml", IsActive: true}
	if err := s.Create(sub); err != nil {
		t.Fatalf("Create: %v", err)
	}
	logs := []model.DownloadLog{
		{SubscriptionID: sub.ID, Title: "C - 01", Status: "completed"},
		{SubscriptionID: sub.ID, Title: "C - 02", Status: "failed"},
	}
	if err := db.DB.Create(&logs).Error; err != nil {
		t.Fatalf("create logs: %v", err)
	}

	if err := s.DeleteCascade(sub.ID); err != nil {
		t.Fatalf("DeleteCascade: %v", err)
	}

	// Subscription gone even from unscoped view (hard delete).
	if _, found, err := s.FindByRSSURLUnscoped("https://example.com/c.xml"); err != nil || found {
		t.Fatalf("expected hard-deleted sub, got found=%v err=%v", found, err)
	}
	var logCount int64
	if err := db.DB.Unscoped().Model(&model.DownloadLog{}).Where("subscription_id = ?", sub.ID).Count(&logCount).Error; err != nil {
		t.Fatalf("count logs: %v", err)
	}
	if logCount != 0 {
		t.Fatalf("expected 0 logs after cascade delete, got %d", logCount)
	}
}
