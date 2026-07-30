package service

import (
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
