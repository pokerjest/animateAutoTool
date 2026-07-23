package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/stretchr/testify/require"
)

func writeScannerFixture(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("video"), 0o600))
}

func createScannerDirectory(t *testing.T, root string) model.LocalAnimeDirectory {
	t.Helper()
	directory := model.LocalAnimeDirectory{Path: root}
	require.NoError(t, db.DB.Create(&directory).Error)
	return directory
}

func TestScannerGroupsLooseFilesAndNestedSeasonDirectories(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	writeScannerFixture(t, filepath.Join(root, "[ANi] Loose Show - 01 [1080p].mkv"))
	writeScannerFixture(t, filepath.Join(root, "[ANi] Loose Show - 02 [1080p].mkv"))
	writeScannerFixture(t, filepath.Join(root, "2026", "Nested Show", "Season 02", "01.mkv"))
	writeScannerFixture(t, filepath.Join(root, "2026", "Nested Show", "Season 02", "02.mkv"))
	directory := createScannerDirectory(t, root)

	result, err := NewScannerService().ScanDirectory(&directory)
	require.NoError(t, err)
	require.Equal(t, 2, result.Added)

	var animes []model.LocalAnime
	require.NoError(t, db.DB.Preload("Episodes").Order("title").Find(&animes).Error)
	require.Len(t, animes, 2)
	byTitle := map[string]model.LocalAnime{}
	for _, anime := range animes {
		byTitle[anime.Title] = anime
	}
	require.Len(t, byTitle["Loose Show"].Episodes, 2)
	require.Len(t, byTitle["Nested Show"].Episodes, 2)
	for _, episode := range byTitle["Nested Show"].Episodes {
		require.Equal(t, 2, episode.SeasonNum)
		require.Greater(t, episode.EpisodeNum, 0)
	}
}

func TestScannerUsesSeriesAndEpisodeNFOData(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	showPath := filepath.Join(root, "Cryptic Folder")
	videoPath := filepath.Join(showPath, "Season 03", "0001.mkv")
	writeScannerFixture(t, videoPath)
	require.NoError(t, os.WriteFile(filepath.Join(showPath, "tvshow.nfo"), []byte(`<tvshow><title>NFO Show Title</title></tvshow>`), 0o600))
	require.NoError(t, os.WriteFile(strings.TrimSuffix(videoPath, filepath.Ext(videoPath))+".nfo", []byte(`<episodedetails><title>The NFO Episode</title><season>3</season><episode>7</episode></episodedetails>`), 0o600))
	directory := createScannerDirectory(t, root)

	_, err := NewScannerService().ScanDirectory(&directory)
	require.NoError(t, err)
	var anime model.LocalAnime
	require.NoError(t, db.DB.Preload("Episodes").First(&anime).Error)
	require.Equal(t, "NFO Show Title", anime.Title)
	require.Len(t, anime.Episodes, 1)
	require.Equal(t, "The NFO Episode", anime.Episodes[0].Title)
	require.Equal(t, 3, anime.Episodes[0].SeasonNum)
	require.Equal(t, 7, anime.Episodes[0].EpisodeNum)
}

func TestScannerConsolidatesDuplicateReleaseFoldersAndPreservesMetadata(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	groupA := filepath.Join(root, "[GroupA] Same Show")
	groupB := filepath.Join(root, "[GroupB] Same Show")
	seasonTwo := filepath.Join(root, "Same Show Season 2")
	fileA := filepath.Join(groupA, "[GroupA] Same Show - 01.mkv")
	fileB := filepath.Join(groupB, "[GroupB] Same Show - 02.mkv")
	fileC := filepath.Join(seasonTwo, "Same Show.S02E01.mkv")
	writeScannerFixture(t, fileA)
	writeScannerFixture(t, fileB)
	writeScannerFixture(t, fileC)
	directory := createScannerDirectory(t, root)

	metadata := model.AnimeMetadata{Title: "Same Show", BangumiID: 12345}
	require.NoError(t, db.DB.Create(&metadata).Error)
	duplicateA := model.LocalAnime{DirectoryID: directory.ID, Title: "[GroupA] Same Show", Path: groupA}
	duplicateB := model.LocalAnime{DirectoryID: directory.ID, Title: "[GroupB] Same Show", Path: groupB, MetadataID: &metadata.ID}
	require.NoError(t, db.DB.Create(&duplicateA).Error)
	require.NoError(t, db.DB.Create(&duplicateB).Error)
	require.NoError(t, db.DB.Create(&model.LocalEpisode{LocalAnimeID: duplicateA.ID, Path: fileA, SeasonNum: 1, EpisodeNum: 1}).Error)
	require.NoError(t, db.DB.Create(&model.LocalEpisode{LocalAnimeID: duplicateB.ID, Path: fileB, SeasonNum: 1, EpisodeNum: 2}).Error)

	_, err := NewScannerService().ScanDirectory(&directory)
	require.NoError(t, err)

	var animes []model.LocalAnime
	require.NoError(t, db.DB.Preload("Episodes").Find(&animes).Error)
	require.Len(t, animes, 1)
	require.Equal(t, duplicateB.ID, animes[0].ID)
	require.NotNil(t, animes[0].MetadataID)
	require.Equal(t, metadata.ID, *animes[0].MetadataID)
	require.Len(t, animes[0].Episodes, 3)
	seasons := map[int]bool{}
	for _, episode := range animes[0].Episodes {
		seasons[episode.SeasonNum] = true
	}
	require.True(t, seasons[1])
	require.True(t, seasons[2])
}

