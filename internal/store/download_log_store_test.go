package store

import (
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

const (
	statusDownloading = "downloading"
	statusCompleted   = "completed"
	statusFailed      = "failed"
	statusArchived    = "archived"
)

func setupDownloadLogStore(t *testing.T) *DownloadLogStore {
	t.Helper()
	db.InitDB(":memory:")
	t.Cleanup(func() {
		_ = db.CloseDB()
		db.DB = nil
	})
	return NewDownloadLogStore(db.DB)
}

func seedLog(t *testing.T, status, target string, subID uint, episode string) *model.DownloadLog {
	t.Helper()
	log := &model.DownloadLog{
		SubscriptionID: subID,
		Title:          "ep " + episode,
		Status:         status,
		Episode:        episode,
		TargetFile:     target,
	}
	if err := db.DB.Create(log).Error; err != nil {
		t.Fatalf("seed log: %v", err)
	}
	return log
}

func TestDownloadLogStoreNilSafety(t *testing.T) {
	s := NewDownloadLogStore(nil)
	if _, err := s.ListActiveOrIncompleteCompleted(statusDownloading, statusFailed, statusCompleted); err != gorm.ErrInvalidDB {
		t.Errorf("ListActiveOrIncompleteCompleted nil: got %v", err)
	}
	if _, err := s.ListByStatuses([]string{statusFailed}); err != gorm.ErrInvalidDB {
		t.Errorf("ListByStatuses nil: got %v", err)
	}
	if _, err := s.ListByStatusesAsc([]string{statusFailed}); err != gorm.ErrInvalidDB {
		t.Errorf("ListByStatusesAsc nil: got %v", err)
	}
	if err := s.UpdateByID(1, map[string]interface{}{"status": statusCompleted}); err != gorm.ErrInvalidDB {
		t.Errorf("UpdateByID nil: got %v", err)
	}
	if err := s.MarkArchived(1, statusArchived); err != gorm.ErrInvalidDB {
		t.Errorf("MarkArchived nil: got %v", err)
	}
	if got := s.HasCompletedSibling(1, "01", statusCompleted); got != false {
		t.Errorf("HasCompletedSibling nil: expected false, got %v", got)
	}
	if _, err := s.CountBySubscription(1); err != gorm.ErrInvalidDB {
		t.Errorf("CountBySubscription nil: got %v", err)
	}
}

func TestDownloadLogStoreEmptyInputShortCircuits(t *testing.T) {
	s := setupDownloadLogStore(t)

	if logs, err := s.ListByStatuses(nil); err != nil || logs != nil {
		t.Errorf("ListByStatuses(nil) -> %v / %v", logs, err)
	}
	if logs, err := s.ListByStatusesAsc(nil); err != nil || logs != nil {
		t.Errorf("ListByStatusesAsc(nil) -> %v / %v", logs, err)
	}
	if err := s.UpdateByID(1, nil); err != nil {
		t.Errorf("UpdateByID empty: %v", err)
	}
}

func TestDownloadLogStoreListActiveOrIncompleteCompleted(t *testing.T) {
	s := setupDownloadLogStore(t)

	seedLog(t, statusDownloading, "", 1, "01")
	seedLog(t, statusFailed, "", 1, "02")
	seedLog(t, statusCompleted, "/full/path/ep03.mkv", 1, "03") // excluded: completed with target
	seedLog(t, statusCompleted, "", 1, "04")                    // included: completed but target empty

	logs, err := s.ListActiveOrIncompleteCompleted(statusDownloading, statusFailed, statusCompleted)
	if err != nil {
		t.Fatalf("ListActiveOrIncompleteCompleted: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(logs))
	}
	// Newest first ordering check.
	if logs[0].Episode != "04" {
		t.Errorf("expected newest first ordering (ep 04), got %s", logs[0].Episode)
	}
}

func TestDownloadLogStoreListByStatusesAndAsc(t *testing.T) {
	s := setupDownloadLogStore(t)

	seedLog(t, statusDownloading, "", 1, "01")
	time.Sleep(10 * time.Millisecond)
	seedLog(t, statusFailed, "", 1, "02")
	time.Sleep(10 * time.Millisecond)
	seedLog(t, statusCompleted, "/path/ep03.mkv", 1, "03")

	desc, err := s.ListByStatuses([]string{statusDownloading, statusFailed})
	if err != nil {
		t.Fatalf("ListByStatuses: %v", err)
	}
	if len(desc) != 2 || desc[0].Episode != "02" {
		t.Fatalf("ListByStatuses ordering wrong: %+v", desc)
	}

	asc, err := s.ListByStatusesAsc([]string{statusDownloading, statusFailed})
	if err != nil {
		t.Fatalf("ListByStatusesAsc: %v", err)
	}
	if len(asc) != 2 || asc[0].Episode != "01" {
		t.Fatalf("ListByStatusesAsc ordering wrong: %+v", asc)
	}
}

func TestDownloadLogStoreUpdateByID(t *testing.T) {
	s := setupDownloadLogStore(t)
	log := seedLog(t, statusDownloading, "", 1, "05")

	if err := s.UpdateByID(log.ID, map[string]interface{}{
		"status":      statusCompleted,
		"target_file": "/x/y/ep05.mkv",
	}); err != nil {
		t.Fatalf("UpdateByID: %v", err)
	}

	var fresh model.DownloadLog
	if err := db.DB.First(&fresh, log.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if fresh.Status != statusCompleted || fresh.TargetFile != "/x/y/ep05.mkv" {
		t.Fatalf("update did not apply: %+v", fresh)
	}
}

func TestDownloadLogStoreMarkArchived(t *testing.T) {
	s := setupDownloadLogStore(t)
	log := seedLog(t, statusFailed, "", 1, "06")

	if err := s.MarkArchived(log.ID, statusArchived); err != nil {
		t.Fatalf("MarkArchived: %v", err)
	}

	var fresh model.DownloadLog
	if err := db.DB.First(&fresh, log.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if fresh.Status != statusArchived {
		t.Fatalf("expected archived, got %s", fresh.Status)
	}
}

func TestDownloadLogStoreHasCompletedSibling(t *testing.T) {
	s := setupDownloadLogStore(t)

	// Sub 1 has a completed ep 01.
	seedLog(t, statusCompleted, "/x/ep01.mkv", 1, "01")
	// Sub 1 also has a failed ep 02.
	seedLog(t, statusFailed, "", 1, "02")
	// Sub 2 has nothing relevant.
	seedLog(t, statusFailed, "", 2, "01")

	if !s.HasCompletedSibling(1, "01", statusCompleted) {
		t.Error("expected sub 1 ep 01 to have a completed sibling")
	}
	if s.HasCompletedSibling(1, "02", statusCompleted) {
		t.Error("did not expect sub 1 ep 02 to have a completed sibling")
	}
	// Episode-less query: any completed entry under the subscription is enough.
	if !s.HasCompletedSibling(1, "", statusCompleted) {
		t.Error("expected sub 1 to have at least one completed entry")
	}
	if s.HasCompletedSibling(2, "", statusCompleted) {
		t.Error("did not expect sub 2 to have any completed entry")
	}
}

func TestDownloadLogStoreCountBySubscription(t *testing.T) {
	s := setupDownloadLogStore(t)

	seedLog(t, statusCompleted, "/x/01.mkv", 7, "01")
	seedLog(t, statusFailed, "", 7, "02")
	seedLog(t, statusDownloading, "", 8, "01")

	count, err := s.CountBySubscription(7)
	if err != nil {
		t.Fatalf("CountBySubscription: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 logs for sub 7, got %d", count)
	}

	count, err = s.CountBySubscription(999)
	if err != nil {
		t.Fatalf("CountBySubscription missing: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 logs for unknown sub, got %d", count)
	}
}
