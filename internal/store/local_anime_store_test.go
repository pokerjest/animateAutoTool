package store

import (
	"errors"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

func setupLocalAnimeStore(t *testing.T) *LocalAnimeStore {
	t.Helper()
	db.InitDB(":memory:")
	t.Cleanup(func() {
		_ = db.CloseDB()
		db.DB = nil
	})
	return NewLocalAnimeStore(db.DB)
}

func TestLocalAnimeStoreNilSafety(t *testing.T) {
	s := NewLocalAnimeStore(nil)
	if _, err := s.ListDirectories(); err != gorm.ErrInvalidDB {
		t.Errorf("ListDirectories nil: got %v", err)
	}
	if _, err := s.FindDirectoryByPath("/x", false); err != gorm.ErrInvalidDB {
		t.Errorf("FindDirectoryByPath nil: got %v", err)
	}
	if err := s.HardDeleteDirectory(&model.LocalAnimeDirectory{}); err != gorm.ErrInvalidDB {
		t.Errorf("HardDeleteDirectory nil: got %v", err)
	}
	if err := s.CreateDirectory(&model.LocalAnimeDirectory{}); err != gorm.ErrInvalidDB {
		t.Errorf("CreateDirectory nil: got %v", err)
	}
	if err := s.RemoveDirectoryWithAnimes(1); err != gorm.ErrInvalidDB {
		t.Errorf("RemoveDirectoryWithAnimes nil: got %v", err)
	}
	if _, err := s.ListAll(); err != gorm.ErrInvalidDB {
		t.Errorf("ListAll nil: got %v", err)
	}
	if _, err := s.GetWithMetadata(1); err != gorm.ErrInvalidDB {
		t.Errorf("GetWithMetadata nil: got %v", err)
	}
	if _, err := s.FindAnimeByPath("/x"); err != gorm.ErrInvalidDB {
		t.Errorf("FindAnimeByPath nil: got %v", err)
	}
	if err := s.CreateAnime(&model.LocalAnime{}); err != gorm.ErrInvalidDB {
		t.Errorf("CreateAnime nil: got %v", err)
	}
	if err := s.SaveAnime(&model.LocalAnime{}); err != gorm.ErrInvalidDB {
		t.Errorf("SaveAnime nil: got %v", err)
	}
	if _, err := s.FindEpisodeByPath("/x"); err != gorm.ErrInvalidDB {
		t.Errorf("FindEpisodeByPath nil: got %v", err)
	}
	if err := s.CreateEpisode(&model.LocalEpisode{}); err != gorm.ErrInvalidDB {
		t.Errorf("CreateEpisode nil: got %v", err)
	}
	if err := s.SaveEpisode(&model.LocalEpisode{}); err != gorm.ErrInvalidDB {
		t.Errorf("SaveEpisode nil: got %v", err)
	}
	if _, err := s.FindEpisodeByPathIncludingDeleted("/x"); err != gorm.ErrInvalidDB {
		t.Errorf("FindEpisodeByPathIncludingDeleted nil: got %v", err)
	}
	if err := s.SaveEpisodeIncludingDeleted(&model.LocalEpisode{}); err != gorm.ErrInvalidDB {
		t.Errorf("SaveEpisodeIncludingDeleted nil: got %v", err)
	}
	if err := s.DeleteEpisodesNotInPaths(1, []string{"a"}); err != gorm.ErrInvalidDB {
		t.Errorf("DeleteEpisodesNotInPaths nil: got %v", err)
	}
	if err := s.CleanupOrphans(); err != gorm.ErrInvalidDB {
		t.Errorf("CleanupOrphans nil: got %v", err)
	}
	if err := s.CleanupOrphansByDirectory(1); err != gorm.ErrInvalidDB {
		t.Errorf("CleanupOrphansByDirectory nil: got %v", err)
	}
	if _, err := s.EpisodePathsByMetadata(1, 1); err != gorm.ErrInvalidDB {
		t.Errorf("EpisodePathsByMetadata nil: got %v", err)
	}
	if _, err := s.EpisodePathsByEpisodeNum(1); err != gorm.ErrInvalidDB {
		t.Errorf("EpisodePathsByEpisodeNum nil: got %v", err)
	}
	if _, err := s.ListEpisodesByAnimeID(1); err != gorm.ErrInvalidDB {
		t.Errorf("ListEpisodesByAnimeID nil: got %v", err)
	}
}

func TestLocalAnimeStoreDirectoryLifecycle(t *testing.T) {
	s := setupLocalAnimeStore(t)

	dir := &model.LocalAnimeDirectory{Path: "/media/anime"}
	if err := s.CreateDirectory(dir); err != nil {
		t.Fatalf("CreateDirectory: %v", err)
	}
	if dir.ID == 0 {
		t.Fatal("expected non-zero id after create")
	}

	got, err := s.FindDirectoryByPath("/media/anime", false)
	if err != nil || got == nil {
		t.Fatalf("FindDirectoryByPath: %v / %v", got, err)
	}

	dirs, err := s.ListDirectories()
	if err != nil || len(dirs) != 1 {
		t.Fatalf("ListDirectories: %v / count=%d", err, len(dirs))
	}

	// Soft-delete then make sure it's hidden by default and visible with includeDeleted.
	if err := db.DB.Delete(dir).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if _, err := s.FindDirectoryByPath("/media/anime", false); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected ErrRecordNotFound after soft delete, got %v", err)
	}
	revived, err := s.FindDirectoryByPath("/media/anime", true)
	if err != nil || revived == nil {
		t.Fatalf("FindDirectoryByPath includeDeleted: %v / %v", revived, err)
	}
	if !revived.DeletedAt.Valid {
		t.Fatal("expected revived row to retain DeletedAt")
	}

	if err := s.HardDeleteDirectory(revived); err != nil {
		t.Fatalf("HardDeleteDirectory: %v", err)
	}
	if _, err := s.FindDirectoryByPath("/media/anime", true); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected gone after hard delete, got %v", err)
	}
}