func TestScannerKeepsSameNamedRemakesWithDifferentYearsSeparate(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	writeScannerFixture(t, filepath.Join(root, "Same Name (2001)", "Same Name - 01.mkv"))
	writeScannerFixture(t, filepath.Join(root, "Same Name (2019)", "Same Name - 01.mkv"))
	directory := createScannerDirectory(t, root)

	_, err := NewScannerService().ScanDirectory(&directory)
	require.NoError(t, err)
	var animes []model.LocalAnime
	require.NoError(t, db.DB.Order("title").Find(&animes).Error)
	require.Len(t, animes, 2)
	require.Equal(t, "Same Name (2001)", animes[0].Title)
	require.Equal(t, "Same Name (2019)", animes[1].Title)
}

func TestScannerRestoresEpisodeWhenAFileReturns(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	showPath := filepath.Join(root, "Returning Show")
	episodePath := filepath.Join(showPath, "Returning Show - 01.mkv")
	writeScannerFixture(t, episodePath)
	directory := createScannerDirectory(t, root)
	scanner := NewScannerService()

	_, err := scanner.ScanDirectory(&directory)
	require.NoError(t, err)
	require.NoError(t, os.Remove(episodePath))
	_, err = scanner.ScanDirectory(&directory)
	require.NoError(t, err)
	writeScannerFixture(t, episodePath)
	_, err = scanner.ScanDirectory(&directory)
	require.NoError(t, err)

	var animeCount int64
	var episodeCount int64
	require.NoError(t, db.DB.Model(&model.LocalAnime{}).Count(&animeCount).Error)
	require.NoError(t, db.DB.Model(&model.LocalEpisode{}).Count(&episodeCount).Error)
	require.EqualValues(t, 1, animeCount)
	require.EqualValues(t, 1, episodeCount)
}

func TestScannerKeepsExistingRecordWhenSeasonFolderIsRenamed(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	originalPath := filepath.Join(root, "Renamed Show Season 1")
	originalEpisode := filepath.Join(originalPath, "Renamed Show - 01.mkv")
	writeScannerFixture(t, originalEpisode)
	directory := createScannerDirectory(t, root)
	scanner := NewScannerService()
	_, err := scanner.ScanDirectory(&directory)
	require.NoError(t, err)

	var original model.LocalAnime
	require.NoError(t, db.DB.First(&original).Error)
	metadata := model.AnimeMetadata{Title: "Renamed Show", BangumiID: 54321}
	require.NoError(t, db.DB.Create(&metadata).Error)
	require.NoError(t, db.DB.Model(&original).Update("metadata_id", metadata.ID).Error)

	renamedPath := filepath.Join(root, "Renamed Show S01")
	require.NoError(t, os.Rename(originalPath, renamedPath))
	_, err = scanner.ScanDirectory(&directory)
	require.NoError(t, err)

	var animes []model.LocalAnime
	require.NoError(t, db.DB.Preload("Episodes").Find(&animes).Error)
	require.Len(t, animes, 1)
	require.Equal(t, original.ID, animes[0].ID)
	require.Equal(t, renamedPath, animes[0].Path)
	require.NotNil(t, animes[0].MetadataID)
	require.Len(t, animes[0].Episodes, 1)
	require.Contains(t, animes[0].Episodes[0].Path, "Renamed Show S01")
}
