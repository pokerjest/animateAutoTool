package store

import (
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

func TestSubscriptionStoreCountFamilyNilSafety(t *testing.T) {
	s := NewSubscriptionStore(nil)
	if _, err := s.Count(); err != gorm.ErrInvalidDB {
		t.Errorf("Count nil: got %v", err)
	}
	if _, err := s.CountActive(); err != gorm.ErrInvalidDB {
		t.Errorf("CountActive nil: got %v", err)
	}
	if _, err := s.CountAutoDisabledOnDone(); err != gorm.ErrInvalidDB {
		t.Errorf("CountAutoDisabledOnDone nil: got %v", err)
	}
	if _, err := s.CountStaleSince(time.Now()); err != gorm.ErrInvalidDB {
		t.Errorf("CountStaleSince nil: got %v", err)
	}
	if _, err := s.ListActiveByIDs(nil); err != gorm.ErrInvalidDB {
		t.Errorf("ListActiveByIDs nil: got %v", err)
	}
	if _, err := s.ListWithStaleStrategy(); err != gorm.ErrInvalidDB {
		t.Errorf("ListWithStaleStrategy nil: got %v", err)
	}
	if _, err := s.ListAll(); err != gorm.ErrInvalidDB {
		t.Errorf("ListAll nil: got %v", err)
	}
}

func TestSubscriptionStoreCountAndAuto(t *testing.T) {
	s := setupSubscriptionStore(t)

	// 2 active, 1 inactive, 1 active+done
	mustSave(t, s, &model.Subscription{Title: "A1", RSSUrl: "u1", IsActive: true, ExpectedEpisodes: 12, LastEp: 6})
	mustSave(t, s, &model.Subscription{Title: "A2", RSSUrl: "u2", IsActive: true, ExpectedEpisodes: 0, LastEp: 0})
	mustSave(t, s, &model.Subscription{Title: "INACTIVE", RSSUrl: "u3", IsActive: false, AutoDisableOnDone: true, ExpectedEpisodes: 12, LastEp: 12})
	mustSave(t, s, &model.Subscription{Title: "ACTIVE_RUNNING", RSSUrl: "u4", IsActive: true, ExpectedEpisodes: 12, LastEp: 11})

	if got, _ := s.Count(); got != 4 {
		t.Errorf("Count = %d, want 4", got)
	}
	if got, _ := s.CountActive(); got != 3 {
		t.Errorf("CountActive = %d, want 3", got)
	}
	if got, _ := s.CountAutoDisabledOnDone(); got != 1 {
		t.Errorf("CountAutoDisabledOnDone = %d, want 1", got)
	}

	all, err := s.ListAll()
	if err != nil || len(all) != 4 {
		t.Fatalf("ListAll: %v / %d", err, len(all))
	}
}

func TestSubscriptionStoreCountStaleSince(t *testing.T) {
	s := setupSubscriptionStore(t)

	old := time.Now().Add(-100 * time.Hour)
	fresh := time.Now().Add(-1 * time.Hour)
	mustSave(t, s, &model.Subscription{Title: "Stale", RSSUrl: "u1", IsActive: true, StaleAfterHours: 24, LastSuccessAt: &old})
	mustSave(t, s, &model.Subscription{Title: "Fresh", RSSUrl: "u2", IsActive: true, StaleAfterHours: 24, LastSuccessAt: &fresh})
	mustSave(t, s, &model.Subscription{Title: "NoStrategy", RSSUrl: "u3", IsActive: true, StaleAfterHours: 0, LastSuccessAt: &old})
	mustSave(t, s, &model.Subscription{Title: "Inactive", RSSUrl: "u4", IsActive: false, StaleAfterHours: 24, LastSuccessAt: &old})

	got, err := s.CountStaleSince(time.Now().Add(-72 * time.Hour))
	if err != nil {
		t.Fatalf("CountStaleSince: %v", err)
	}
	if got != 1 {
		t.Fatalf("expected 1 stale (only 'Stale'), got %d", got)
	}
}

func TestSubscriptionStoreListActiveByIDsAndStaleStrategy(t *testing.T) {
	s := setupSubscriptionStore(t)

	a := &model.Subscription{Title: "A", RSSUrl: "u1", IsActive: true}
	mustSave(t, s, a)
	b := &model.Subscription{Title: "B", RSSUrl: "u2", IsActive: false}
	mustSave(t, s, b)
	old := time.Now().Add(-100 * time.Hour)
	c := &model.Subscription{Title: "C", RSSUrl: "u3", IsActive: true, StaleAfterHours: 24, LastSuccessAt: &old}
	mustSave(t, s, c)

	// Empty input short-circuits.
	if got, err := s.ListActiveByIDs(nil); err != nil || got != nil {
		t.Errorf("empty IDs: %v / %v", got, err)
	}
	if got, err := s.ListActiveByIDs([]uint{}); err != nil || got != nil {
		t.Errorf("empty slice: %v / %v", got, err)
	}

	got, err := s.ListActiveByIDs([]uint{a.ID, b.ID, c.ID})
	if err != nil {
		t.Fatalf("ListActiveByIDs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 active subs, got %d (%+v)", len(got), got)
	}

	stale, err := s.ListWithStaleStrategy()
	if err != nil {
		t.Fatalf("ListWithStaleStrategy: %v", err)
	}
	if len(stale) != 1 || stale[0].ID != c.ID {
		t.Fatalf("expected only C, got %+v", stale)
	}
}

func mustSave(t *testing.T, s *SubscriptionStore, sub *model.Subscription) {
	t.Helper()
	if err := s.Save(sub); err != nil {
		t.Fatalf("Save: %v", err)
	}
	_ = db.DB // keep linter quiet
}
