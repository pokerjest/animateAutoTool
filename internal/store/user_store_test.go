package store

import (
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

func TestUserStoreCRUDAndNilSafety(t *testing.T) {
	if _, err := NewUserStore(nil).Count(); err != gorm.ErrInvalidDB {
		t.Fatalf("expected ErrInvalidDB, got %v", err)
	}

	db.InitDB(":memory:")
	t.Cleanup(func() {
		_ = db.CloseDB()
		db.DB = nil
	})

	st := NewUserStore(db.DB)
	user := &model.User{Username: "alice", PasswordHash: "hash"}
	if err := st.Create(user); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	count, err := st.Count()
	if err != nil {
		t.Fatalf("Count returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}

	byName, err := st.GetByUsername("alice")
	if err != nil {
		t.Fatalf("GetByUsername returned error: %v", err)
	}
	if byName.ID != user.ID {
		t.Fatalf("expected user ID %d, got %d", user.ID, byName.ID)
	}

	byID, err := st.GetByID(user.ID)
	if err != nil {
		t.Fatalf("GetByID returned error: %v", err)
	}
	byID.PasswordHash = "new-hash"
	if err := st.Save(byID); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	refreshed, err := st.GetByUsername("alice")
	if err != nil {
		t.Fatalf("GetByUsername after save returned error: %v", err)
	}
	if refreshed.PasswordHash != "new-hash" {
		t.Fatalf("expected updated hash, got %q", refreshed.PasswordHash)
	}

	if err := st.DeleteByUsername("alice"); err != nil {
		t.Fatalf("DeleteByUsername returned error: %v", err)
	}
	if _, err := st.GetByUsername("alice"); err == nil {
		t.Fatal("expected deleted user lookup to fail")
	}
}
