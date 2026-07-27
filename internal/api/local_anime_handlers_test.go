package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalAnimePageIncludesHighlightID(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/local-anime?highlight=42", nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected local anime page to succeed, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.Len() == 0 {
		t.Fatalf("expected local anime page to render body, got empty response")
	}

	assert.Contains(t, w.Body.String(), "highlightAnimeId: 42")
	assert.Contains(t, w.Body.String(), "autoOpenAnimeId: 0")
}

func TestLocalAnimePageIncludesAutoOpenAnimeID(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/local-anime?highlight=42&open=1", nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected local anime page with auto-open to succeed, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.Len() == 0 {
		t.Fatalf("expected local anime page with auto-open to render body, got empty response")
	}

	assert.Contains(t, w.Body.String(), "highlightAnimeId: 42")
	assert.Contains(t, w.Body.String(), "autoOpenAnimeId: 42")
}

func TestLocalAnimePageIncludesFocusedEpisodePath(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/local-anime?highlight=42&open=1&focus_episode=%2Fdownloads%2Fshow%2F01.mkv", nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected local anime page with focused episode to succeed, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.Len() == 0 {
		t.Fatalf("expected local anime page with focused episode to render body, got empty response")
	}

	assert.Contains(t, w.Body.String(), "autoFocusEpisodePath:")
	assert.Contains(t, w.Body.String(), "/downloads/show/01.mkv")
}

func TestRenderLocalAnimeTemplateIncludesDeepLinkState(t *testing.T) {
	html, err := renderTemplateToString("local_anime.html", LocalAnimeData{
		SkipLayout:       true,
		HighlightAnimeID: 42,
		AutoOpenAnimeID:  42,
		AutoFocusEpisode: "/downloads/show/01.mkv",
		AnimeList:        []model.LocalAnime{},
	})
	if err != nil {
		t.Fatalf("expected local anime template to render, got error: %v", err)
	}

	assert.Contains(t, html, "highlightAnimeId: 42")
	assert.Contains(t, html, "autoOpenAnimeId: 42")
	assert.Contains(t, html, "autoFocusEpisodePath:")
	assert.Contains(t, html, "/downloads/show/01.mkv")
	assert.Contains(t, html, "刚完成")
}

func TestLocalAnimeDiagnosticsEndpointRendersOpenIssues(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	animeID := uint(9)
	if err := service.ReportLibraryIssue(service.LibraryIssueInput{
		IssueKey:      "scrape:9",
		IssueType:     service.LibraryIssueTypeScrape,
		Title:         "Problem Show",
		DirectoryPath: "/library/Problem Show",
		LocalAnimeID:  &animeID,
		Message:       "tmdb token missing",
		Hint:          "检查元数据配置",
	}); err != nil {
		t.Fatalf("failed to seed library issue: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/local-anime/diagnostics", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected diagnostics endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "刮削失败")
	assert.Contains(t, w.Body.String(), "Problem Show")
	assert.Contains(t, w.Body.String(), "tmdb token missing")
}

func TestLocalAnimeCardEndpointRendersSingleCard(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	metadata := model.AnimeMetadata{Title: "Card Show", BangumiTitle: "Card Show"}
	if err := db.DB.Create(&metadata).Error; err != nil {
		t.Fatalf("failed to create metadata: %v", err)
	}

	anime := model.LocalAnime{
		Title:      "Card Show",
		Path:       "/library/card-show",
		FileCount:  3,
		TotalSize:  1024,
		MetadataID: &metadata.ID,
	}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("failed to create anime: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/local-anime/%d/card", anime.ID), nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected card endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), fmt.Sprintf(`id="local-card-%d"`, anime.ID))
	assert.Contains(t, w.Body.String(), "Card Show")
}

