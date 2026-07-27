package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfiguredJellyfinLibraryIDsDefaultsToAllAndParsesJSON(t *testing.T) {
	configStore := store.NewConfigStore(db.DB)
	previous := configStore.GetDefault(model.ConfigKeyJellyfinLibraryIDs, "")
	t.Cleanup(func() { _ = configStore.Set(model.ConfigKeyJellyfinLibraryIDs, previous) })

	require.NoError(t, configStore.Set(model.ConfigKeyJellyfinLibraryIDs, ""))
	assert.Empty(t, configuredJellyfinLibraryIDs())

	require.NoError(t, configStore.Set(model.ConfigKeyJellyfinLibraryIDs, `["anime","movies",""]`))
	assert.Equal(t, []string{"anime", "movies"}, configuredJellyfinLibraryIDs())
}

func TestJellyfinConfiguredRequiresURLAndAPIKey(t *testing.T) {
	configStore := store.NewConfigStore(db.DB)
	previousURL := configStore.GetDefault(model.ConfigKeyJellyfinUrl, "")
	previousKey := configStore.GetDefault(model.ConfigKeyJellyfinApiKey, "")
	t.Cleanup(func() {
		_ = configStore.Set(model.ConfigKeyJellyfinUrl, previousURL)
		_ = configStore.Set(model.ConfigKeyJellyfinApiKey, previousKey)
	})

	require.NoError(t, configStore.Set(model.ConfigKeyJellyfinUrl, "http://jellyfin.test"))
	require.NoError(t, configStore.Set(model.ConfigKeyJellyfinApiKey, ""))
	assert.False(t, jellyfinConfigured())

	require.NoError(t, configStore.Set(model.ConfigKeyJellyfinApiKey, "test-key"))
	assert.True(t, jellyfinConfigured())
}

func TestJellyfinConnectionStatusUsesSelectedPlaybackSource(t *testing.T) {
	resetAuthFixtures(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "test-key", req.Header.Get("X-Emby-Token"))
		assert.Equal(t, "/System/Info", req.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ProductName":"Jellyfin"}`))
	}))
	t.Cleanup(upstream.Close)

	configStore := store.NewConfigStore(db.DB)
	require.NoError(t, configStore.Set(model.ConfigKeyJellyfinUrl, upstream.URL))
	require.NoError(t, configStore.Set(model.ConfigKeyJellyfinDirectUrl, upstream.URL))
	require.NoError(t, configStore.Set(model.ConfigKeyJellyfinApiKey, "test-key"))

	router := setupRouter()
	cookie, _ := loginCookie(t, router, "admin")
	request := func(target string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Cookie", cookie)
		markLocalRequest(req)
		router.ServeHTTP(recorder, req)
		return recorder
	}

	proxy := request("/api/v1/settings/connections/jellyfin?source=proxy")
	require.Equal(t, http.StatusOK, proxy.Code, proxy.Body.String())
	assert.Contains(t, proxy.Body.String(), `"source":"proxy"`)
	assert.Contains(t, proxy.Body.String(), `"source_label":"AnimateTool 代理"`)
	assert.Contains(t, proxy.Body.String(), `"connected":true`)

	direct := request("/api/v1/settings/connections/jellyfin?source=direct")
	require.Equal(t, http.StatusOK, direct.Code, direct.Body.String())
	assert.Contains(t, direct.Body.String(), `"source":"direct"`)
	assert.Contains(t, direct.Body.String(), `"source_label":"Jellyfin 直连"`)
	assert.Contains(t, direct.Body.String(), `"connected":true`)

	invalid := request("/api/v1/settings/connections/jellyfin?source=netbird")
	assert.Equal(t, http.StatusBadRequest, invalid.Code)
	assert.Contains(t, invalid.Body.String(), "invalid_playback_source")

	require.NoError(t, db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyJellyfinDirectUrl).UpdateColumn("value", "").Error)
	missingDirect := request("/api/v1/settings/connections/jellyfin?source=direct")
	require.Equal(t, http.StatusOK, missingDirect.Code, missingDirect.Body.String())
	assert.Contains(t, missingDirect.Body.String(), "Jellyfin 直连地址未配置")
}

