package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

func TestMetadataRefreshHandlersRollBackStateWhenBackgroundWorkIsRejected(t *testing.T) {
	resetBackgroundTasksForTest(t)
	StartBackgroundTasks(context.Background())
	StopBackgroundTasks()

	handlers := []struct {
		name    string
		handler gin.HandlerFunc
	}{
		{name: "legacy", handler: RefreshLibraryMetadataHandler},
		{name: "v1", handler: V1RefreshLibraryHandler},
	}
	for _, test := range handlers {
		t.Run(test.name, func(t *testing.T) {
			service.GlobalRefreshStatus.Finish("")
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/metadata/refresh", nil)
			test.handler(ctx)
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
			}
			if service.GlobalRefreshStatus.Snapshot().IsRunning {
				t.Fatal("metadata refresh status remained running after launch rejection")
			}
		})
	}
}
