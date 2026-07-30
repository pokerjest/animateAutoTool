package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/stretchr/testify/require"
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
