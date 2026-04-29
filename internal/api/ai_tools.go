package api

import (
	"context"
	"encoding/json"
	"runtime"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/ai"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

// GlobalAIRegistry holds the tools available for the LLM.
var GlobalAIRegistry *ai.Registry

func init() {
	GlobalAIRegistry = ai.NewRegistry()
	registerTools()
}

func registerTools() {
	// Tool 1: Get System Status
	GlobalAIRegistry.Register(
		"get_system_status",
		"获取当前系统的运行状态，包括内存使用、协程数量和运行时间",
		ai.JSONSchemaObject(map[string]any{}, []string{}),
		func(ctx context.Context, args string) (string, error) {
			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)

			status := map[string]any{
				"uptime_seconds":   int64(time.Since(runtimeStatsStartedAt).Seconds()),
				"goroutines":       runtime.NumGoroutine(),
				"heap_alloc_bytes": mem.HeapAlloc,
			}
			b, _ := json.Marshal(status)
			return string(b), nil
		},
	)

	// Tool 2: Scan Local Library
	GlobalAIRegistry.Register(
		"scan_local_library",
		"触发本地番剧目录的全量扫描和刮削。在用户要求整理库、更新本地数据时调用。",
		ai.JSONSchemaObject(map[string]any{}, []string{}),
		func(ctx context.Context, args string) (string, error) {
			go func() {
				scanner := service.NewScannerService()
				_ = scanner.ScanAll()

				// Phase 2: Agent
				agent := service.NewAgentService()
				agent.RunAgentForLibrary()

				triggerJellyfinLibraryRefresh(context.Background())
			}()
			return `{"status": "success", "message": "已在后台启动全量扫描和刮削。"}`, nil
		},
	)
}
