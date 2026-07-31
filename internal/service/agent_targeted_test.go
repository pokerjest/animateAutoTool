package service

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/stretchr/testify/require"
)

func TestRunAgentForAnimeIDsOnlyEnrichesRequestedSeries(t *testing.T) {
	withServiceTestDB(t)
	directory := model.LocalAnimeDirectory{Path: t.TempDir()}
	require.NoError(t, db.DB.Create(&directory).Error)
	first := model.LocalAnime{DirectoryID: directory.ID, Title: "First Target", Path: t.TempDir()}
	second := model.LocalAnime{DirectoryID: directory.ID, Title: "Second Target", Path: t.TempDir()}
	require.NoError(t, db.DB.Create(&first).Error)
	require.NoError(t, db.DB.Create(&second).Error)

	var mu sync.Mutex
	var calls []uint
	agent := &AgentService{
		NetworkWorkerCount: 1,
		NetworkRateLimit:   0,
		enrichAnime: func(anime *model.LocalAnime) error {
			mu.Lock()
			calls = append(calls, anime.ID)
			mu.Unlock()
			return nil
		},
	}
	agent.RunAgentForAnimeIDs([]uint{first.ID, first.ID, 0, 999999})

	require.Equal(t, []uint{first.ID}, calls)
}

func TestScanLocalAssetsRepairsMissingMetadataAssociation(t *testing.T) {
	withServiceTestDB(t)

	animePath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(animePath, "tvshow.nfo"), []byte(`<?xml version="1.0" encoding="UTF-8"?>
<tvshow>
  <title>Recovered from NFO</title>
  <uniqueid type="anilist">12345</uniqueid>
</tvshow>`), 0o600))

	staleMetadata := model.AnimeMetadata{Title: "Deleted metadata"}
	require.NoError(t, db.DB.Create(&staleMetadata).Error)
	anime := model.LocalAnime{
		Title:      "Dangling Association",
		Path:       animePath,
		MetadataID: &staleMetadata.ID,
	}
	require.NoError(t, db.DB.Create(&anime).Error)
	require.NoError(t, db.DB.Delete(&staleMetadata).Error)

	var loaded model.LocalAnime
	require.NoError(t, db.DB.Preload("Metadata").First(&loaded, anime.ID).Error)
	require.NotNil(t, loaded.MetadataID)
	require.Nil(t, loaded.Metadata)

	NewAgentService().scanLocalAssets(&loaded)

	var repaired model.LocalAnime
	require.NoError(t, db.DB.Preload("Metadata").First(&repaired, anime.ID).Error)
	require.NotNil(t, repaired.MetadataID)
	require.NotNil(t, repaired.Metadata)
	require.NotEqual(t, staleMetadata.ID, *repaired.MetadataID)
	require.Equal(t, "Recovered from NFO", repaired.Metadata.Title)
	require.Equal(t, 12345, repaired.Metadata.AniListID)
}