func TestLocalAnimeScanStatusEndpointRendersLatestSummary(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	service.GlobalScanStatus.Begin(2)
	service.GlobalScanStatus.Advance("/library/Show A", &service.ScanResult{DiscoveredFiles: 24, CandidateSeries: 2, Added: 2, Updated: 1}, nil)
	service.GlobalScanStatus.Advance("/library/Show B", nil, fmt.Errorf("permission denied"))
	service.GlobalScanStatus.Finish()

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/local-anime/scan-status", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected scan status endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "扫描任务摘要")
	assert.Contains(t, w.Body.String(), "最近一轮扫描了 2 个目录")
	assert.Contains(t, w.Body.String(), "permission denied")
}

func TestGetPlayInfoReturnsDiagnosticWhenJellyfinNotConfigured(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	metadata := model.AnimeMetadata{Title: "No Jellyfin Yet", BangumiID: 1001}
	if err := db.DB.Create(&metadata).Error; err != nil {
		t.Fatalf("failed to create metadata: %v", err)
	}

	anime := model.LocalAnime{Title: "No Jellyfin Yet", Path: "/library/no-jellyfin", MetadataID: &metadata.ID}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("failed to create anime: %v", err)
	}

	ep := model.LocalEpisode{LocalAnimeID: anime.ID, Title: "Episode 1", EpisodeNum: 1, SeasonNum: 1, Path: "/library/no-jellyfin/01.mkv"}
	if err := db.DB.Create(&ep).Error; err != nil {
		t.Fatalf("failed to create episode: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/jellyfin/play/%d", ep.ID), nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected jellyfin config error, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), `"code":"jellyfin_not_configured"`)
	assert.Contains(t, w.Body.String(), "设置页填写 Jellyfin 地址和 API Key")
	assert.Contains(t, w.Body.String(), `"primary_action":"打开设置页"`)
}

func TestGetPlayInfoReturnsDiagnosticWhenSeriesMissingInJellyfin(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	jf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == testJellyfinUsersPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"Id":"user-1","Name":"admin"}]`))
		case req.Method == http.MethodGet && req.URL.Path == testJellyfinItemsPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Items":[]}`))
		default:
			http.NotFound(w, req)
		}
	}))
	defer jf.Close()

	configs := []model.GlobalConfig{
		{Key: model.ConfigKeyJellyfinUrl, Value: jf.URL},
		{Key: model.ConfigKeyJellyfinApiKey, Value: "test-key"},
	}
	for _, cfg := range configs {
		if err := db.DB.Save(&cfg).Error; err != nil {
			t.Fatalf("failed to seed jellyfin config %s: %v", cfg.Key, err)
		}
	}

	metadata := model.AnimeMetadata{Title: "Missing In Jellyfin", BangumiID: 2222}
	if err := db.DB.Create(&metadata).Error; err != nil {
		t.Fatalf("failed to create metadata: %v", err)
	}

	anime := model.LocalAnime{Title: "Missing In Jellyfin", Path: "/library/missing-jf", MetadataID: &metadata.ID}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("failed to create anime: %v", err)
	}

	ep := model.LocalEpisode{LocalAnimeID: anime.ID, Title: "Episode 1", EpisodeNum: 1, SeasonNum: 1, Path: "/library/missing-jf/01.mkv"}
	if err := db.DB.Create(&ep).Error; err != nil {
		t.Fatalf("failed to create episode: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/jellyfin/play/%d", ep.ID), nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected missing jellyfin series to return 404, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), `"code":"jellyfin_series_not_found"`)
	assert.Contains(t, w.Body.String(), "刷新资料库")
	assert.Contains(t, w.Body.String(), "/local-anime?highlight=")
}

