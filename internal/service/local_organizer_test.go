package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalOrganizerPreviewAndExecuteMovesEpisodeSidecarsAndAssets(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	sourceDir := filepath.Join(root, "[Group] Example Season 1")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	video := filepath.Join(sourceDir, "[Group] Example - 01 [1080p].mkv")
	subtitle := filepath.Join(sourceDir, "[Group] Example - 01 [1080p].zh-CN.ass")
	nfo := filepath.Join(sourceDir, "[Group] Example - 01 [1080p].nfo")
	poster := filepath.Join(sourceDir, "poster.jpg")
	for _, path := range []string{video, subtitle, nfo, poster} {
		require.NoError(t, os.WriteFile(path, []byte(filepath.Base(path)), 0o600))
	}

	directory := model.LocalAnimeDirectory{Path: root}
	require.NoError(t, db.DB.Create(&directory).Error)
	metadata := model.AnimeMetadata{TitleCN: "示例番剧", AirDate: "2026-01-01"}
	require.NoError(t, db.DB.Create(&metadata).Error)
	anime := model.LocalAnime{DirectoryID: directory.ID, Title: "[Group] Example Season 1", Path: sourceDir, Season: 1, MetadataID: &metadata.ID}
	require.NoError(t, db.DB.Create(&anime).Error)
	episode := model.LocalEpisode{LocalAnimeID: anime.ID, EpisodeNum: 1, SeasonNum: 1, Path: video, Container: "mkv"}
	require.NoError(t, db.DB.Create(&episode).Error)

	organizer := NewLocalOrganizer(db.DB, nil)
	preview, err := organizer.Preview("user-1", LocalOrganizePreviewRequest{
		Selection: LocalOrganizeSelection{Mode: OrganizeSelectionIDs, AnimeIDs: []uint{anime.ID}},
	})
	require.NoError(t, err)
	require.Len(t, preview.Items, 1)
	assert.True(t, preview.Items[0].MetadataMatched)
	assert.Equal(t, 4, preview.ChangeCount)
	assert.Equal(t, filepath.Join(root, "示例番剧"), preview.Items[0].TargetPath)

	result, err := organizer.Execute(t.Context(), preview, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 4, result.Moved)
	wantBase := filepath.Join(root, "示例番剧", "Season 01", "示例番剧 - S01E01")
	for _, target := range []string{wantBase + ".mkv", wantBase + ".zh-CN.ass", wantBase + ".nfo", filepath.Join(root, "示例番剧", "poster.jpg")} {
		_, statErr := os.Stat(target)
		assert.NoError(t, statErr, target)
	}
	var updated model.LocalEpisode
	require.NoError(t, db.DB.First(&updated, episode.ID).Error)
	assert.Equal(t, wantBase+".mkv", updated.Path)
	_, sourceErr := os.Stat(sourceDir)
	assert.True(t, os.IsNotExist(sourceErr))
}

func TestLocalOrganizerQuerySelectionExcludesItemsAndWarnsWithoutMetadata(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	directory := model.LocalAnimeDirectory{Path: root}
	require.NoError(t, db.DB.Create(&directory).Error)
	makeAnime := func(title string) model.LocalAnime {
		path := filepath.Join(root, title)
		require.NoError(t, os.MkdirAll(path, 0o755))
		video := filepath.Join(path, title+" - 01.mkv")
		require.NoError(t, os.WriteFile(video, []byte("video"), 0o600))
		anime := model.LocalAnime{DirectoryID: directory.ID, Title: title, Path: path, Season: 1}
		require.NoError(t, db.DB.Create(&anime).Error)
		require.NoError(t, db.DB.Create(&model.LocalEpisode{LocalAnimeID: anime.ID, EpisodeNum: 1, SeasonNum: 1, Path: video}).Error)
		return anime
	}
	first := makeAnime("Query Show A")
	second := makeAnime("Query Show B")
	_ = first

	preview, err := NewLocalOrganizer(db.DB, nil).Preview("user", LocalOrganizePreviewRequest{Selection: LocalOrganizeSelection{
		Mode: OrganizeSelectionQuery, Query: "Query Show", ExcludeIDs: []uint{second.ID},
	}})
	require.NoError(t, err)
	require.Len(t, preview.Items, 1)
	assert.False(t, preview.Items[0].MetadataMatched)
	assert.NotEmpty(t, preview.Items[0].Warnings)
}

func TestLocalOrganizerProtectsConflictsAndMultiFileTorrents(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	sourceDir := filepath.Join(root, "Torrent Show")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	video := filepath.Join(sourceDir, "Torrent Show - 01.mkv")
	require.NoError(t, os.WriteFile(video, []byte("video"), 0o600))
	directory := model.LocalAnimeDirectory{Path: root}
	require.NoError(t, db.DB.Create(&directory).Error)
	anime := model.LocalAnime{DirectoryID: directory.ID, Title: "Torrent Show", Path: sourceDir, Season: 1}
	require.NoError(t, db.DB.Create(&anime).Error)
	require.NoError(t, db.DB.Create(&model.LocalEpisode{LocalAnimeID: anime.ID, EpisodeNum: 1, SeasonNum: 1, Path: video}).Error)

	qb := &organizerFakeQB{torrents: []downloader.TorrentInfo{{Hash: "multi", ContentPath: sourceDir, SavePath: root}}}
	preview, err := NewLocalOrganizer(db.DB, qb).Preview("user", LocalOrganizePreviewRequest{Selection: LocalOrganizeSelection{Mode: OrganizeSelectionIDs, AnimeIDs: []uint{anime.ID}}})
	require.NoError(t, err)
	require.Len(t, preview.Items[0].Changes, 1)
	assert.Equal(t, OrganizeStatusSkipped, preview.Items[0].Changes[0].Status)
	assert.Contains(t, preview.Items[0].Changes[0].Reason, "多文件种子")
}

