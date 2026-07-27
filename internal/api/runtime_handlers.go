package api

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
)

var runtimeStatsStartedAt = time.Now()

type runtimeGoSnapshot struct {
	Goroutines int `json:"goroutines"`
	GOMAXPROCS int `json:"gomaxprocs"`
	NumCPU     int `json:"num_cpu"`
}

type runtimeMemorySnapshot struct {
	HeapAllocBytes  uint64 `json:"heap_alloc_bytes"`
	HeapInuseBytes  uint64 `json:"heap_inuse_bytes"`
	StackInuseBytes uint64 `json:"stack_inuse_bytes"`
	SysBytes        uint64 `json:"sys_bytes"`
}

type runtimeGCSnapshot struct {
	NumGC      uint32 `json:"num_gc"`
	LastGCUnix int64  `json:"last_gc_unix"`
}

type runtimeSnapshot struct {
	GeneratedAt   time.Time             `json:"generated_at"`
	TimestampUnix int64                 `json:"timestamp_unix"`
	UptimeSeconds int64                 `json:"uptime_seconds"`
	StartedAtUnix int64                 `json:"started_at_unix"`
	Go            runtimeGoSnapshot     `json:"go"`
	Memory        runtimeMemorySnapshot `json:"memory"`
	GC            runtimeGCSnapshot     `json:"gc"`
}

func buildRuntimeSnapshot() runtimeSnapshot {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	lastGCUnix := int64(0)
	if mem.LastGC > 0 {
		lastGCUnix = int64(mem.LastGC / uint64(time.Second))
	}
	return runtimeSnapshot{
		GeneratedAt:   time.Now(),
		TimestampUnix: time.Now().Unix(),
		UptimeSeconds: int64(time.Since(runtimeStatsStartedAt).Seconds()),
		StartedAtUnix: runtimeStatsStartedAt.Unix(),
		Go: runtimeGoSnapshot{
			Goroutines: runtime.NumGoroutine(),
			GOMAXPROCS: runtime.GOMAXPROCS(0),
			NumCPU:     runtime.NumCPU(),
		},
		Memory: runtimeMemorySnapshot{
			HeapAllocBytes:  mem.HeapAlloc,
			HeapInuseBytes:  mem.HeapInuse,
			StackInuseBytes: mem.StackInuse,
			SysBytes:        mem.Sys,
		},
		GC: runtimeGCSnapshot{NumGC: mem.NumGC, LastGCUnix: lastGCUnix},
	}
}

func HealthPageHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "health.html", gin.H{
		"SkipLayout": IsHTMX(c),
		"Report":     buildHealthReport(),
	})
}

func HealthReportHandler(c *gin.Context) {
	c.JSON(http.StatusOK, buildHealthReport())
}

func RuntimeStatsHandler(c *gin.Context) {
	c.JSON(http.StatusOK, buildRuntimeSnapshot())
}
