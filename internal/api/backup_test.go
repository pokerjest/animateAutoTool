package api

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnalyzeBackupRejectsNonSQLiteUpload(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("backup_file", "README.md")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := io.Copy(part, strings.NewReader("# not a database")); err != nil {
		t.Fatalf("failed to write upload payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/backup/analyze", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "无效的数据库备份文件") {
		t.Fatalf("expected invalid database message, got %q", w.Body.String())
	}
}
