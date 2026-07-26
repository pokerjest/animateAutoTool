package api

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportDiagnosticLogsRequiresAuthentication(t *testing.T) {
	r := setupRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/logs/export", nil)
	markLocalRequest(req)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestExportDiagnosticLogsDownloadsLatestThreeFiles(t *testing.T) {
	resetAuthFixtures(t)
	logDir := config.LogsDir()
	require.NoError(t, os.MkdirAll(logDir, 0o755))
	removeTestHourlyLogs(t, logDir)
	t.Cleanup(func() { removeTestHourlyLogs(t, logDir) })

	logs := map[string]string{
		"server-20260726-09.log": "oldest\n",
		"server-20260726-10.log": "first\n",
		"server-20260726-11.log": "second\n",
		"server-20260726-12.log": "third\n",
	}
	for name, content := range logs {
		require.NoError(t, os.WriteFile(filepath.Join(logDir, name), []byte(content), 0o600))
	}

	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/logs/export", nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	assert.Equal(t, "3", w.Header().Get("X-Log-File-Count"))
	assert.Contains(t, w.Header().Get("Content-Type"), "application/zip")
	assert.Contains(t, w.Header().Get("Content-Disposition"), "animate-auto-tool-logs-")

	reader, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	require.NoError(t, err)
	require.Len(t, reader.File, 3)
	wantNames := []string{"server-20260726-12.log", "server-20260726-11.log", "server-20260726-10.log"}
	for index, file := range reader.File {
		assert.Equal(t, wantNames[index], file.Name)
		assert.NotContains(t, file.Name, logDir)
	}
}

func TestExportDiagnosticLogsReportsWhenNoLogsExist(t *testing.T) {
	resetAuthFixtures(t)
	logDir := config.LogsDir()
	require.NoError(t, os.MkdirAll(logDir, 0o755))
	removeTestHourlyLogs(t, logDir)

	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/logs/export", nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "diagnostic_logs_not_found")
}

func removeTestHourlyLogs(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("read log directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "server-") && strings.HasSuffix(entry.Name(), ".log") {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !os.IsNotExist(err) {
				t.Fatalf("remove test log %s: %v", entry.Name(), err)
			}
		}
	}
}
