package store

import (
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

func setupSubscriptionStore(t *testing.T) *SubscriptionStore {
	t.Helper()
	db.InitDB(":memory:")
	t.Cleanup(func() {
		_ = db.CloseDB()
		db.DB = nil
	})
	return NewSubscriptionStore(db.DB)
}

func TestSubscriptionStoreNilSafety(t *testing.T) {
	s := NewSubscriptionStore(nil)
	if _, err := s.GetByID(1); err != gorm.ErrInvalidDB {
		t.Fatalf("GetByID with nil db: got %v", err)
	}
	if _, err := s.GetByIDWithMetadata(1); err != gorm.ErrInvalidDB {
		t.Fatalf("GetByIDWithMetadata with nil db: got %v", err)
	}
	if _, err := s.ListWithMetadata(); err != gorm.ErrInvalidDB {
		t.Fatalf("ListWithMetadata with nil db: got %v", err)
	}
	if _, err := s.ListActive(); err != gorm.ErrInvalidDB {
		t.Fatalf("ListActive with nil db: got %v", err)
	}
	if err := s.Save(&model.Subscription{}); err != gorm.ErrInvalidDB {
		t.Fatalf("Save with nil db: got %v", err)
	}
}

func TestSubscriptionStoreSaveAndGetByID(t *testing.T) {
	s := setupSubscriptionStore(t)

	sub := &model.Subscription{Title: "Show A", RSSUrl: "https://example.com/a.xml", IsActive: true}
	if err := s.Save(sub); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if sub.ID == 0 {
		t.Fatal("expected non-zero ID after save")
	}

	got, err := s.GetByID(sub.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Title != "Show A" || got.RSSUrl != "https://example.com/a.xml" {
		t.Fatalf("unexpected sub: %+v", got)
	}

	if _, err := s.GetByID(99999); err != gorm.ErrRecordNotFound {
		t.Fatalf("GetByID missing: expected ErrRecordNotFound, got %v", err)
	}
}

func TestSubscriptionStoreGetByIDWithMetadata(t *testing.T) {
	s := setupSubscriptionStore(t)

	meta := &model.AnimeMetadata{Title: "M1"}
	if err := db.DB.Create(meta).Error; err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	sub := &model.Subscription{Title: "Show B", RSSUrl: "https://example.com/b.xml", MetadataID: &meta.ID}
	if err := s.Save(sub); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.GetByIDWithMetadata(sub.ID)
	if err != nil {
		t.Fatalf("GetByIDWithMetadata: %v", err)
	}
	if got.Metadata == nil {
		t.Fatal("expected preloaded Metadata, got nil")
	}
	if got.Metadata.Title != "M1" {
		t.Fatalf("expected metadata title M1, got %q", got.Metadata.Title)
	}
}

func TestSubscriptionStoreListVariants(t *testing.T) {
	s := setupSubscriptionStore(t)

	if err := s.Save(&model.Subscription{Title: "Active", RSSUrl: "https://example.com/1.xml", IsActive: true}); err != nil {
		t.Fatalf("save active: %v", err)
	}
	if err := s.Save(&model.Subscription{Title: "Inactive", RSSUrl: "https://example.com/2.xml", IsActive: false}); err != nil {
		t.Fatalf("save inactive: %v", err)
	}

	all, err := s.ListWithMetadata()
	if err != nil {
		t.Fatalf("ListWithMetadata: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 subs, got %d", len(all))
	}

	active, err := s.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].Title != "Active" {
		t.Fatalf("expected only Active sub, got %+v", active)
	}
}
