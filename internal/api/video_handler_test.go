package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPlayMagnetHandlerReturnsNotImplementedWhenOfflineDownloadUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body, err := json.Marshal(PlayRequest{Magnet: "magnet:?xt=urn:btih:test"})
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/play/magnet", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req

	PlayMagnetHandler(ctx)

	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("expected status %d, got %d", http.StatusNotImplemented, recorder.Code)
	}

	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !strings.Contains(payload["error"], "秒播") {
		t.Fatalf("expected not-implemented message, got %q", payload["error"])
	}
}
