package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func postPlaybackProgress(t *testing.T, router http.Handler, cookie string, input PlaybackProgressInput) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playback/progress", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	router.ServeHTTP(recorder, req)
	return recorder
}

func getContinueWatching(t *testing.T, router http.Handler, cookie string) []ContinueWatchingItem {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/playback/continue?limit=10", nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var payload struct {
		Data struct {
			Items []ContinueWatchingItem `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	return payload.Data.Items
}

func seedPlaybackAnime(t *testing.T, suffix int) (model.LocalAnime, model.LocalEpisode, model.LocalEpisode) {
	t.Helper()
	metadata := model.AnimeMetadata{Title: fmt.Sprintf("Playback %d", suffix), TitleCN: fmt.Sprintf("继续观看 %d", suffix), BangumiID: 980000 + suffix}
	require.NoError(t, db.DB.Create(&metadata).Error)
	anime := model.LocalAnime{Title: metadata.Title, Path: fmt.Sprintf("/tmp/playback-%d", suffix), MetadataID: &metadata.ID}
	require.NoError(t, db.DB.Create(&anime).Error)
	first := model.LocalEpisode{LocalAnimeID: anime.ID, Title: "Episode 1", SeasonNum: 1, EpisodeNum: 1, Path: fmt.Sprintf("/tmp/playback-%d-01.mkv", suffix)}
	second := model.LocalEpisode{LocalAnimeID: anime.ID, Title: "Episode 2", SeasonNum: 1, EpisodeNum: 2, Path: fmt.Sprintf("/tmp/playback-%d-02.mkv", suffix)}
	require.NoError(t, db.DB.Create(&first).Error)
	require.NoError(t, db.DB.Create(&second).Error)
	t.Cleanup(func() {
		_ = db.DB.Unscoped().Where("local_anime_id = ?", anime.ID).Delete(&model.PlaybackHistory{}).Error
		_ = db.DB.Unscoped().Where("local_anime_id = ?", anime.ID).Delete(&model.LocalEpisode{}).Error
		_ = db.DB.Unscoped().Delete(&model.LocalAnime{}, anime.ID).Error
		_ = db.DB.Unscoped().Delete(&model.AnimeMetadata{}, metadata.ID).Error
	})
	return anime, first, second
}

func TestPlaybackProgressPersistsLocallyAndAdvancesOnlyAfterEnded(t *testing.T) {
	resetAuthFixtures(t)
	router := setupRouter()
	cookie, _ := loginCookie(t, router, "admin")
	anime, first, second := seedPlaybackAnime(t, 71)

	recorder := postPlaybackProgress(t, router, cookie, PlaybackProgressInput{EpisodeID: first.ID, Event: "timeupdate", Ticks: 900, DurationTicks: 1000})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var progressResponse struct {
		Data struct {
			JellyfinSynced bool `json:"jellyfin_synced"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &progressResponse))
	assert.False(t, progressResponse.Data.JellyfinSynced, "local progress must survive when Jellyfin is unavailable")

	items := getContinueWatching(t, router, cookie)
	require.Len(t, items, 1)
	assert.Equal(t, anime.ID, items[0].AnimeID)
	assert.Equal(t, first.ID, items[0].EpisodeID)
	assert.InDelta(t, 90, items[0].ProgressPercent, 0.01)

	recorder = postPlaybackProgress(t, router, cookie, PlaybackProgressInput{EpisodeID: first.ID, Event: "ended", Ticks: 1000, DurationTicks: 1000})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	items = getContinueWatching(t, router, cookie)
	require.Len(t, items, 1)
	assert.Equal(t, second.ID, items[0].EpisodeID)
	assert.Zero(t, items[0].PositionTicks)

	recorder = postPlaybackProgress(t, router, cookie, PlaybackProgressInput{EpisodeID: second.ID, Event: "ended", Ticks: 1000, DurationTicks: 1000})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Empty(t, getContinueWatching(t, router, cookie), "a finished series without a next episode should leave continue watching")

	recorder = postPlaybackProgress(t, router, cookie, PlaybackProgressInput{EpisodeID: first.ID, Event: "restart", Ticks: 1000, DurationTicks: 1000})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	items = getContinueWatching(t, router, cookie)
	require.Len(t, items, 1)
	assert.Equal(t, first.ID, items[0].EpisodeID)
	assert.Zero(t, items[0].PositionTicks)

	admin, err := store.NewUserStore(db.DB).GetByUsername("admin")
	require.NoError(t, err)
	editor, err := service.NewAuthService().CreateUser("playback-editor", "editor-pass-123")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DB.Unscoped().Delete(&model.User{}, editor.ID).Error })
	adminRows, err := store.NewPlaybackHistoryStore(db.DB).ListRecent(admin.ID, 10)
	require.NoError(t, err)
	editorRows, err := store.NewPlaybackHistoryStore(db.DB).ListRecent(editor.ID, 10)
	require.NoError(t, err)
	assert.NotEmpty(t, adminRows)
	assert.Empty(t, editorRows)
}

