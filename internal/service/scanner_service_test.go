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

func TestScanAllReportsEstimatedFolderProgress(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	writeScannerFixture(t, filepath.Join(root, "Progress Show", "Season 01", "Progress Show - 01.mkv"))
	createScannerDirectory(t, root)

	updates := make([]ScanProgress, 0)
	err := NewScannerService().ScanAllWithProgress(func(progress ScanProgress) {
		updates = append(updates, progress)
	})
	require.NoError(t, err)
	require.NotEmpty(t, updates)

	var sawPlanning bool
	var lastCurrent int64
	for _, update := range updates {
		if update.Phase == "planning" && update.FoldersFound >= 3 {
			sawPlanning = true
		}
		if update.Total == 0 {
			continue
		}
		require.GreaterOrEqual(t, update.Current, lastCurrent)
		require.LessOrEqual(t, update.Current, update.Total)
		lastCurrent = update.Current
	}
	require.True(t, sawPlanning)
	last := updates[len(updates)-1]
	require.Equal(t, "complete", last.Phase)
	require.EqualValues(t, 4, last.Total)
	require.Equal(t, last.Total, last.Current)
	require.Contains(t, last.Message, "文件扫描完成")
	require.GreaterOrEqual(t, len(updates), 2)
	finalizing := updates[len(updates)-2]
	require.Equal(t, "finalizing", finalizing.Phase)
	require.Contains(t, finalizing.Message, "1/1 个扫描根目录")
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
	require.Equal(t, 4, result.DiscoveredFiles)
	require.Equal(t, 2, result.CandidateSeries)
	require.Equal(t, 2, result.Added)

	var animes []model.LocalAnime
	require.NoError(t, db.DB.Preload("Episodes").Order("title").Find(&animes).Error)
	require.Len(t, animes, 2)
	byTitle := map[string]model.LocalAnime{}
	for _, anime := range animes {
		byTitle[anime.Title] = anime
	}
	require.Len(t, byTitle["Loose Show"].Episodes, 2)
	require.Equal(t, root, byTitle["Loose Show"].Path)
	require.Len(t, byTitle["Nested Show"].Episodes, 2)
	for _, episode := range byTitle["Nested Show"].Episodes {
		require.Equal(t, 2, episode.SeasonNum)
		require.Greater(t, episode.EpisodeNum, 0)
	}
}

