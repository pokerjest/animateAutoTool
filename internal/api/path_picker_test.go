package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPickDirectoryHandlerRequiresDirectLoopback(t *testing.T) {
	t.Setenv("GIN_MODE", "test")
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	req := httptest.NewRequest(http.MethodPost, "/api/system/pick-directory", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.20:2345"
	req.Host = "example.com"
	ctx.Request = req

	PickDirectoryHandler(ctx)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, recorder.Code)
	}
}

func TestPickDirectoryHandlerReturnsPickedPath(t *testing.T) {
	t.Setenv("GIN_MODE", "test")
	gin.SetMode(gin.TestMode)

	previous := pickDirectoryFunc
	pickDirectoryFunc = func(title, defaultPath string) (string, error) {
		if title != "下载目录" {
			t.Fatalf("unexpected title: %q", title)
		}
		if defaultPath != `E:\anime` {
			t.Fatalf("unexpected default path: %q", defaultPath)
		}
		return `E:\bangumi`, nil
	}
	t.Cleanup(func() {
		pickDirectoryFunc = previous
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	req := httptest.NewRequest(http.MethodPost, "/api/system/pick-directory", strings.NewReader(`{"title":"下载目录","default_path":"E:\\anime"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:2345"
	req.Host = testLocalHost
	ctx.Request = req

	PickDirectoryHandler(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if payload["path"] != `E:\bangumi` {
		t.Fatalf("unexpected path: %q", payload["path"])
	}
}

func TestPickDirectoryHandlerReturnsCancelled(t *testing.T) {
	t.Setenv("GIN_MODE", "test")
	gin.SetMode(gin.TestMode)

	previous := pickDirectoryFunc
	pickDirectoryFunc = func(title, defaultPath string) (string, error) {
		return "", errPickerCancelled
	}
	t.Cleanup(func() {
		pickDirectoryFunc = previous
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	req := httptest.NewRequest(http.MethodPost, "/api/system/pick-directory", strings.NewReader(`{"title":"下载目录"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:2345"
	req.Host = testLocalHost
	ctx.Request = req

	PickDirectoryHandler(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var payload map[string]bool
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !payload["cancelled"] {
		t.Fatalf("expected cancelled=true, got %#v", payload)
	}
}
