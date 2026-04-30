package store

import (
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

func TestDownloadLogStoreExtraNilSafety(t *testing.T) {
	s := NewDownloadLogStore(nil)
	if _, err := s.ListBySubscription(1, 5); err != gorm.ErrInvalidDB {
		t.Errorf("ListBySubscription nil: got %v", err)
	}
	if _, err := s.ListBySubscriptionAndStatuses(1, []string{statusFailed}); err != gorm.ErrInvalidDB {
		t.Errorf("ListBySubscriptionAndStatuses nil: got %v", err)
	}
	if _, err := s.CountResettable(1, []string{statusFailed}, time.Now()); err != gorm.ErrInvalidDB {
		t.Errorf("CountResettable nil: got %v", err)
	}
	if err := s.MarkResettableArchived(1, []string{statusFailed}, time.Now(), statusArchived); err != gorm.ErrInvalidDB {
		t.Errorf("MarkResettableArchived nil: got %v", err)
	}
	if err := s.UpdateTargetFileByOld("/old", "/new"); err != gorm.ErrInvalidDB {
		t.Errorf("UpdateTargetFileByOld nil: got %v", err)
	}
	if _, err := s.CountByStatus(statusFailed); err != gorm.ErrInvalidDB {
		t.Errorf("CountByStatus nil: got %v", err)
	}
}

func TestDownloadLogStoreCountByStatus(t *testing.T) {
	s := setupDownloadLogStore(t)
	seedLog(t, statusCompleted, "/x", 1, "01")
	seedLog(t, statusCompleted, "/y", 1, "02")
	seedLog(t, statusFailed, "", 1, "03")

	if n, _ := s.CountByStatus(statusCompleted); n != 2 {
		t.Errorf("CountByStatus(completed) = %d, want 2", n)
	}
	if n, _ := s.CountByStatus(statusFailed); n != 1 {
		t.Errorf("CountByStatus(failed) = %d, want 1", n)
	}
	if n, _ := s.CountByStatus("nope"); n != 0 {
		t.Errorf("CountByStatus(nope) = %d, want 0", n)
	}
}

func TestDownloadLogStoreListBySubscription(t *testing.T) {
	s := setupDownloadLogStore(t)

	for i := 0; i < 5; i++ {
		seedLog(t, statusCompleted, "", 1, "ep")
		time.Sleep(2 * time.Millisecond)
	}
	seedLog(t, statusCompleted, "", 2, "ep")

	all, err := s.ListBySubscription(1, 0)
	if err != nil {
		t.Fatalf("ListBySubscription unlimited: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("expected 5 logs for sub 1, got %d", len(all))
	}

	limited, err := s.ListBySubscription(1, 3)
	if err != nil {
		t.Fatalf("ListBySubscription limited: %v", err)
	}
	if len(limited) != 3 {
		t.Fatalf("expected 3 logs after limit, got %d", len(limited))
	}
}

func TestDownloadLogStoreListBySubscriptionAndStatuses(t *testing.T) {
	s := setupDownloadLogStore(t)

	seedLog(t, statusCompleted, "/x/01.mkv", 1, "01")
	seedLog(t, statusFailed, "", 1, "02")
	seedLog(t, statusDownloading, "", 1, "03")
	seedLog(t, statusCompleted, "/x/04.mkv", 2, "01") // wrong sub

	got, err := s.ListBySubscriptionAndStatuses(1, []string{statusCompleted, statusFailed})
	if err != nil {
		t.Fatalf("ListBySubscriptionAndStatuses: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(got))
	}

	if got, _ := s.ListBySubscriptionAndStatuses(1, nil); got != nil {
		t.Errorf("expected nil for empty statuses, got %v", got)
	}
}

func TestDownloadLogStoreCountResettableAndMarkArchived(t *testing.T) {
	s := setupDownloadLogStore(t)

	old := &model.DownloadLog{SubscriptionID: 1, Status: statusFailed, Episode: "01", Title: "old"}
	if err := db.DB.Create(old).Error; err != nil {
		t.Fatalf("create old: %v", err)
	}
	// Backdate created_at
	if err := db.DB.Model(old).UpdateColumn("created_at", time.Now().Add(-50*time.Hour)).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}

	fresh := &model.DownloadLog{SubscriptionID: 1, Status: statusFailed, Episode: "02", Title: "fresh"}
	if err := db.DB.Create(fresh).Error; err != nil {
		t.Fatalf("create fresh: %v", err)
	}

	cutoff := time.Now().Add(-24 * time.Hour)
	count, err := s.CountResettable(1, []string{statusFailed}, cutoff)
	if err != nil {
		t.Fatalf("CountResettable: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected only 1 resettable (the old one), got %d", count)
	}

	// Empty statuses short-circuits.
	if got, _ := s.CountResettable(1, nil, cutoff); got != 0 {
		t.Errorf("CountResettable empty statuses: %d", got)
	}
	if err := s.MarkResettableArchived(1, nil, cutoff, statusArchived); err != nil {
		t.Errorf("MarkResettableArchived empty statuses: %v", err)
	}

	if err := s.MarkResettableArchived(1, []string{statusFailed}, cutoff, statusArchived); err != nil {
		t.Fatalf("MarkResettableArchived: %v", err)
	}

	var refreshed model.DownloadLog
	if err := db.DB.First(&refreshed, old.ID).Error; err != nil {
		t.Fatalf("reload old: %v", err)
	}
	if refreshed.Status != statusArchived {
		t.Fatalf("expected old to be archived, got %s", refreshed.Status)
	}
	var stillFresh model.DownloadLog
	if err := db.DB.First(&stillFresh, fresh.ID).Error; err != nil {
		t.Fatalf("reload fresh: %v", err)
	}
	if stillFresh.Status != statusFailed {
		t.Fatalf("expected fresh untouched, got %s", stillFresh.Status)
	}
}

func TestDownloadLogStoreUpdateTargetFileByOld(t *testing.T) {
	s := setupDownloadLogStore(t)
	logA := seedLog(t, statusCompleted, "/old/path.mkv", 1, "01")
	logB := seedLog(t, statusCompleted, "/other/path.mkv", 1, "02")

	if err := s.UpdateTargetFileByOld("/old/path.mkv", "/new/path.mkv"); err != nil {
		t.Fatalf("UpdateTargetFileByOld: %v", err)
	}

	var fresh model.DownloadLog
	if err := db.DB.First(&fresh, logA.ID).Error; err != nil {
		t.Fatalf("reload A: %v", err)
	}
	if fresh.TargetFile != "/new/path.mkv" {
		t.Fatalf("A not updated: %s", fresh.TargetFile)
	}

	var untouched model.DownloadLog
	if err := db.DB.First(&untouched, logB.ID).Error; err != nil {
		t.Fatalf("reload B: %v", err)
	}
	if untouched.TargetFile != "/other/path.mkv" {
		t.Fatalf("B should not change: %s", untouched.TargetFile)
	}
}
