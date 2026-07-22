package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// v1CaptureWriter lets the v1 layer reuse established JSON-only handlers
// while still enforcing the public v1 response envelope. It must not be used
// for streams, downloads, redirects, or HTML handlers.
type v1CaptureWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newV1CaptureWriter() *v1CaptureWriter {
	return &v1CaptureWriter{header: make(http.Header), status: http.StatusOK}
}

func (w *v1CaptureWriter) Header() http.Header { return w.header }
func (w *v1CaptureWriter) WriteHeader(code int) {
	if !w.Written() {
		w.status = code
	}
}
func (w *v1CaptureWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(data)
}
func (w *v1CaptureWriter) WriteString(value string) (int, error) { return w.Write([]byte(value)) }
func (w *v1CaptureWriter) Status() int                           { return w.status }
func (w *v1CaptureWriter) Size() int                             { return w.body.Len() }
func (w *v1CaptureWriter) Written() bool                         { return w.body.Len() > 0 || w.status != http.StatusOK }
func (w *v1CaptureWriter) WriteHeaderNow()                       {}
func (w *v1CaptureWriter) Flush()                                {}
func (w *v1CaptureWriter) CloseNotify() <-chan bool {
	ch := make(chan bool)
	return ch
}
func (w *v1CaptureWriter) Pusher() http.Pusher { return nil }
func (w *v1CaptureWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, fmt.Errorf("v1 capture writer does not support hijacking")
}

// v1RunJSONHandler captures a Gin handler that returns JSON and rewrites its
// output into the canonical {data,message} / {error:{code,message}} shape.
func v1RunJSONHandler(c *gin.Context, handler gin.HandlerFunc) {
	original := c.Writer
	capture := newV1CaptureWriter()
	c.Writer = capture
	handler(c)
	c.Writer = original

	for key, values := range capture.Header() {
		if strings.EqualFold(key, "Content-Type") || strings.EqualFold(key, "Content-Length") {
			continue
		}
		for _, value := range values {
			original.Header().Add(key, value)
		}
	}

	status := capture.Status()
	payload := decodeV1LegacyPayload(capture.body.Bytes())
	if status >= http.StatusBadRequest {
		v1Error(c, status, "request_failed", v1PayloadMessage(payload))
		return
	}

	message := ""
	if object, ok := payload.(map[string]any); ok {
		if value, ok := object["message"].(string); ok {
			message = value
		}
	}
	v1Message(c, status, message, payload)
}

func decodeV1LegacyPayload(body []byte) any {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	var payload any
	if json.Unmarshal(body, &payload) == nil {
		return payload
	}
	return string(body)
}

func v1PayloadMessage(payload any) string {
	switch value := payload.(type) {
	case string:
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	case map[string]any:
		for _, key := range []string{"message", "error"} {
			if text, ok := value[key].(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	return "请求未能完成"
}
