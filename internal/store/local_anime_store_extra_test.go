package store

import (
	"errors"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

func TestLocalAnimeStoreExtraNilSafety(t *testing.T) {
	s := NewLocalAnimeStore(nil)
	if _, err := s.CountAnimes(); err != gorm.ErrInvalidDB {
		t.Errorf("CountAnimes nil: got %v", err)
	}
	if _, err := s.CountEpisodes(); err != gorm.ErrInvalidDB {
		t.Errorf("CountEpisodes nil: got %v", err)
	}
	if _, err := s.CountAnimesWithJellyfin(); err != gorm.ErrInvalidDB {
		t.Errorf("CountAnimesWithJellyfin nil: got %v", err)
	}
	if _, err := s.CountEpisodesWithJellyfin(); err != gorm.ErrInvalidDB {
		t.Errorf("CountEpisodesWithJellyfin nil: got %v", err)
	}
	if _, err := s.GetAnime(1); err != gorm.ErrInvalidDB {
		t.Errorf("GetAnime nil: got %v", err)
	}
	if _, err := s.GetDirectory(1); err != gorm.ErrInvalidDB {
		t.Errorf("GetDirectory nil: got %v", err)
	}
	if _, err := s.ListAnimesByDirectory(1); err != gorm.ErrInvalidDB {
		t.Errorf("ListAnimesByDirectory nil: got %v", err)
	}
	if _, err := s.ListAnimesByDirectoryWithEpisodes(1); err != gorm.ErrInvalidDB {
		t.Errorf("ListAnimesByDirectoryWithEpisodes nil: got %v", err)
	}
	if _, err := s.ListEpisodesByAnimeIDOrdered(1); err != gorm.ErrInvalidDB {
		t.Errorf("ListEpisodesByAnimeIDOrdered nil: got %v", err)
	}
	if err := s.UpdateEpisodePathByID(1, "/x"); err != gorm.ErrInvalidDB {
		t.Errorf("UpdateEpisodePathByID nil: got %v", err)
	}
	if err := s.UpdateEpisodePathByOldPath("/old", "/new"); err != gorm.ErrInvalidDB {
		t.Errorf("UpdateEpisodePathByOldPath nil: got %v", err)
	}
}

func TestLocalAnimeStoreCountFamily(t *testing.T) {
	s := setupLocalAnimeStore(t)

	dir := &model.LocalAnimeDirectory{Path: "/p/anime"}
	if err := s.CreateDirectory(dir); err != nil {
		t.Fatalf("CreateDirectory: %v", err)
	}

	a1 := &model.LocalAnime{Title: "A1", Path: "/p/anime/A1", DirectoryID: dir.ID, JellyfinSeriesID: "jf-A1"}
	if err := s.CreateAnime(a1); err != nil {
		t.Fatalf("create A1: %v", err)
	}
	a2 := &model.LocalAnime{Title: "A2", Path: "/p/anime/A2", DirectoryID: dir.ID}
	if err := s.CreateAnime(a2); err != nil {
		t.Fatalf("create A2: %v", err)
	}

	if err := s.CreateEpisode(&model.LocalEpisode{LocalAnimeID: a1.ID, Path: "/p/anime/A1/01.mkv", JellyfinItemID: "jf-ep"}); err != nil {
		t.Fatalf("ep1: %v", err)
	}
	if err := s.CreateEpisode(&model.LocalEpisode{LocalAnimeID: a1.ID, Path: "/p/anime/A1/02.mkv"}); err != nil {
		t.Fatalf("ep2: %v", err)
	}

	if got, _ := s.CountAnimes(); got != 2 {
		t.Errorf("CountAnimes = %d, want 2", got)
	}
	if got, _ := s.CountEpisodes(); got != 2 {
		t.Errorf("CountEpisodes = %d, want 2", got)
	}
	if got, _ := s.CountAnimesWithJellyfin(); got != 1 {
		t.Errorf("CountAnimesWithJellyfin = %d, want 1", got)
	}
	if got, _ := s.CountEpisodesWithJellyfin(); got != 1 {
		t.Errorf("CountEpisodesWithJellyfin = %d, want 1", got)
	}
}

func TestLocalAnimeStoreGetAnimeAndDirectory(t *testing.T) {
	s := setupLocalAnimeStore(t)

	dir := &model.LocalAnimeDirectory{Path: "/p/anime"}
	if err := s.CreateDirectory(dir); err != nil {
		t.Fatalf("CreateDirectory: %v", err)
	}
	anime := &model.LocalAnime{Title: "Show", Path: "/p/anime/Show", DirectoryID: dir.ID}
	if err := s.CreateAnime(anime); err != nil {
		t.Fatalf("CreateAnime: %v", err)
	}

	got, err := s.GetAnime(anime.ID)
	if err != nil || got == nil || got.Title != "Show" {
		t.Fatalf("GetAnime: %v / %+v", err, got)
	}
	if _, err := s.GetAnime(99999); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected ErrRecordNotFound, got %v", err)
	}

	gotDir, err := s.GetDirectory(dir.ID)
	if err != nil || gotDir == nil || gotDir.Path != "/p/anime" {
		t.Fatalf("GetDirectory: %v / %+v", err, gotDir)
	}
}

