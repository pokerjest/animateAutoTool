package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/pokerjest/animateAutoTool/internal/taskstate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestV1RefreshSubscriptionsStartsRepairTask(t *testing.T) {
	resetAuthFixtures(t)
	taskstate.Global.Reset()
	t.Cleanup(taskstate.Global.Reset)

	previous := runSubscriptionRefreshNow
	runSubscriptionRefreshNow = func(_ context.Context, report service.SubscriptionRefreshProgressFunc) (service.SubscriptionRefreshResult, error) {
		report(service.SubscriptionRefreshProgress{Message: "正在修复下载记录", Current: 1, Total: 2})
		return service.SubscriptionRefreshResult{Checked: 2, SyncedLogs: 1, LibraryRepairs: 1}, nil
	}
	t.Cleanup(func() { runSubscriptionRefreshNow = previous })

	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions/refresh", nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"task_id":"subscription-refresh"`)

	require.Eventually(t, func() bool {
		task, ok := taskstate.Global.Get(subscriptionRefreshTaskID)
		return ok && task.Status == taskstate.StatusCompleted
	}, time.Second, 10*time.Millisecond)
	task, _ := taskstate.Global.Get(subscriptionRefreshTaskID)
	assert.Contains(t, task.Message, "检查 2 条订阅")
	assert.Contains(t, task.Message, "修复 2 条下载记录")
}
