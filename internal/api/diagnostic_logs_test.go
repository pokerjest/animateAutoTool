package api

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/service"
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

func TestExportHealthDiagnosticsRequiresAuthentication(t *testing.T) {
	r := setupRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/health/export", nil)
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

func TestExportHealthDiagnosticsIncludesIssueLogsAndSnapshots(t *testing.T) {
	resetAuthFixtures(t)
	logDir := config.LogsDir()
	require.NoError(t, os.MkdirAll(logDir, 0o755))
	removeTestHealthLogs(t, logDir)
	t.Cleanup(func() { removeTestHealthLogs(t, logDir) })
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "health-20260727-12.log"), []byte("database is locked\n"), 0o600))
	require.NoError(t, service.ReportLibraryIssue(service.LibraryIssueInput{
		IssueKey: "scrape:999", IssueType: service.LibraryIssueTypeScrape, Title: "测试番剧",
		DirectoryPath: "/media/测试番剧", Message: "metadata failed password=super-secret", Hint: "交给开发者检查",
	}))
	t.Cleanup(func() { _ = db.DB.Unscoped().Where("issue_key = ?", "scrape:999").Delete(&model.LibraryIssue{}).Error })

	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/health/export", nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "1", w.Header().Get("X-Health-Event-File-Count"))
	assert.Equal(t, "true", w.Header().Get("X-Health-Logs-Consumed"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), "animate-auto-tool-health-")
	reader, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	require.NoError(t, err)
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	for _, name := range []string{"README.txt", "current-problems.txt", "manifest.json", "health-report.json", "runtime.json", "database.json", "open-library-issues.json", "failed-tasks.json", "failed-subscription-runs.json", "failed-downloads.json", "health-20260727-12.log"} {
		assert.True(t, slices.Contains(names, name), "missing %s in %#v", name, names)
	}
	assert.Contains(t, zipFileContent(t, reader, "current-problems.txt"), "测试番剧")
	issueSnapshot := zipFileContent(t, reader, "open-library-issues.json")
	assert.Contains(t, issueSnapshot, "测试番剧")
	assert.NotContains(t, issueSnapshot, "super-secret")
	assert.Contains(t, issueSnapshot, "[REDACTED]")
	_, statErr := os.Stat(filepath.Join(logDir, "health-20260727-12.log"))
	assert.True(t, os.IsNotExist(statErr), "exported health log should be consumed: %v", statErr)

	// A second export must not repeat consumed event files, while unresolved
	// database-backed problems remain in the fresh current-state snapshot.
	second := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/health/export", nil)
	secondRequest.Header.Set("Cookie", cookie)
	markLocalRequest(secondRequest)
	r.ServeHTTP(second, secondRequest)
	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	assert.Equal(t, "0", second.Header().Get("X-Health-Event-File-Count"))
	secondReader, err := zip.NewReader(bytes.NewReader(second.Body.Bytes()), int64(second.Body.Len()))
	require.NoError(t, err)
	assert.Contains(t, zipFileContent(t, secondReader, "current-problems.txt"), "测试番剧")
}

func TestExportHealthDiagnosticsSucceedsWithoutIssueLogFiles(t *testing.T) {
	resetAuthFixtures(t)
	logDir := config.LogsDir()
	require.NoError(t, os.MkdirAll(logDir, 0o755))
	removeTestHealthLogs(t, logDir)

	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/health/export", nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "0", w.Header().Get("X-Health-Event-File-Count"))
	assert.NotEqual(t, "0", w.Header().Get("X-Diagnostic-Snapshot-Count"))
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

func removeTestHealthLogs(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("read log directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "health-") && strings.HasSuffix(entry.Name(), ".log") {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !os.IsNotExist(err) {
				t.Fatalf("remove test health log %s: %v", entry.Name(), err)
			}
		}
	}
}

func zipFileContent(t *testing.T, reader *zip.Reader, name string) string {
	t.Helper()
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		source, err := file.Open()
		require.NoError(t, err)
		defer func() { _ = source.Close() }()
		var content bytes.Buffer
		_, err = content.ReadFrom(source)
		require.NoError(t, err)
		return content.String()
	}
	t.Fatalf("missing ZIP entry %s", name)
	return ""
}
