package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestNFOGeneratorPreservesLocalFieldsAndAddsMissingMetadata(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	path := filepath.Join(root, "tvshow.nfo")
	require.NoError(t, os.WriteFile(path, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<tvshow><title>用户手工标题</title><plot>手工简介</plot><uniqueid type="tmdb">42</uniqueid></tvshow>`), 0o600))

	anime := &model.LocalAnime{
		Title: "Network Title",
		Path:  root,
		Metadata: &model.AnimeMetadata{
			Title:         "Network Title",
			Summary:       "Network summary",
			Genres:        `["Animation","Drama"]`,
			Studios:       `["Studio A"]`,
			BangumiID:     100,
			BangumiRating: 8.5,
		},
	}
	require.NoError(t, NewNFOGeneratorService().GenerateTVShowNFO(anime))

	nfo, err := parser.ParseTVShowNFO(path)
	require.NoError(t, err)
	require.Equal(t, "用户手工标题", nfo.Title)
	require.Equal(t, "手工简介", nfo.Plot)
	require.ElementsMatch(t, []string{"Animation", "Drama"}, nfo.Genre)
	require.ElementsMatch(t, []string{"Studio A"}, nfo.Studio)
	require.Len(t, nfo.UniqueIDs, 2)

	matches, err := filepath.Glob(filepath.Join(root, ".animate-atomic-*"))
	require.NoError(t, err)
	require.Empty(t, matches)
}

func TestNFOGeneratorWritesEpisodeRangeAndType(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	video := filepath.Join(root, "Show - S00E01-E02.mkv")
	require.NoError(t, os.WriteFile(video, []byte("video"), 0o600))
	anime := &model.LocalAnime{Title: "Show", Path: root}
	episode := &model.LocalEpisode{
		Title: "Special collection", Path: video, SeasonNum: 0, EpisodeNum: 1,
		EpisodeEndNum: 2, EpisodeType: "special", ParsedTitle: "Show",
	}
	require.NoError(t, NewNFOGeneratorService().GenerateEpisodeNFO(episode, anime))
	nfo, err := parser.ParseEpisodeNFO(filepath.Join(root, "Show - S00E01-E02.nfo"))
	require.NoError(t, err)
	require.Equal(t, 0, nfo.Season)
	require.Equal(t, 1, nfo.Episode)
	require.Equal(t, 2, nfo.EpisodeEnd)
	require.Equal(t, "special", nfo.Type)
}

func TestNFOGeneratorDoesNotTreatMissingLocalSeasonAsZero(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	video := filepath.Join(root, "Show - S02E01.mkv")
	require.NoError(t, os.WriteFile(video, []byte("video"), 0o600))
	nfoPath := filepath.Join(root, "Show - S02E01.nfo")
	require.NoError(t, os.WriteFile(nfoPath, []byte(`<episodedetails><title>手工标题</title></episodedetails>`), 0o600))

	anime := &model.LocalAnime{Title: "Show", Path: root}
	episode := &model.LocalEpisode{Title: "Network episode", Path: video, SeasonNum: 2, EpisodeNum: 1}
	require.NoError(t, NewNFOGeneratorService().GenerateEpisodeNFO(episode, anime))

	nfo, err := parser.ParseEpisodeNFO(nfoPath)
	require.NoError(t, err)
	require.Equal(t, 2, nfo.Season)
	require.Equal(t, 1, nfo.Episode)
	require.Equal(t, "手工标题", nfo.Title)
}

func TestNFOGeneratorUsesParentDirectoryForLegacyFilePath(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	videoPath := filepath.Join(root, "Loose Show - 01.mp4")
	require.NoError(t, os.WriteFile(videoPath, []byte("video"), 0o600))

	anime := &model.LocalAnime{
		Title: "Loose Show",
		Path:  videoPath,
		Metadata: &model.AnimeMetadata{
			Title: "Loose Show",
		},
	}
	require.NoError(t, NewNFOGeneratorService().GenerateTVShowNFO(anime))
	_, err := os.Stat(filepath.Join(root, "tvshow.nfo"))
	require.NoError(t, err)
	require.Empty(t, mustOpenLibraryIssues(t))
}

func TestNFOGeneratorReportsWriteFailureWithoutReadOnlyFalsePositive(t *testing.T) {
	withServiceTestDB(t)
	root := t.TempDir()
	videoPath := filepath.Join(root, "Locked Show - 01.mp4")
	require.NoError(t, os.WriteFile(videoPath, []byte("video"), 0o600))

	legacy := model.LibraryIssue{
		IssueKey:      "readonly:23",
		IssueType:     LibraryIssueTypeScrape,
		Status:        LibraryIssueStatusOpen,
		Title:         "媒体目录只读 (Directory Read-Only)",
		DirectoryPath: videoPath,
	}
	require.NoError(t, db.DB.Create(&legacy).Error)

	originalProbe := probeMediaDirectoryWritable
	probeMediaDirectoryWritable = func(path string) error {
		require.Equal(t, root, path)
		return errors.New("sharing violation")
	}
	t.Cleanup(func() { probeMediaDirectoryWritable = originalProbe })

	anime := &model.LocalAnime{Model: gorm.Model{ID: 23}, Title: "Locked Show", Path: videoPath}
	_, err := NewNFOGeneratorService().checkAndReportWritability(anime)
	require.Error(t, err)

	issues := mustOpenLibraryIssues(t)
	require.Len(t, issues, 1)
	require.Equal(t, "media-write:23", issues[0].IssueKey)
	require.Equal(t, root, issues[0].DirectoryPath)
	require.NotContains(t, issues[0].Title, "只读")
	require.Contains(t, issues[0].Message, "sharing violation")
}

func mustOpenLibraryIssues(t *testing.T) []model.LibraryIssue {
	t.Helper()
	issues, err := ListOpenLibraryIssues(20)
	require.NoError(t, err)
	return issues
}