func TestLocalAnimeStoreListAnimesByDirectory(t *testing.T) {
	s := setupLocalAnimeStore(t)

	d1 := &model.LocalAnimeDirectory{Path: "/p/anime"}
	if err := s.CreateDirectory(d1); err != nil {
		t.Fatalf("dir1: %v", err)
	}
	d2 := &model.LocalAnimeDirectory{Path: "/q/anime"}
	if err := s.CreateDirectory(d2); err != nil {
		t.Fatalf("dir2: %v", err)
	}
	if err := s.CreateAnime(&model.LocalAnime{Title: "A", Path: "/p/anime/A", DirectoryID: d1.ID}); err != nil {
		t.Fatalf("A: %v", err)
	}
	if err := s.CreateAnime(&model.LocalAnime{Title: "B", Path: "/p/anime/B", DirectoryID: d1.ID}); err != nil {
		t.Fatalf("B: %v", err)
	}
	if err := s.CreateAnime(&model.LocalAnime{Title: "C", Path: "/q/anime/C", DirectoryID: d2.ID}); err != nil {
		t.Fatalf("C: %v", err)
	}

	got, err := s.ListAnimesByDirectory(d1.ID)
	if err != nil {
		t.Fatalf("ListAnimesByDirectory: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 animes for d1, got %d", len(got))
	}
}

func TestLocalAnimeStoreListEpisodesByAnimeIDOrdered(t *testing.T) {
	s := setupLocalAnimeStore(t)
	anime := &model.LocalAnime{Title: "Show", Path: "/p/s"}
	if err := s.CreateAnime(anime); err != nil {
		t.Fatalf("CreateAnime: %v", err)
	}
	if err := s.CreateEpisode(&model.LocalEpisode{LocalAnimeID: anime.ID, Path: "/p/s/s2e1", SeasonNum: 2, EpisodeNum: 1}); err != nil {
		t.Fatalf("ep s2e1: %v", err)
	}
	if err := s.CreateEpisode(&model.LocalEpisode{LocalAnimeID: anime.ID, Path: "/p/s/s1e2", SeasonNum: 1, EpisodeNum: 2}); err != nil {
		t.Fatalf("ep s1e2: %v", err)
	}
	if err := s.CreateEpisode(&model.LocalEpisode{LocalAnimeID: anime.ID, Path: "/p/s/s1e1", SeasonNum: 1, EpisodeNum: 1}); err != nil {
		t.Fatalf("ep s1e1: %v", err)
	}

	eps, err := s.ListEpisodesByAnimeIDOrdered(anime.ID)
	if err != nil {
		t.Fatalf("ListEpisodesByAnimeIDOrdered: %v", err)
	}
	if len(eps) != 3 {
		t.Fatalf("expected 3, got %d", len(eps))
	}
	if eps[0].SeasonNum != 1 || eps[0].EpisodeNum != 1 {
		t.Fatalf("first should be S1E1, got S%dE%d", eps[0].SeasonNum, eps[0].EpisodeNum)
	}
	if eps[2].SeasonNum != 2 || eps[2].EpisodeNum != 1 {
		t.Fatalf("last should be S2E1, got S%dE%d", eps[2].SeasonNum, eps[2].EpisodeNum)
	}
}

func TestLocalAnimeStoreUpdateEpisodePath(t *testing.T) {
	s := setupLocalAnimeStore(t)
	anime := &model.LocalAnime{Title: "Show", Path: "/p/s"}
	if err := s.CreateAnime(anime); err != nil {
		t.Fatalf("CreateAnime: %v", err)
	}
	ep := &model.LocalEpisode{LocalAnimeID: anime.ID, Path: "/p/s/old.mkv"}
	if err := s.CreateEpisode(ep); err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}

	if err := s.UpdateEpisodePathByID(ep.ID, "/p/s/new1.mkv"); err != nil {
		t.Fatalf("UpdateEpisodePathByID: %v", err)
	}
	var fresh model.LocalEpisode
	if err := db.DB.First(&fresh, ep.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if fresh.Path != "/p/s/new1.mkv" {
		t.Fatalf("path not updated: %s", fresh.Path)
	}

	if err := s.UpdateEpisodePathByOldPath("/p/s/new1.mkv", "/p/s/new2.mkv"); err != nil {
		t.Fatalf("UpdateEpisodePathByOldPath: %v", err)
	}
	var fresh2 model.LocalEpisode
	if err := db.DB.First(&fresh2, ep.ID).Error; err != nil {
		t.Fatalf("reload2: %v", err)
	}
	if fresh2.Path != "/p/s/new2.mkv" {
		t.Fatalf("path not updated by old-path: %s", fresh2.Path)
	}
}
