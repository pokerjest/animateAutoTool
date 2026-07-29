package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestV1UpdaterReleasesRejectsUnknownChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/settings/updater/releases", V1UpdaterReleasesHandler)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/settings/updater/releases?channel=nightly", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "invalid_update_channel")
}

func TestV1UpdaterActionRejectsUnknownFieldsAndInvalidVersions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/settings/updater/:action", V1UpdaterActionHandler)

	cases := []string{
		`{"version":"v0.9.8","download_url":"https://example.test/update"}`,
		`{"version":"latest"}`,
	}
	for _, body := range cases {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/settings/updater/apply", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusBadRequest, recorder.Code, body)
	}
}