func TestGetPlayInfoReturnsDiagnosticWhenLocalMediaMissing(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	jf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == testJellyfinUsersPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"Id":"user-1","Name":"admin"}]`))
		case req.Method == http.MethodGet && req.URL.Path == testJellyfinItemsPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Items":[{"Id":"series-1","ProviderIds":{"bangumi":"3333"}}]}`))
		case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/Users/user-1/Items"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Items":[{"Id":"episode-1","UserData":{"PlaybackPositionTicks":0,"Played":false}}]}`))
		default:
			http.NotFound(w, req)
		}
	}))
	defer jf.Close()

	configs := []model.GlobalConfig{
		{Key: model.ConfigKeyJellyfinUrl, Value: jf.URL},
		{Key: model.ConfigKeyJellyfinApiKey, Value: "test-key"},
	}
	for _, cfg := range configs {
		if err := db.DB.Save(&cfg).Error; err != nil {
			t.Fatalf("failed to seed jellyfin config %s: %v", cfg.Key, err)
		}
	}

	metadata := model.AnimeMetadata{Title: "Missing Media", BangumiID: 3333}
	if err := db.DB.Create(&metadata).Error; err != nil {
		t.Fatalf("failed to create metadata: %v", err)
	}

	anime := model.LocalAnime{Title: "Missing Media", Path: "/library/missing-media", MetadataID: &metadata.ID}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("failed to create anime: %v", err)
	}

	ep := model.LocalEpisode{LocalAnimeID: anime.ID, Title: "Episode 1", EpisodeNum: 1, SeasonNum: 1, Path: "/tmp/definitely-not-found-animate-auto-tool.mkv"}
	if err := db.DB.Create(&ep).Error; err != nil {
		t.Fatalf("failed to create episode: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/jellyfin/play/%d", ep.ID), nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected local media missing to return 404, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), `"code":"local_media_missing"`)
	assert.Contains(t, w.Body.String(), "重新扫描本地库")
}

func TestGetPlayInfoUsesConfiguredPrivatePlaybackURLs(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	jf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == testJellyfinUsersPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"Id":"user-1","Name":"admin"}]`))
		case req.Method == http.MethodGet && req.URL.Path == testJellyfinItemsPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Items":[{"Id":"series-1","ProviderIds":{"bangumi":"4444"}}]}`))
		case req.Method == http.MethodGet && req.URL.Path == "/Users/user-1/Items/episode-1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"episode-1","RunTimeTicks":14400000000,"UserData":{"PlaybackPositionTicks":900000000,"Played":false,"IsFavorite":false}}`))
		case req.Method == http.MethodGet && req.URL.Path == "/Users/user-1/Items/series-1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"series-1","UserData":{"IsFavorite":false}}`))
		case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/Users/user-1/Items"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Items":[{"Id":"episode-1","UserData":{"PlaybackPositionTicks":900000000,"Played":false}}]}`))
		default:
			http.NotFound(w, req)
		}
	}))
	defer jf.Close()

	for _, cfg := range []model.GlobalConfig{
		{Key: model.ConfigKeyJellyfinUrl, Value: jf.URL},
		{Key: model.ConfigKeyJellyfinDirectUrl, Value: "https://media.example-tailnet.ts.net/jellyfin/"},
		{Key: model.ConfigKeyNetBirdProxyURL, Value: "http://100.90.80.70:8306/"},
		{Key: model.ConfigKeyJellyfinApiKey, Value: "test-key"},
	} {
		require.NoError(t, db.DB.Save(&cfg).Error)
	}

	metadata := model.AnimeMetadata{Title: "Tailscale Show", BangumiID: 4444}
	require.NoError(t, db.DB.Create(&metadata).Error)
	anime := model.LocalAnime{Title: metadata.Title, Path: t.TempDir(), MetadataID: &metadata.ID}
	require.NoError(t, db.DB.Create(&anime).Error)
	episodePath := filepath.Join(anime.Path, "01.mkv")
	require.NoError(t, os.WriteFile(episodePath, []byte("video"), 0o600))
	ep := model.LocalEpisode{LocalAnimeID: anime.ID, Title: "Episode 1", EpisodeNum: 1, SeasonNum: 1, Path: episodePath}
	require.NoError(t, db.DB.Create(&ep).Error)

	cookie, _ := loginCookie(t, r, "admin")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/jellyfin/play/%d", ep.ID), nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var payload PlayInfoResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.Equal(t, fmt.Sprintf("/api/v1/jellyfin/stream/%d", ep.ID), payload.StreamURL)
	assert.Equal(t, "https://media.example-tailnet.ts.net/jellyfin/Videos/episode-1/stream?api_key=test-key&static=true", payload.DirectStreamURL)
	netBirdURL, err := url.Parse(payload.NetBirdStreamURL)
	require.NoError(t, err)
	assert.Equal(t, "http", netBirdURL.Scheme)
	assert.Equal(t, "100.90.80.70:8306", netBirdURL.Host)
	assert.Equal(t, fmt.Sprintf("/api/v1/netbird/jellyfin/stream/%d", ep.ID), netBirdURL.Path)
	assert.NoError(t, verifyNetBirdStreamToken(netBirdURL.Query().Get("token"), ep.ID, time.Now()))
	assert.Equal(t, int64(900000000), payload.ResumeTicks)
}

func TestUpdateJellyfinEpisodeState(t *testing.T) {
	resetAuthFixtures(t)
	requests := make([]string, 0)
	jf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests = append(requests, req.Method+" "+req.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case req.Method == http.MethodGet && req.URL.Path == testJellyfinUsersPath:
			_, _ = w.Write([]byte(`[{"Id":"user-1","Name":"admin"}]`))
		case req.Method == http.MethodGet && req.URL.Path == "/Users/user-1/Items/episode-state-1":
			_, _ = w.Write([]byte(`{"Id":"episode-state-1","UserData":{"Played":true,"IsFavorite":true}}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(jf.Close)

	for _, cfg := range []model.GlobalConfig{
		{Key: model.ConfigKeyJellyfinUrl, Value: jf.URL},
		{Key: model.ConfigKeyJellyfinApiKey, Value: "test-key"},
	} {
		require.NoError(t, db.DB.Save(&cfg).Error)
	}
	metadata := model.AnimeMetadata{Title: "Episode State", BangumiID: 910001}
	require.NoError(t, db.DB.Create(&metadata).Error)
	anime := model.LocalAnime{Title: metadata.Title, Path: t.TempDir(), MetadataID: &metadata.ID, JellyfinSeriesID: "series-state-1"}
	require.NoError(t, db.DB.Create(&anime).Error)
	episode := model.LocalEpisode{LocalAnimeID: anime.ID, Title: "Episode 1", EpisodeNum: 1, SeasonNum: 1, Path: filepath.Join(anime.Path, "01.mkv"), JellyfinItemID: "episode-state-1"}
	require.NoError(t, db.DB.Create(&episode).Error)

	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/jellyfin/episodes/%d/user-state", episode.ID), strings.NewReader(`{"played":true,"favorite":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"data":{"favorite":true,"played":true}}`, w.Body.String())
	assert.Contains(t, requests, "POST /Users/user-1/PlayedItems/episode-state-1")
	assert.Contains(t, requests, "POST /Users/user-1/FavoriteItems/episode-state-1")
}

