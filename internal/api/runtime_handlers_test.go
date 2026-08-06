package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/runtimejournal"
)

func TestReadinessRejectsBlockedRecovery(t *testing.T) {
	runtimejournal.SetRecoveryBlocked(true)
	t.Cleanup(func() { runtimejournal.SetRecoveryBlocked(false) })

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/healthz/ready", ReadinessHandler)
	request := httptest.NewRequest(http.MethodGet, "/healthz/ready", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected readiness 503 while recovery is blocked, got %d", response.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode readiness payload: %v", err)
	}
	if payload["reason"] != "recovery_blocked" {
		t.Fatalf("unexpected readiness reason: %#v", payload["reason"])
	}
}