func TestLocalAnimeStoreRemoveDirectoryWithAnimes(t *testing.T) {
	s := setupLocalAnimeStore(t)

	dir := &model.LocalAnimeDirectory{Path: "/media/anime"}
	if err := s.CreateDirectory(dir); err != nil {
		t.Fatalf("CreateDirectory: %v", err)
	}
	anime := &model.LocalAnime{Title: "Show", Path: "/media/anime/Show", DirectoryID: dir.ID}
	if err := s.CreateAnime(anime); err != nil {
		t.Fatalf("CreateAnime: %v", err)
	}

	if err := s.RemoveDirectoryWithAnimes(dir.ID); err != nil {
		t.Fatalf("RemoveDirectoryWithAnimes: %v", err)
	}

	if _, err := s.FindDirectoryByPath("/media/anime", true); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("directory should be gone, got %v", err)
	}
	var count int64
	if err := db.DB.Unscoped().Model(&model.LocalAnime{}).Where("directory_id = ?", dir.ID).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected anime rows wiped, got %d", count)
	}
}

func TestLocalAnimeStoreAnimeAndEpisodeCRUD(t *testing.T) {
	s := setupLocalAnimeStore(t)

	dir := &model.LocalAnimeDirectory{Path: "/media/anime"}
	if err := s.CreateDirectory(dir); err != nil {
		t.Fatalf("CreateDirectory: %v", err)
	}
	anime := &model.LocalAnime{Title: "Show A", Path: "/media/anime/A", DirectoryID: dir.ID, FileCount: 1, TotalSize: 100}
	if err := s.CreateAnime(anime); err != nil {
		t.Fatalf("CreateAnime: %v", err)
	}

	got, err := s.FindAnimeByPath("/media/anime/A")
	if err != nil || got == nil {
		t.Fatalf("FindAnimeByPath: %v / %v", got, err)
	}

	got.FileCount = 5
	got.TotalSize = 500
	if err := s.SaveAnime(got); err != nil {
		t.Fatalf("SaveAnime: %v", err)
	}
	reload, _ := s.FindAnimeByPath("/media/anime/A")
	if reload.FileCount != 5 || reload.TotalSize != 500 {
		t.Fatalf("save did not persist: %+v", reload)
	}

	ep := &model.LocalEpisode{LocalAnimeID: anime.ID, Path: "/media/anime/A/ep01.mkv", EpisodeNum: 1}
	if err := s.CreateEpisode(ep); err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}
	gotEp, err := s.FindEpisodeByPath("/media/anime/A/ep01.mkv")
	if err != nil || gotEp == nil {
		t.Fatalf("FindEpisodeByPath: %v / %v", gotEp, err)
	}
	gotEp.EpisodeNum = 2
	if err := s.SaveEpisode(gotEp); err != nil {
		t.Fatalf("SaveEpisode: %v", err)
	}

	all, err := s.ListAll()
	if err != nil || len(all) != 1 {
		t.Fatalf("ListAll: %v / count=%d", err, len(all))
	}

	withMeta, err := s.GetWithMetadata(anime.ID)
	if err != nil || withMeta == nil {
		t.Fatalf("GetWithMetadata: %v / %v", withMeta, err)
	}
}

func TestLocalAnimeStoreDeleteEpisodesNotInPaths(t *testing.T) {
	s := setupLocalAnimeStore(t)

	anime := &model.LocalAnime{Title: "Show", Path: "/p/s"}
	if err := s.CreateAnime(anime); err != nil {
		t.Fatalf("CreateAnime: %v", err)
	}

	keep := "/p/s/keep.mkv"
	gone := "/p/s/gone.mkv"
	if err := s.CreateEpisode(&model.LocalEpisode{LocalAnimeID: anime.ID, Path: keep}); err != nil {
		t.Fatalf("ep keep: %v", err)
	}
	if err := s.CreateEpisode(&model.LocalEpisode{LocalAnimeID: anime.ID, Path: gone}); err != nil {
		t.Fatalf("ep gone: %v", err)
	}

	if err := s.DeleteEpisodesNotInPaths(anime.ID, []string{keep}); err != nil {
		t.Fatalf("DeleteEpisodesNotInPaths: %v", err)
	}
	eps, err := s.ListEpisodesByAnimeID(anime.ID)
	if err != nil {
		t.Fatalf("ListEpisodesByAnimeID: %v", err)
	}
	if len(eps) != 1 || eps[0].Path != keep {
		t.Fatalf("expected only keep, got %+v", eps)
	}

	// Empty keep list deletes everything left.
	if err := s.DeleteEpisodesNotInPaths(anime.ID, nil); err != nil {
		t.Fatalf("DeleteEpisodesNotInPaths empty: %v", err)
	}
	if eps, _ := s.ListEpisodesByAnimeID(anime.ID); len(eps) != 0 {
		t.Fatalf("expected all gone, got %+v", eps)
	}
}