func TestUpdateJellyfinSeriesFavorite(t *testing.T) {
	resetAuthFixtures(t)
	requests := make([]string, 0)
	jf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests = append(requests, req.Method+" "+req.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if req.Method == http.MethodGet && req.URL.Path == testJellyfinUsersPath {
			_, _ = w.Write([]byte(`[{"Id":"user-1","Name":"admin"}]`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(jf.Close)

	for _, cfg := range []model.GlobalConfig{
		{Key: model.ConfigKeyJellyfinUrl, Value: jf.URL},
		{Key: model.ConfigKeyJellyfinApiKey, Value: "test-key"},
	} {
		require.NoError(t, db.DB.Save(&cfg).Error)
	}
	metadata := model.AnimeMetadata{Title: "Series State", BangumiID: 910002}
	require.NoError(t, db.DB.Create(&metadata).Error)
	anime := model.LocalAnime{Title: metadata.Title, Path: t.TempDir(), MetadataID: &metadata.ID, JellyfinSeriesID: "series-state-2"}
	require.NoError(t, db.DB.Create(&anime).Error)

	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/jellyfin/series/%d/user-state", anime.ID), strings.NewReader(`{"favorite":false}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"data":{"favorite":false}}`, w.Body.String())
	assert.Contains(t, requests, "DELETE /Users/user-1/FavoriteItems/series-state-2")
}