func TestMediaHandlersExposeProviderNeutralJellyfinCatalog(t *testing.T) {
	resetAuthFixtures(t)

	var progressBody string
	var favoriteMethod string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "test-key", req.Header.Get("X-Emby-Token"))
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/Users":
			_, _ = w.Write([]byte(`[{"Id":"user-1","Name":"admin"}]`))
		case "/Library/MediaFolders":
			_, _ = w.Write([]byte(`{"Items":[{"Id":"library-1","Name":"Anime","CollectionType":"tvshows","ChildCount":3},{"Id":"music-1","Name":"Music","CollectionType":"music","ChildCount":8}],"TotalRecordCount":2}`))
		case "/Users/user-1/Items":
			switch {
			case req.URL.Query().Get("ParentId") == "season-1":
				_, _ = w.Write([]byte(`{"Items":[{"Id":"episode-1","Name":"Episode 1","Type":"Episode","ParentId":"season-1","SeriesId":"series-1","SeriesName":"Example","IndexNumber":1,"ParentIndexNumber":1,"RunTimeTicks":12000000000,"UserData":{"PlaybackPositionTicks":3000000000}}],"TotalRecordCount":1}`))
			case req.URL.Query().Get("ParentId") == "series-1":
				_, _ = w.Write([]byte(`{"Items":[{"Id":"season-1","Name":"Season 1","Type":"Season","IndexNumber":1}],"TotalRecordCount":1}`))
			default:
				assert.Equal(t, "1", req.URL.Query().Get("StartIndex"))
				assert.Equal(t, "1", req.URL.Query().Get("Limit"))
				assert.Equal(t, "Example", req.URL.Query().Get("SearchTerm"))
				_, _ = w.Write([]byte(`{"Items":[{"Id":"series-2","Name":"Example 2","Type":"Series","ProductionYear":2026,"UserData":{"IsFavorite":true}}],"TotalRecordCount":3}`))
			}
		case "/Users/user-1/Items/series-1":
			_, _ = w.Write([]byte(`{"Id":"series-1","Name":"Example","Type":"Series","Overview":"Summary","RunTimeTicks":12000000000,"UserData":{"IsFavorite":true}}`))
		case "/Users/user-1/Items/episode-1":
			_, _ = w.Write([]byte(`{"Id":"episode-1","Name":"Episode 1","Type":"Episode","RunTimeTicks":12000000000,"UserData":{"PlaybackPositionTicks":3000000000}}`))
		case "/Users/user-1/Items/Resume":
			_, _ = w.Write([]byte(`{"Items":[{"Id":"episode-1","Name":"Episode 1","Type":"Episode","RunTimeTicks":12000000000,"UserData":{"PlaybackPositionTicks":3000000000}}],"TotalRecordCount":1}`))
		case "/Items/series-1/Images/Primary":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte{0xff, 0xd8, 0xff, 0xd9})
		case "/Sessions/Playing/Progress":
			body, _ := io.ReadAll(req.Body)
			progressBody = string(body)
			_, _ = w.Write([]byte(`{}`))
		case "/Users/user-1/FavoriteItems/series-1":
			favoriteMethod = req.Method
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(upstream.Close)

	configStore := store.NewConfigStore(db.DB)
	for key, value := range map[string]string{
		model.ConfigKeyJellyfinUrl:        upstream.URL,
		model.ConfigKeyJellyfinDirectUrl:  "https://jellyfin.example.test",
		model.ConfigKeyJellyfinApiKey:     "test-key",
		model.ConfigKeyNetBirdProxyURL:    "https://netbird.example.test",
		model.ConfigKeyJellyfinLibraryIDs: `["library-1"]`,
	} {
		require.NoError(t, configStore.Set(key, value))
	}

	router := setupRouter()
	cookie, _ := loginCookie(t, router, "admin")
	request := func(method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Cookie", cookie)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		markLocalRequest(req)
		router.ServeHTTP(recorder, req)
		return recorder
	}

	providers := request(http.MethodGet, "/api/v1/media/providers", "")
	require.Equal(t, http.StatusOK, providers.Code, providers.Body.String())
	assert.Contains(t, providers.Body.String(), `"connected":true`)

	libraries := request(http.MethodGet, "/api/v1/media/providers/jellyfin/libraries", "")
	require.Equal(t, http.StatusOK, libraries.Code, libraries.Body.String())
	assert.Contains(t, libraries.Body.String(), `"selected":true`)
	assert.NotContains(t, libraries.Body.String(), `"music-1"`)

	items := request(http.MethodGet, "/api/v1/media/providers/jellyfin/items?library_id=library-1&q=Example&page=2&page_size=1", "")
	require.Equal(t, http.StatusOK, items.Code, items.Body.String())
	assert.Contains(t, items.Body.String(), `"total":3`)
	assert.Contains(t, items.Body.String(), `"id":"series-2"`)

	detail := request(http.MethodGet, "/api/v1/media/providers/jellyfin/items/series-1", "")
	require.Equal(t, http.StatusOK, detail.Code, detail.Body.String())
	assert.Contains(t, detail.Body.String(), `"poster_url":"/api/v1/media/providers/jellyfin/items/series-1/image"`)
	assert.NotContains(t, detail.Body.String(), "test-key")

	children := request(http.MethodGet, "/api/v1/media/providers/jellyfin/items/series-1/children?type=season", "")
	require.Equal(t, http.StatusOK, children.Code, children.Body.String())
	assert.Contains(t, children.Body.String(), `"id":"season-1"`)

	resume := request(http.MethodGet, "/api/v1/media/providers/jellyfin/continue?page_size=10", "")
	require.Equal(t, http.StatusOK, resume.Code, resume.Body.String())
	assert.Contains(t, resume.Body.String(), `"id":"episode-1"`)

	play := request(http.MethodGet, "/api/v1/media/providers/jellyfin/items/episode-1/play", "")
	require.Equal(t, http.StatusOK, play.Code, play.Body.String())
	assert.Contains(t, play.Body.String(), `"stream_url":"/api/v1/media/providers/jellyfin/items/episode-1/stream"`)
	assert.Contains(t, play.Body.String(), `"netbird_stream_url":"https://netbird.example.test/api/v1/netbird/media/jellyfin/stream/episode-1?token=`)

	image := request(http.MethodGet, "/api/v1/media/providers/jellyfin/items/series-1/image", "")
	require.Equal(t, http.StatusOK, image.Code)
	assert.Equal(t, "image/jpeg", image.Header().Get("Content-Type"))

	progress := request(http.MethodPost, "/api/v1/media/providers/jellyfin/items/episode-1/progress", `{"event":"pause","ticks":123,"duration_ticks":456}`)
	require.Equal(t, http.StatusOK, progress.Code, progress.Body.String())
	assert.Contains(t, progressBody, `"ItemId":"episode-1"`)
	assert.Contains(t, progressBody, `"PositionTicks":123`)

	state := request(http.MethodPut, "/api/v1/media/providers/jellyfin/items/series-1/user-state", `{"favorite":true}`)
	require.Equal(t, http.StatusOK, state.Code, state.Body.String())
	assert.Equal(t, http.MethodPost, favoriteMethod)
	assert.Contains(t, state.Body.String(), `"favorite":true`)

	var payload map[string]any
	require.NoError(t, json.NewDecoder(bytes.NewBuffer(play.Body.Bytes())).Decode(&payload))
	assert.NotNil(t, payload["data"], fmt.Sprintf("unexpected response: %s", play.Body.String()))
}
