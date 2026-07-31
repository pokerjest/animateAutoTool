package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/jellyfin"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncJellyfinLibraryMappingsFindsAlreadyScannedSeries(t *testing.T) {
	withServiceTestDB(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/Items", r.URL.Path)
		assert.Equal(t, "Series,Movie", r.URL.Query().Get("IncludeItemTypes"))
		assert.Equal(t, "test-key", r.Header.Get("X-Emby-Token"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Items":[{"Id":"jf-series-88","Name":"测试番剧","ProviderIds":{"Bangumi":"7788"}}]}`))
	}))
	t.Cleanup(server.Close)

	require.NoError(t, store.NewConfigStore(db.DB).SetMany(map[string]string{
		model.ConfigKeyJellyfinUrl:    server.URL,
		model.ConfigKeyJellyfinApiKey: "test-key",
	}))
	metadata := model.AnimeMetadata{Title: "测试番剧", BangumiID: 7788}
	require.NoError(t, db.DB.Create(&metadata).Error)
	anime := model.LocalAnime{Title: "测试番剧", MetadataID: &metadata.ID}
	require.NoError(t, db.DB.Create(&anime).Error)
	require.NoError(t, db.DB.Create(&model.LocalEpisode{LocalAnimeID: anime.ID, SeasonNum: 1, EpisodeNum: 1, Path: "/library/test-s01e01.mkv"}).Error)

	result, err := SyncJellyfinLibraryMappings(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, result.PendingSeries)
	assert.Equal(t, 1, result.MatchedSeries)

	var updated model.LocalAnime
	require.NoError(t, db.DB.First(&updated, anime.ID).Error)
	assert.Equal(t, "jf-series-88", updated.JellyfinSeriesID)
}

func TestSyncJellyfinLibraryMappingsFallsBackToUniqueExactTitle(t *testing.T) {
	withServiceTestDB(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Items":[{"Id":"jf-title-match","Name":"标题匹配番剧","ProviderIds":{}}]}`))
	}))
	t.Cleanup(server.Close)
	require.NoError(t, store.NewConfigStore(db.DB).SetMany(map[string]string{
		model.ConfigKeyJellyfinUrl: server.URL, model.ConfigKeyJellyfinApiKey: "test-key",
	}))

	metadata := model.AnimeMetadata{TitleCN: "标题匹配番剧"}
	require.NoError(t, db.DB.Create(&metadata).Error)
	anime := model.LocalAnime{Title: "下载目录名称", MetadataID: &metadata.ID}
	require.NoError(t, db.DB.Create(&anime).Error)
	require.NoError(t, db.DB.Create(&model.LocalEpisode{LocalAnimeID: anime.ID, SeasonNum: 1, EpisodeNum: 1, Path: "/library/title-s01e01.mkv"}).Error)

	result, err := SyncJellyfinLibraryMappings(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, result.MatchedSeries)
	var updated model.LocalAnime
	require.NoError(t, db.DB.First(&updated, anime.ID).Error)
	assert.Equal(t, "jf-title-match", updated.JellyfinSeriesID)
}

func TestBuildJellyfinTitleIndexRejectsAmbiguousTitles(t *testing.T) {
	index := buildJellyfinTitleIndex([]jellyfin.LibrarySeries{
		{ID: "series-one", Name: "同名番剧"},
		{ID: "series-two", Name: "同名番剧"},
	})
	assert.Empty(t, index[normalizeJellyfinTitle("同名番剧")])
}

func TestSyncJellyfinLibraryMappingsSkipsSeriesWithoutEpisodes(t *testing.T) {
	withServiceTestDB(t)

	metadata := model.AnimeMetadata{Title: "没有来源 ID"}
	require.NoError(t, db.DB.Create(&metadata).Error)
	require.NoError(t, db.DB.Create(&model.LocalAnime{Title: metadata.Title, MetadataID: &metadata.ID}).Error)

	result, err := SyncJellyfinLibraryMappings(context.Background())
	require.NoError(t, err)
	assert.Zero(t, result.PendingSeries)
	assert.Zero(t, result.MatchedSeries)
}

func TestSyncJellyfinLibraryMappingsForAnimeIDsHandlesSeriesAppearingAcrossPolls(t *testing.T) {
	withServiceTestDB(t)

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"Items":[{"Id":"jf-first","Name":"同时完成番剧 A","ProviderIds":{}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"Items":[{"Id":"jf-first","Name":"同时完成番剧 A","ProviderIds":{}},{"Id":"jf-second","Name":"同时完成番剧 B","ProviderIds":{}}]}`))
	}))
	t.Cleanup(server.Close)
	require.NoError(t, store.NewConfigStore(db.DB).SetMany(map[string]string{
		model.ConfigKeyJellyfinUrl: server.URL, model.ConfigKeyJellyfinApiKey: "test-key",
	}))

	first := model.LocalAnime{Title: "同时完成番剧 A"}
	second := model.LocalAnime{Title: "同时完成番剧 B"}
	require.NoError(t, db.DB.Create(&first).Error)
	require.NoError(t, db.DB.Create(&second).Error)
	require.NoError(t, db.DB.Create(&model.LocalEpisode{
		LocalAnimeID: first.ID, SeasonNum: 1, EpisodeNum: 1, Path: "/library/concurrent-a-s01e01.mkv",
	}).Error)
	require.NoError(t, db.DB.Create(&model.LocalEpisode{
		LocalAnimeID: second.ID, SeasonNum: 1, EpisodeNum: 1, Path: "/library/concurrent-b-s01e01.mkv",
	}).Error)

	firstPass, err := SyncJellyfinLibraryMappingsForAnimeIDs(context.Background(), []uint{first.ID, second.ID})
	require.NoError(t, err)
	assert.Equal(t, 2, firstPass.PendingSeries)
	assert.Equal(t, 1, firstPass.MatchedSeries)

	secondPass, err := SyncJellyfinLibraryMappingsForAnimeIDs(context.Background(), []uint{first.ID, second.ID})
	require.NoError(t, err)
	assert.Equal(t, 1, secondPass.PendingSeries)
	assert.Equal(t, 1, secondPass.MatchedSeries)

	var updated []model.LocalAnime
	require.NoError(t, db.DB.Where("id IN ?", []uint{first.ID, second.ID}).Order("id").Find(&updated).Error)
	require.Len(t, updated, 2)
	assert.Equal(t, "jf-first", updated[0].JellyfinSeriesID)
	assert.Equal(t, "jf-second", updated[1].JellyfinSeriesID)
}
