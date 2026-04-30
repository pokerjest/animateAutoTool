package store

import (
	"errors"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

const testAnimeMetadataShowTitle = "Show"

func setupAnimeMetadataStore(t *testing.T) *AnimeMetadataStore {
	t.Helper()
	db.InitDB(":memory:")
	t.Cleanup(func() {
		_ = db.CloseDB()
		db.DB = nil
	})
	return NewAnimeMetadataStore(db.DB)
}

func TestAnimeMetadataStoreNilSafety(t *testing.T) {
	s := NewAnimeMetadataStore(nil)
	if _, err := s.GetByID(1); err != gorm.ErrInvalidDB {
		t.Errorf("GetByID nil: got %v", err)
	}
	if _, err := s.FindByBangumiID(1); err != gorm.ErrInvalidDB {
		t.Errorf("FindByBangumiID nil: got %v", err)
	}
	if _, err := s.FindByAnyTitle("x"); err != gorm.ErrInvalidDB {
		t.Errorf("FindByAnyTitle nil: got %v", err)
	}
	if _, err := s.FindByTMDBID(1); err != gorm.ErrInvalidDB {
		t.Errorf("FindByTMDBID nil: got %v", err)
	}
	if _, err := s.FindByAniListID(1); err != gorm.ErrInvalidDB {
		t.Errorf("FindByAniListID nil: got %v", err)
	}
	if err := s.Create(&model.AnimeMetadata{}); err != gorm.ErrInvalidDB {
		t.Errorf("Create nil: got %v", err)
	}
	if err := s.Save(&model.AnimeMetadata{}); err != gorm.ErrInvalidDB {
		t.Errorf("Save nil: got %v", err)
	}
	if _, err := s.ListAll(); err != gorm.ErrInvalidDB {
		t.Errorf("ListAll nil: got %v", err)
	}
	if _, err := s.ListWithImageRawMissing(); err != gorm.ErrInvalidDB {
		t.Errorf("ListWithImageRawMissing nil: got %v", err)
	}
	if err := s.PropagateToSubscriptions(1, map[string]interface{}{"x": 1}); err != gorm.ErrInvalidDB {
		t.Errorf("PropagateToSubscriptions nil: got %v", err)
	}
	if err := s.PropagateToLocalAnimes(1, map[string]interface{}{"x": 1}); err != gorm.ErrInvalidDB {
		t.Errorf("PropagateToLocalAnimes nil: got %v", err)
	}
}

func TestAnimeMetadataStorePropagateNoOpOnZeroOrEmpty(t *testing.T) {
	s := setupAnimeMetadataStore(t)
	if err := s.PropagateToSubscriptions(0, map[string]interface{}{"x": 1}); err != nil {
		t.Errorf("PropagateToSubscriptions zero id: %v", err)
	}
	if err := s.PropagateToLocalAnimes(1, nil); err != nil {
		t.Errorf("PropagateToLocalAnimes empty updates: %v", err)
	}
}

func TestAnimeMetadataStoreCreateAndGet(t *testing.T) {
	s := setupAnimeMetadataStore(t)

	m := &model.AnimeMetadata{Title: testAnimeMetadataShowTitle, BangumiID: 100, TMDBID: 200, AniListID: 300}
	if err := s.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if m.ID == 0 {
		t.Fatal("expected non-zero id")
	}

	got, err := s.GetByID(m.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Title != testAnimeMetadataShowTitle {
		t.Fatalf("unexpected title: %q", got.Title)
	}

	got.Title = "Show v2"
	if err := s.Save(got); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if reload, _ := s.GetByID(m.ID); reload.Title != "Show v2" {
		t.Fatalf("save did not persist: %q", reload.Title)
	}
}

func TestAnimeMetadataStoreFindByExternalIDs(t *testing.T) {
	s := setupAnimeMetadataStore(t)

	m := &model.AnimeMetadata{Title: testAnimeMetadataShowTitle, BangumiID: 11, TMDBID: 22, AniListID: 33}
	if err := s.Create(m); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got, err := s.FindByBangumiID(11); err != nil || got == nil || got.ID != m.ID {
		t.Errorf("FindByBangumiID: %v / %v", got, err)
	}
	if got, err := s.FindByTMDBID(22); err != nil || got == nil || got.ID != m.ID {
		t.Errorf("FindByTMDBID: %v / %v", got, err)
	}
	if got, err := s.FindByAniListID(33); err != nil || got == nil || got.ID != m.ID {
		t.Errorf("FindByAniListID: %v / %v", got, err)
	}

	if _, err := s.FindByBangumiID(999); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("expected ErrRecordNotFound, got %v", err)
	}
}

func TestAnimeMetadataStoreFindByAnyTitle(t *testing.T) {
	s := setupAnimeMetadataStore(t)

	if err := s.Create(&model.AnimeMetadata{Title: testAnimeMetadataShowTitle, TitleCN: "节目", TitleJP: "ショー", TitleEN: "Show EN", BangumiID: 1}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, q := range []string{testAnimeMetadataShowTitle, "节目", "ショー", "Show EN"} {
		got, err := s.FindByAnyTitle(q)
		if err != nil || got == nil {
			t.Errorf("FindByAnyTitle(%q): %v / %v", q, got, err)
		}
	}

	if _, err := s.FindByAnyTitle("missing"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("missing title: expected ErrRecordNotFound, got %v", err)
	}
}

func TestAnimeMetadataStoreListAll(t *testing.T) {
	s := setupAnimeMetadataStore(t)

	if err := s.Create(&model.AnimeMetadata{Title: "A", BangumiID: 1}); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if err := s.Create(&model.AnimeMetadata{Title: "B", BangumiID: 2}); err != nil {
		t.Fatalf("create B: %v", err)
	}

	all, err := s.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}
}

func TestAnimeMetadataStoreListWithImageRawMissing(t *testing.T) {
	s := setupAnimeMetadataStore(t)

	cached := &model.AnimeMetadata{Title: "Cached", BangumiID: 1, BangumiImage: "http://x", BangumiImageRaw: []byte{1, 2, 3}}
	if err := s.Create(cached); err != nil {
		t.Fatalf("create cached: %v", err)
	}
	pending := &model.AnimeMetadata{Title: "Pending", BangumiID: 2, BangumiImage: "http://y"}
	if err := s.Create(pending); err != nil {
		t.Fatalf("create pending: %v", err)
	}

	list, err := s.ListWithImageRawMissing()
	if err != nil {
		t.Fatalf("ListWithImageRawMissing: %v", err)
	}
	if len(list) != 1 || list[0].ID != pending.ID {
		t.Fatalf("expected only pending, got %+v", list)
	}
}

func TestAnimeMetadataStorePropagateToSubscriptionsAndLocalAnimes(t *testing.T) {
	s := setupAnimeMetadataStore(t)

	m := &model.AnimeMetadata{Title: "Old", BangumiID: 42}
	if err := s.Create(m); err != nil {
		t.Fatalf("Create meta: %v", err)
	}

	sub := &model.Subscription{Title: "Sub", RSSUrl: "http://r/1.xml", Image: "old.png", Summary: "old", MetadataID: &m.ID}
	if err := db.DB.Create(sub).Error; err != nil {
		t.Fatalf("create sub: %v", err)
	}
	la := &model.LocalAnime{Title: "Local", Path: "/p", Image: "old.png", Summary: "old", AirDate: "old", MetadataID: &m.ID}
	if err := db.DB.Create(la).Error; err != nil {
		t.Fatalf("create local: %v", err)
	}

	if err := s.PropagateToSubscriptions(m.ID, map[string]interface{}{
		"image":   "new.png",
		"title":   "New",
		"summary": "new sum",
	}); err != nil {
		t.Fatalf("PropagateToSubscriptions: %v", err)
	}
	if err := s.PropagateToLocalAnimes(m.ID, map[string]interface{}{
		"image":    "new.png",
		"title":    "New",
		"summary":  "new sum",
		"air_date": "2026",
	}); err != nil {
		t.Fatalf("PropagateToLocalAnimes: %v", err)
	}

	var freshSub model.Subscription
	if err := db.DB.First(&freshSub, sub.ID).Error; err != nil {
		t.Fatalf("reload sub: %v", err)
	}
	if freshSub.Title != "New" || freshSub.Image != "new.png" || freshSub.Summary != "new sum" {
		t.Fatalf("subscription not propagated: %+v", freshSub)
	}

	var freshLA model.LocalAnime
	if err := db.DB.First(&freshLA, la.ID).Error; err != nil {
		t.Fatalf("reload local: %v", err)
	}
	if freshLA.Title != "New" || freshLA.AirDate != "2026" {
		t.Fatalf("local anime not propagated: %+v", freshLA)
	}
}