func TestScannerKeepsLooseSeriesMetadataSeparatedAcrossRescans(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	writeScannerFixture(t, filepath.Join(root, "[ANi] Loose Alpha - 01 [1080p].mkv"))
	writeScannerFixture(t, filepath.Join(root, "[ANi] Loose Beta - 01 [1080p].mkv"))
	directory := createScannerDirectory(t, root)
	scanner := NewScannerService()

	_, err := scanner.ScanDirectory(&directory)
	require.NoError(t, err)

	var animes []model.LocalAnime
	require.NoError(t, db.DB.Order("title").Find(&animes).Error)
	require.Len(t, animes, 2)
	for i := range animes {
		require.Equal(t, root, animes[i].Path)
		metadata := model.AnimeMetadata{Title: animes[i].Title}
		require.NoError(t, db.DB.Create(&metadata).Error)
		require.NoError(t, db.DB.Model(&animes[i]).Update("metadata_id", metadata.ID).Error)
	}

	_, err = scanner.ScanDirectory(&directory)
	require.NoError(t, err)

	var rescanned []model.LocalAnime
	require.NoError(t, db.DB.Preload("Metadata").Order("title").Find(&rescanned).Error)
	require.Len(t, rescanned, 2)
	require.Equal(t, "Loose Alpha", rescanned[0].Title)
	require.NotNil(t, rescanned[0].Metadata)
	require.Equal(t, "Loose Alpha", rescanned[0].Metadata.Title)
	require.Equal(t, "Loose Beta", rescanned[1].Title)
	require.NotNil(t, rescanned[1].Metadata)
	require.Equal(t, "Loose Beta", rescanned[1].Metadata.Title)
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

func TestScannerPrefersExplicitFilenameEpisodeOverConflictingNFO(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	showPath := filepath.Join(root, "Explicit Number Show")
	videoPath := filepath.Join(showPath, "[Group] Explicit Number Show [01][AVC-8bit 1080P].mp4")
	writeScannerFixture(t, videoPath)
	nfoPath := strings.TrimSuffix(videoPath, filepath.Ext(videoPath)) + ".nfo"
	require.NoError(t, os.WriteFile(nfoPath, []byte(`<episodedetails><title>Sidecar Title</title><season>1</season><episode>8</episode></episodedetails>`), 0o600))
	directory := createScannerDirectory(t, root)

	_, err := NewScannerService().ScanDirectory(&directory)
	require.NoError(t, err)
	var anime model.LocalAnime
	require.NoError(t, db.DB.Preload("Episodes").First(&anime).Error)
	require.Len(t, anime.Episodes, 1)
	require.Equal(t, "Sidecar Title", anime.Episodes[0].Title)
	require.Equal(t, 1, anime.Episodes[0].EpisodeNum)
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

func TestScannerTargetScanRemovesEmptyDuplicateAtPopulatedPath(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	showPath := filepath.Join(root, "Concurrent Download Show")
	firstEpisode := filepath.Join(showPath, "Concurrent Download Show - 01.mkv")
	secondEpisode := filepath.Join(showPath, "Concurrent Download Show - 02.mkv")
	writeScannerFixture(t, firstEpisode)
	writeScannerFixture(t, secondEpisode)
	directory := createScannerDirectory(t, root)
	scanner := NewScannerService()

	_, err := scanner.ScanDirectory(&directory)
	require.NoError(t, err)
	var populated model.LocalAnime
	require.NoError(t, db.DB.Preload("Episodes").Where("path = ?", showPath).First(&populated).Error)
	require.Len(t, populated.Episodes, 2)

	ghost := model.LocalAnime{
		DirectoryID: directory.ID,
		Title:       populated.Title,
		Path:        showPath,
		FileCount:   0,
	}
	require.NoError(t, db.DB.Create(&ghost).Error)

	result, err := scanner.ScanTargets(&directory, []string{firstEpisode, secondEpisode})
	require.NoError(t, err)
	require.Len(t, result.ScannedScopes, 1)
	require.GreaterOrEqual(t, result.ScanResult.Deleted, 1)

	var animes []model.LocalAnime
	require.NoError(t, db.DB.Where("directory_id = ?", directory.ID).Find(&animes).Error)
	require.Len(t, animes, 1)
	require.Equal(t, populated.ID, animes[0].ID)
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

func TestScannerStoresRichParseEvidenceAndFingerprint(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	video := filepath.Join(root, "Evidence Show", "Season 02", "[Group] Evidence Show 2x03-04v2 [1080p][CHS].mkv")
	writeScannerFixture(t, video)
	directory := createScannerDirectory(t, root)
	scanner := NewScannerService()

	_, err := scanner.ScanDirectory(&directory)
	require.NoError(t, err)
	var episode model.LocalEpisode
	require.NoError(t, db.DB.First(&episode).Error)
	require.Equal(t, 2, episode.SeasonNum)
	require.Equal(t, 3, episode.EpisodeNum)
	require.Equal(t, 4, episode.EpisodeEndNum)
	require.Equal(t, "v2", episode.VersionTag)
	require.Equal(t, "chs", episode.LanguageTag)
	require.Equal(t, "filename:season-x-episode", episode.ParseSource)
	require.Greater(t, episode.ParseConfidence, 0.9)
	require.Len(t, episode.ScanFingerprint, 64)
	firstFingerprint := episode.ScanFingerprint

	require.NoError(t, os.WriteFile(video, []byte("replacement release"), 0o600))
	_, err = scanner.ScanDirectory(&directory)
	require.NoError(t, err)
	require.NoError(t, db.DB.First(&episode, episode.ID).Error)
	require.NotEqual(t, firstFingerprint, episode.ScanFingerprint)
}

func TestScannerTargetScanOnlyUpdatesAffectedSeries(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	showA := filepath.Join(root, "Target Show A", "Season 01")
	showB := filepath.Join(root, "Target Show B", "Season 01")
	writeScannerFixture(t, filepath.Join(showA, "Target Show A - 01.mkv"))
	writeScannerFixture(t, filepath.Join(showB, "Target Show B - 01.mkv"))
	directory := createScannerDirectory(t, root)
	scanner := NewScannerService()
	_, err := scanner.ScanDirectory(&directory)
	require.NoError(t, err)

	var before []model.LocalAnime
	require.NoError(t, db.DB.Preload("Episodes").Order("title").Find(&before).Error)
	require.Len(t, before, 2)
	showBID := before[1].ID
	showBEpisodes := len(before[1].Episodes)

	target := filepath.Join(showA, "Target Show A - 02.mkv")
	writeScannerFixture(t, target)
	result, err := scanner.ScanTargets(&directory, []string{target})
	require.NoError(t, err)
	require.Len(t, result.AffectedAnimeIDs, 1)

	var after []model.LocalAnime
	require.NoError(t, db.DB.Preload("Episodes").Order("title").Find(&after).Error)
	require.Len(t, after, 2)
	require.Len(t, after[0].Episodes, 2)
	require.Equal(t, showBID, after[1].ID)
	require.Len(t, after[1].Episodes, showBEpisodes)
}

func TestScannerTargetScanPreservesOtherSeasons(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	showPath := filepath.Join(root, "Seasoned Show")
	writeScannerFixture(t, filepath.Join(showPath, "Season 01", "Seasoned Show - 01.mkv"))
	directory := createScannerDirectory(t, root)
	scanner := NewScannerService()
	_, err := scanner.ScanDirectory(&directory)
	require.NoError(t, err)

	target := filepath.Join(showPath, "Season 02", "Seasoned Show - 01.mkv")
	writeScannerFixture(t, target)
	result, err := scanner.ScanTargets(&directory, []string{target})
	require.NoError(t, err)
	require.Len(t, result.AffectedAnimeIDs, 1)

	var anime model.LocalAnime
	require.NoError(t, db.DB.Preload("Episodes").First(&anime).Error)
	require.Len(t, anime.Episodes, 2)
	seasons := map[int]bool{}
	for _, episode := range anime.Episodes {
		seasons[episode.SeasonNum] = true
	}
	require.True(t, seasons[1])
	require.True(t, seasons[2])
}

func TestScannerTargetScanFallsBackForLooseRootFiles(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	looseTarget := filepath.Join(root, "Loose Root Show - 02.mkv")
	writeScannerFixture(t, filepath.Join(root, "Loose Root Show - 01.mkv"))
	writeScannerFixture(t, looseTarget)
	writeScannerFixture(t, filepath.Join(root, "Other Show", "Other Show - 01.mkv"))
	directory := createScannerDirectory(t, root)
	scanner := NewScannerService()
	_, err := scanner.ScanDirectory(&directory)
	require.NoError(t, err)

	result, err := scanner.ScanTargets(&directory, []string{looseTarget})
	require.NoError(t, err)
	require.Contains(t, result.ScannedScopes, root)
	require.Len(t, result.AffectedAnimeIDs, 1)
}

func TestScannerTargetScanFallsBackWhenExistingSeriesSpansScope(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	first := filepath.Join(root, "Group A", "Same Scope Show", "Same Scope Show - 01.mkv")
	second := filepath.Join(root, "Group B", "Same Scope Show", "Same Scope Show - 02.mkv")
	writeScannerFixture(t, first)
	writeScannerFixture(t, second)
	directory := createScannerDirectory(t, root)
	scanner := NewScannerService()
	_, err := scanner.ScanDirectory(&directory)
	require.NoError(t, err)

	result, err := scanner.ScanTargets(&directory, []string{first})
	require.NoError(t, err)
	require.Contains(t, result.ScannedScopes, root)

	var anime model.LocalAnime
	require.NoError(t, db.DB.Preload("Episodes").First(&anime).Error)
	require.Len(t, anime.Episodes, 2)
}

func TestScannerKeepsSpecialsInSeasonZero(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	video := filepath.Join(root, "Special Show", "Specials", "Special Show OVA - 01.mkv")
	writeScannerFixture(t, video)
	directory := createScannerDirectory(t, root)

	_, err := NewScannerService().ScanDirectory(&directory)
	require.NoError(t, err)
	var episode model.LocalEpisode
	require.NoError(t, db.DB.First(&episode).Error)
	require.Equal(t, 0, episode.SeasonNum)
	require.Equal(t, "special", episode.EpisodeType)
}

func TestScannerUsesSpecialDirectoryEvidenceForPlainEpisodeNames(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	video := filepath.Join(root, "Folder Special Show", "Specials", "Folder Special Show - 01.mkv")
	writeScannerFixture(t, video)
	directory := createScannerDirectory(t, root)

	_, err := NewScannerService().ScanDirectory(&directory)
	require.NoError(t, err)
	var episode model.LocalEpisode
	require.NoError(t, db.DB.First(&episode).Error)
	require.Equal(t, 0, episode.SeasonNum)
	require.Equal(t, "special", episode.EpisodeType)
	require.Equal(t, "directory:special", episode.ParseSource)
}
