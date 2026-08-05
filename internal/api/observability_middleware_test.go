package api

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestLoggingMiddlewareOmitsQueryAndSensitiveHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&output)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	})

	router := gin.New()
	router.Use(RequestLoggingMiddleware(), RecoveryLoggingMiddleware())
	router.GET("/probe/:id", func(c *gin.Context) {
		c.Status(http.StatusInternalServerError)
	})

	request := httptest.NewRequest(http.MethodGet, "/probe/42?token=top-secret&password=hidden", nil)
	request.Header.Set("Authorization", "Bearer should-not-appear")
	request.Header.Set("Cookie", "session=should-not-appear")
	request.Header.Set("X-Request-ID", "stable-test-request")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", response.Code)
	}
	logged := output.String()
	for _, secret := range []string{"top-secret", "hidden", "should-not-appear", "token=", "password="} {
		if strings.Contains(logged, secret) {
			t.Fatalf("request log leaked sensitive value %q: %s", secret, logged)
		}
	}
	for _, expected := range []string{
		"ERROR: HTTPRequest: completed",
		"request_id=stable-test-request",
		"method=GET",
		"route=/probe/:id",
		"status=500",
	} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("request log missing %q: %s", expected, logged)
		}
	}
}

func TestRecoveryLoggingMiddlewareRecordsRequestAndStack(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&output)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	})

	router := gin.New()
	router.Use(RequestLoggingMiddleware(), RecoveryLoggingMiddleware())
	router.GET("/panic", func(*gin.Context) {
		panic("middleware-boom")
	})

	request := httptest.NewRequest(http.MethodGet, "/panic?api_key=must-not-leak", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected recovered status 500, got %d", response.Code)
	}
	logged := output.String()
	for _, expected := range []string{
		"handler panic",
		"middleware-boom",
		"route=/panic",
		"observability_middleware_test.go",
	} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("panic log missing %q: %s", expected, logged)
		}
	}
	if strings.Contains(logged, "must-not-leak") {
		t.Fatalf("panic log leaked query secret: %s", logged)
	}
	if response.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected generated request ID response header")
	}
}

func TestSafeRequestIDRejectsLogInjection(t *testing.T) {
	if got := safeRequestID("valid-id_01:part.two"); got == "" {
		t.Fatal("expected safe request ID to be accepted")
	}
	if got := safeRequestID("bad\nforged-log"); got != "" {
		t.Fatalf("expected newline request ID to be rejected, got %q", got)
	}
}