func TestContinueWatchingDeduplicatesAnimeSortsAndLimits(t *testing.T) {
	resetAuthFixtures(t)
	router := setupRouter()
	cookie, _ := loginCookie(t, router, "admin")
	admin, err := store.NewUserStore(db.DB).GetByUsername("admin")
	require.NoError(t, err)
	base := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	var newestAnime model.LocalAnime
	for index := range 11 {
		anime, first, second := seedPlaybackAnime(t, 800+index)
		if index == 10 {
			newestAnime = anime
		}
		require.NoError(t, store.NewPlaybackHistoryStore(db.DB).Upsert(&model.PlaybackHistory{
			UserID: admin.ID, LocalAnimeID: anime.ID, LocalEpisodeID: first.ID,
			PositionTicks: int64(100 + index), DurationTicks: 1000, LastEvent: "pause", LastPlayedAt: base.Add(time.Duration(index) * time.Minute),
		}))
		if index == 10 {
			require.NoError(t, store.NewPlaybackHistoryStore(db.DB).Upsert(&model.PlaybackHistory{
				UserID: admin.ID, LocalAnimeID: anime.ID, LocalEpisodeID: second.ID,
				PositionTicks: 50, DurationTicks: 1000, LastEvent: "pause", LastPlayedAt: base.Add(-time.Minute),
			}))
		}
	}

	items := getContinueWatching(t, router, cookie)
	require.Len(t, items, 10)
	assert.Equal(t, newestAnime.ID, items[0].AnimeID)
	seen := make(map[uint]struct{}, len(items))
	for index, item := range items {
		_, duplicate := seen[item.AnimeID]
		assert.False(t, duplicate, "anime %d appeared more than once", item.AnimeID)
		seen[item.AnimeID] = struct{}{}
		if index > 0 {
			assert.False(t, items[index-1].UpdatedAt.Before(item.UpdatedAt))
		}
	}
}

func TestContinueWatchingImportsJellyfinResumeItems(t *testing.T) {
	resetAuthFixtures(t)
	_, first, _ := seedPlaybackAnime(t, 72)
	first.JellyfinItemID = "resume-episode-1"
	require.NoError(t, db.DB.Save(&first).Error)

	jellyfinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/Users":
			_, _ = w.Write([]byte(`[{"Id":"user-1","Name":"admin"}]`))
		case "/Users/user-1/Items/Resume":
			_, _ = w.Write([]byte(`{"Items":[{"Id":"resume-episode-1","RunTimeTicks":2000,"UserData":{"PlaybackPositionTicks":500,"Played":false,"LastPlayedDate":"2026-07-26T10:00:00Z"}}]}`))
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(jellyfinServer.Close)
	require.NoError(t, db.DB.Save(&model.GlobalConfig{Key: model.ConfigKeyJellyfinUrl, Value: jellyfinServer.URL}).Error)
	require.NoError(t, db.DB.Save(&model.GlobalConfig{Key: model.ConfigKeyJellyfinApiKey, Value: "test-key"}).Error)

	router := setupRouter()
	cookie, _ := loginCookie(t, router, "admin")
	items := getContinueWatching(t, router, cookie)
	require.Len(t, items, 1)
	assert.Equal(t, first.ID, items[0].EpisodeID)
	assert.Equal(t, int64(500), items[0].PositionTicks)
}

func TestProxyVideoPreservesRangeResponse(t *testing.T) {
	resetAuthFixtures(t)
	_, first, _ := seedPlaybackAnime(t, 73)
	first.JellyfinItemID = "range-episode-1"
	require.NoError(t, db.DB.Save(&first).Error)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "bytes=0-3", req.Header.Get("Range"))
		assert.Equal(t, "/Videos/range-episode-1/stream", req.URL.Path)
		assert.Equal(t, "true", req.URL.Query().Get("static"))
		assert.Equal(t, "test-key", req.URL.Query().Get("api_key"))
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Range", "bytes 0-3/8")
		w.Header().Set("Content-Length", "4")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("test"))
	}))
	t.Cleanup(upstream.Close)
	require.NoError(t, db.DB.Save(&model.GlobalConfig{Key: model.ConfigKeyJellyfinUrl, Value: upstream.URL}).Error)
	require.NoError(t, db.DB.Save(&model.GlobalConfig{Key: model.ConfigKeyJellyfinApiKey, Value: "test-key"}).Error)

	router := setupRouter()
	cookie, _ := loginCookie(t, router, "admin")
	appServer := httptest.NewServer(router)
	t.Cleanup(appServer.Close)
	req, err := http.NewRequest(http.MethodGet, appServer.URL+"/api/v1/jellyfin/stream/"+strconv.FormatUint(uint64(first.ID), 10), nil)
	require.NoError(t, err)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Range", "bytes=0-3")
	markLocalRequest(req)
	response, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusPartialContent, response.StatusCode)
	assert.Equal(t, "bytes", response.Header.Get("Accept-Ranges"))
	assert.Equal(t, "bytes 0-3/8", response.Header.Get("Content-Range"))
	assert.Equal(t, "4", response.Header.Get("Content-Length"))
	assert.Equal(t, "test", string(body))
}