func TestLocalOrganizerUsesQBForSingleFileTorrent(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	sourceDir := filepath.Join(root, "Seeded Show")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	video := filepath.Join(sourceDir, "Seeded Show - 02.mkv")
	require.NoError(t, os.WriteFile(video, []byte("video"), 0o600))
	directory := model.LocalAnimeDirectory{Path: root}
	require.NoError(t, db.DB.Create(&directory).Error)
	anime := model.LocalAnime{DirectoryID: directory.ID, Title: "Seeded Show", Path: sourceDir, Season: 1}
	require.NoError(t, db.DB.Create(&anime).Error)
	episode := model.LocalEpisode{LocalAnimeID: anime.ID, EpisodeNum: 2, SeasonNum: 1, Path: video}
	require.NoError(t, db.DB.Create(&episode).Error)
	qb := &organizerFakeQB{torrents: []downloader.TorrentInfo{{Hash: "single", ContentPath: video, SavePath: sourceDir}}}
	organizer := NewLocalOrganizer(db.DB, qb)
	preview, err := organizer.Preview("user", LocalOrganizePreviewRequest{Selection: LocalOrganizeSelection{Mode: OrganizeSelectionIDs, AnimeIDs: []uint{anime.ID}}})
	require.NoError(t, err)
	require.Len(t, preview.Items[0].Changes, 1)
	assert.True(t, preview.Items[0].Changes[0].ManagedByQB)

	result, err := organizer.Execute(t.Context(), preview, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Moved)
	require.Len(t, qb.renamed, 1)
	assert.Equal(t, [3]string{"single", "Seeded Show - 02.mkv", "Seeded Show - S01E02.mkv"}, qb.renamed[0])
	require.Len(t, qb.locations, 1)
	assert.Equal(t, filepath.Join(root, "Seeded Show", "Season 01"), qb.locations[0][1])
}

func TestLocalOrganizerNeverOverwritesAndRevalidatesSources(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	sourceDir := filepath.Join(root, "Conflict Show")
	targetDir := filepath.Join(sourceDir, "Season 01")
	require.NoError(t, os.MkdirAll(targetDir, 0o755))
	video := filepath.Join(sourceDir, "Conflict Show - 01.mkv")
	require.NoError(t, os.WriteFile(video, []byte("source"), 0o600))
	directory := model.LocalAnimeDirectory{Path: root}
	require.NoError(t, db.DB.Create(&directory).Error)
	anime := model.LocalAnime{DirectoryID: directory.ID, Title: "Conflict Show", Path: sourceDir, Season: 1}
	require.NoError(t, db.DB.Create(&anime).Error)
	require.NoError(t, db.DB.Create(&model.LocalEpisode{LocalAnimeID: anime.ID, EpisodeNum: 1, SeasonNum: 1, Path: video}).Error)
	organizer := NewLocalOrganizer(db.DB, nil)
	request := LocalOrganizePreviewRequest{Selection: LocalOrganizeSelection{Mode: OrganizeSelectionIDs, AnimeIDs: []uint{anime.ID}}}

	target := filepath.Join(targetDir, "Conflict Show - S01E01.mkv")
	require.NoError(t, os.WriteFile(target, []byte("existing"), 0o600))
	conflict, err := organizer.Preview("user", request)
	require.NoError(t, err)
	assert.Equal(t, OrganizeStatusConflict, conflict.Items[0].Changes[0].Status)
	result, err := organizer.Execute(t.Context(), conflict, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Skipped)
	//nolint:gosec // target is inside t.TempDir.
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "existing", string(content))

	require.NoError(t, os.Remove(target))
	stale, err := organizer.Preview("user", request)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(video, []byte("source changed after preview"), 0o600))
	result, err = organizer.Execute(t.Context(), stale, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Skipped)
	_, statErr := os.Stat(target)
	assert.True(t, os.IsNotExist(statErr))
}

func TestLocalOrganizePlanStoreExpiresAndConsumesOnce(t *testing.T) {
	store := NewLocalOrganizePlanStore()
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	store.Put(&LocalOrganizePreview{PlanID: "one", ExpiresAt: now.Add(time.Minute), owner: "user"})
	plan, err := store.Take("one", "user")
	require.NoError(t, err)
	assert.Equal(t, "one", plan.PlanID)
	_, err = store.Take("one", "user")
	assert.ErrorIs(t, err, ErrOrganizePlanNotFound)
	store.Put(&LocalOrganizePreview{PlanID: "two", ExpiresAt: now.Add(time.Minute), owner: "user"})
	now = now.Add(2 * time.Minute)
	_, err = store.Take("two", "user")
	assert.ErrorIs(t, err, ErrOrganizePlanNotFound)
}

type organizerFakeQB struct {
	torrents  []downloader.TorrentInfo
	renamed   [][3]string
	locations [][2]string
}

func (f *organizerFakeQB) ListTorrents() ([]downloader.TorrentInfo, error) { return f.torrents, nil }
func (f *organizerFakeQB) RenameFile(hash, oldPath, newPath string) error {
	f.renamed = append(f.renamed, [3]string{hash, oldPath, newPath})
	return nil
}
func (f *organizerFakeQB) SetLocation(hash, location string) error {
	f.locations = append(f.locations, [2]string{hash, location})
	return nil
}
