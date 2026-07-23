package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/taskstate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSEStreamsTypedTaskUpdate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/events", SSEHandler)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/events", nil)
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Contains(t, response.Header.Get("Content-Type"), "text/event-stream")

	reader := bufio.NewReader(response.Body)
	for {
		line, readErr := reader.ReadString('\n')
		require.NoError(t, readErr)
		if line == "\n" || line == "\r\n" {
			break
		}
	}

	taskstate.Global.Start("sse-task", "scan", "本地扫描", "正在扫描")
	t.Cleanup(taskstate.Global.Reset)
	var stream strings.Builder
	for !strings.Contains(stream.String(), `"task_id":"sse-task"`) {
		line, readErr := reader.ReadString('\n')
		require.NoError(t, readErr)
		stream.WriteString(line)
	}

	assert.Contains(t, stream.String(), "event:task_update")
	assert.Contains(t, stream.String(), `"status":"running"`)
}
