package api

import (
	"errors"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const slowHTTPRequestThreshold = 5 * time.Second

// RequestLoggingMiddleware records enough context to diagnose failed or slow
// requests without persisting query strings, request bodies, cookies, or
// authorization headers.
func RequestLoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		requestID := safeRequestID(c.GetHeader("X-Request-ID"))
		if requestID == "" {
			requestID = uuid.NewString()
		}
		c.Request.Header.Set("X-Request-ID", requestID)
		c.Header("X-Request-ID", requestID)

		c.Next()

		duration := time.Since(startedAt)
		status := c.Writer.Status()
		route := strings.TrimSpace(c.FullPath())
		if route == "" && c.Request != nil && c.Request.URL != nil {
			route = c.Request.URL.EscapedPath()
		}
		if route == "" {
			route = "/"
		}
		message := "HTTPRequest: completed request_id=%s method=%s route=%s status=%d bytes=%d duration=%s client_ip=%s errors=%d"
		args := []any{
			requestID,
			c.Request.Method,
			route,
			status,
			c.Writer.Size(),
			duration.Round(time.Millisecond),
			c.ClientIP(),
			len(c.Errors),
		}
		switch {
		case status >= http.StatusInternalServerError:
			log.Printf("ERROR: "+message, args...)
		case duration >= slowHTTPRequestThreshold:
			log.Printf("WARN: HTTPRequest: slow "+message[len("HTTPRequest: "):], args...)
		default:
			log.Printf(message, args...)
		}
	}
}

// RecoveryLoggingMiddleware keeps handler panics from terminating the server
// and emits the request identity plus a stack trace for immediate diagnosis.
// http.ErrAbortHandler remains delegated to net/http so intentional streaming
// aborts do not become application 500 responses.
func RecoveryLoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(recovered)
			}
			requestID := safeRequestID(c.GetHeader("X-Request-ID"))
			route := strings.TrimSpace(c.FullPath())
			if route == "" && c.Request != nil && c.Request.URL != nil {
				route = c.Request.URL.EscapedPath()
			}
			log.Printf(
				"ERROR: HTTPRequest: handler panic request_id=%s method=%s route=%s recovery_action=return_500 panic=%v\n%s",
				requestID,
				c.Request.Method,
				route,
				recovered,
				debug.Stack(),
			)
			c.AbortWithStatus(http.StatusInternalServerError)
		}()
		c.Next()
	}
}

func safeRequestID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 64 {
		return ""
	}
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9':
		case char == '-', char == '_', char == '.', char == ':':
		default:
			return ""
		}
	}
	return value
}
