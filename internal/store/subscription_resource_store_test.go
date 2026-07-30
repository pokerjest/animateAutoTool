package store

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

func TestSubscriptionResourceStoreUpsertPersistsZeroValueSelection(t *testing.T) {
	target, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := target.AutoMigrate(&model.SubscriptionResource{}); err != nil {
		t.Fatalf("migrate resource table: %v", err)
	}
	store := NewSubscriptionResourceStore(target)
	resource := model.SubscriptionResource{
		SubscriptionID: 1,
		CanonicalKey:   "episode:1:1",
		Fingerprint:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Title:          "Resource V1",
		State:          "seen",
		Selected:       true,
	}
	if err := store.Upsert(&resource); err != nil {
		t.Fatalf("create resource: %v", err)
	}
	resource.Selected = false
	resource.State = "superseded"
	if err := store.Upsert(&resource); err != nil {
		t.Fatalf("update resource: %v", err)
	}
	found, err := store.FindByFingerprint(resource.SubscriptionID, resource.Fingerprint)
	if err != nil {
		t.Fatalf("find resource: %v", err)
	}
	if found.Selected || found.State != "superseded" {
		t.Fatalf("zero-value update was not persisted: %+v", found)
	}
}
