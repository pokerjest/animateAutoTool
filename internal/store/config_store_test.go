package store

import (
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"gorm.io/gorm"
)

func TestConfigStoreSetManyAndListMap(t *testing.T) {
	db.InitDB(":memory:")
	t.Cleanup(func() {
		_ = db.CloseDB()
		db.DB = nil
	})

	store := NewConfigStore(db.DB)
	if err := store.SetMany(map[string]string{
		"alpha": "1",
		"beta":  "2",
	}); err != nil {
		t.Fatalf("SetMany returned error: %v", err)
	}

	values, err := store.ListMap()
	if err != nil {
		t.Fatalf("ListMap returned error: %v", err)
	}
	if values["alpha"] != "1" || values["beta"] != "2" {
		t.Fatalf("unexpected values: %#v", values)
	}
}

func TestConfigStoreGetDefaultHandlesMissingValue(t *testing.T) {
	store := NewConfigStore(nil)
	if got := store.GetDefault("missing", "fallback"); got != "fallback" {
		t.Fatalf("expected fallback, got %q", got)
	}
	if _, err := store.Get("missing"); err != gorm.ErrInvalidDB {
		t.Fatalf("expected ErrInvalidDB, got %v", err)
	}
}
