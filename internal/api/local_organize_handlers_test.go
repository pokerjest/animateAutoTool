package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/pokerjest/animateAutoTool/internal/taskstate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalOrganizeEndpointsRequireAuthentication(t *testing.T) {
	router := setupRouter()
	for _, path := range []string{"/api/v1/local-anime/organize/preview", "/api/v1/local-anime/organize"} {
		request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{}`))
		request.Header.Set("Content-Type", "application/json")
		markLocalRequest(request)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		assert.Equal(t, http.StatusUnauthorized, response.Code)
	}
}

func TestLocalOrganizePreviewAndTaskFlow(t *testing.T) {
	resetAuthFixtures(t)
	service.GlobalLocalOrganizePlans.Reset()
	taskstate.Global.Reset()
	localOrganizeRunMu.Lock()
	localOrganizeRunning = false
	localOrganizeRunMu.Unlock()
	originalFactory := newLocalOrganizer
	newLocalOrganizer = func() (*service.LocalOrganizer, error) { return service.NewLocalOrganizer(db.DB, nil), nil }
	t.Cleanup(func() {
		newLocalOrganizer = originalFactory
		service.GlobalLocalOrganizePlans.Reset()
		localOrganizeRunMu.Lock()
		localOrganizeRunning = false
		localOrganizeRunMu.Unlock()
	})

	root := t.TempDir()
	sourceDir := filepath.Join(root, "Existing Show")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	video := filepath.Join(sourceDir, "Existing Show - 01.mkv")
	require.NoError(t, os.WriteFile(video, []byte("video"), 0o600))
	directory := model.LocalAnimeDirectory{Path: root}
	require.NoError(t, db.DB.Create(&directory).Error)
	anime := model.LocalAnime{DirectoryID: directory.ID, Title: "Existing Show", Path: sourceDir, Season: 1}
	require.NoError(t, db.DB.Create(&anime).Error)
	require.NoError(t, db.DB.Create(&model.LocalEpisode{LocalAnimeID: anime.ID, EpisodeNum: 1, SeasonNum: 1, Path: video}).Error)
	t.Cleanup(func() {
		var ids []uint
		_ = db.DB.Unscoped().Model(&model.LocalAnime{}).Where("directory_id = ?", directory.ID).Pluck("id", &ids).Error
		if len(ids) > 0 {
			_ = db.DB.Unscoped().Where("local_anime_id IN ?", ids).Delete(&model.LocalEpisode{}).Error
			_ = db.DB.Unscoped().Where("id IN ?", ids).Delete(&model.LocalAnime{}).Error
		}
		_ = db.DB.Unscoped().Delete(&model.LocalAnimeDirectory{}, directory.ID).Error
	})

	router := setupRouter()
	cookie, _ := loginCookie(t, router, "admin")
	previewBody := []byte(`{"selection":{"mode":"ids","anime_ids":[` + jsonNumber(anime.ID) + `]}}`)
	previewRequest := httptest.NewRequest(http.MethodPost, "/api/v1/local-anime/organize/preview", bytes.NewReader(previewBody))
	previewRequest.Header.Set("Content-Type", "application/json")
	previewRequest.Header.Set("Cookie", cookie)
	markLocalRequest(previewRequest)
	previewResponse := httptest.NewRecorder()
	router.ServeHTTP(previewResponse, previewRequest)
	require.Equal(t, http.StatusOK, previewResponse.Code, previewResponse.Body.String())
	var previewEnvelope struct {
		Data service.LocalOrganizePreview `json:"data"`
	}
	require.NoError(t, json.Unmarshal(previewResponse.Body.Bytes(), &previewEnvelope))
	assert.NotEmpty(t, previewEnvelope.Data.PlanID)
	assert.Equal(t, 1, previewEnvelope.Data.ChangeCount)

	applyBody, err := json.Marshal(map[string]any{"plan_id": previewEnvelope.Data.PlanID, "include_anime_ids": []uint{anime.ID}})
	require.NoError(t, err)
	applyRequest := httptest.NewRequest(http.MethodPost, "/api/v1/local-anime/organize", bytes.NewReader(applyBody))
	applyRequest.Header.Set("Content-Type", "application/json")
	applyRequest.Header.Set("Cookie", cookie)
	markLocalRequest(applyRequest)
	applyResponse := httptest.NewRecorder()
	router.ServeHTTP(applyResponse, applyRequest)
	require.Equal(t, http.StatusAccepted, applyResponse.Code, applyResponse.Body.String())
	var taskEnvelope struct {
		Data struct {
			TaskID string `json:"task_id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(applyResponse.Body.Bytes(), &taskEnvelope))
	require.NotEmpty(t, taskEnvelope.Data.TaskID)

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		task, ok := taskstate.Global.Get(taskEnvelope.Data.TaskID)
		if ok && (task.Status == taskstate.StatusCompleted || task.Status == taskstate.StatusError) {
			require.Equal(t, taskstate.StatusCompleted, task.Status, task.Message)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	task, ok := taskstate.Global.Get(taskEnvelope.Data.TaskID)
	require.True(t, ok)
	assert.Equal(t, taskstate.StatusCompleted, task.Status, task.Message)
	_, statErr := os.Stat(filepath.Join(root, "Existing Show", "Season 01", "Existing Show - S01E01.mkv"))
	assert.NoError(t, statErr)

	replay := httptest.NewRecorder()
	replayRequest := httptest.NewRequest(http.MethodPost, "/api/v1/local-anime/organize", bytes.NewReader(applyBody))
	replayRequest.Header.Set("Content-Type", "application/json")
	replayRequest.Header.Set("Cookie", cookie)
	markLocalRequest(replayRequest)
	router.ServeHTTP(replay, replayRequest)
	assert.Equal(t, http.StatusGone, replay.Code)
}

func jsonNumber(value uint) string {
	data, _ := json.Marshal(value)
	return string(data)
}