func TestLocalAnimeStoreCanRestoreSoftDeletedEpisode(t *testing.T) {
	s := setupLocalAnimeStore(t)
	anime := &model.LocalAnime{Title: "Show", Path: "/p/s"}
	if err := s.CreateAnime(anime); err != nil {
		t.Fatalf("CreateAnime: %v", err)
	}
	episodePath := "/p/s/01.mkv"
	if err := s.CreateEpisode(&model.LocalEpisode{LocalAnimeID: anime.ID, Path: episodePath, EpisodeNum: 1}); err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}
	if err := s.DeleteEpisodesNotInPaths(anime.ID, nil); err != nil {
		t.Fatalf("DeleteEpisodesNotInPaths: %v", err)
	}

	episode, err := s.FindEpisodeByPathIncludingDeleted(episodePath)
	if err != nil {
		t.Fatalf("FindEpisodeByPathIncludingDeleted: %v", err)
	}
	episode.DeletedAt = gorm.DeletedAt{}
	if err := s.SaveEpisodeIncludingDeleted(episode); err != nil {
		t.Fatalf("SaveEpisodeIncludingDeleted: %v", err)
	}
	if _, err := s.FindEpisodeByPath(episodePath); err != nil {
		t.Fatalf("expected restored episode to be visible: %v", err)
	}
}

func TestLocalAnimeStoreCleanupOrphans(t *testing.T) {
	s := setupLocalAnimeStore(t)

	dir := &model.LocalAnimeDirectory{Path: "/p/anime"}
	if err := s.CreateDirectory(dir); err != nil {
		t.Fatalf("CreateDirectory: %v", err)
	}

	withEpisodes := &model.LocalAnime{Title: "WithEp", Path: "/p/anime/we", DirectoryID: dir.ID}
	if err := s.CreateAnime(withEpisodes); err != nil {
		t.Fatalf("CreateAnime withEp: %v", err)
	}
	if err := s.CreateEpisode(&model.LocalEpisode{LocalAnimeID: withEpisodes.ID, Path: "/p/anime/we/01.mkv"}); err != nil {
		t.Fatalf("episode: %v", err)
	}

	noEpisodes := &model.LocalAnime{Title: "Empty", Path: "/p/anime/empty", DirectoryID: dir.ID}
	if err := s.CreateAnime(noEpisodes); err != nil {
		t.Fatalf("CreateAnime empty: %v", err)
	}

	orphanDir := &model.LocalAnime{Title: "Orphan", Path: "/elsewhere/o", DirectoryID: 9999}
	if err := s.CreateAnime(orphanDir); err != nil {
		t.Fatalf("CreateAnime orphan: %v", err)
	}

	if err := s.CleanupOrphans(); err != nil {
		t.Fatalf("CleanupOrphans: %v", err)
	}

	all, err := s.ListAll()
	if err != nil {
		t.Fatalf("ListAll after cleanup: %v", err)
	}
	if len(all) != 1 || all[0].Title != "WithEp" {
		t.Fatalf("expected only WithEp to survive, got %+v", all)
	}
}

func TestLocalAnimeStoreEpisodePathJoins(t *testing.T) {
	s := setupLocalAnimeStore(t)

	meta := &model.AnimeMetadata{Title: "M"}
	if err := db.DB.Create(meta).Error; err != nil {
		t.Fatalf("create meta: %v", err)
	}
	anime := &model.LocalAnime{Title: "Show", Path: "/p/s", MetadataID: &meta.ID}
	if err := s.CreateAnime(anime); err != nil {
		t.Fatalf("CreateAnime: %v", err)
	}
	if err := s.CreateEpisode(&model.LocalEpisode{LocalAnimeID: anime.ID, Path: "/p/s/01.mkv", EpisodeNum: 1}); err != nil {
		t.Fatalf("episode: %v", err)
	}

	rows, err := s.EpisodePathsByMetadata(meta.ID, 1)
	if err != nil {
		t.Fatalf("EpisodePathsByMetadata: %v", err)
	}
	if len(rows) != 1 || rows[0].Path != "/p/s/01.mkv" {
		t.Fatalf("unexpected rows: %+v", rows)
	}

	titleRows, err := s.EpisodePathsByEpisodeNum(1)
	if err != nil {
		t.Fatalf("EpisodePathsByEpisodeNum: %v", err)
	}
	if len(titleRows) != 1 || titleRows[0].AnimeTitle != "Show" {
		t.Fatalf("unexpected title rows: %+v", titleRows)
	}
}
