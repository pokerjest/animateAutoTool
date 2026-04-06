package api

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
)

var runtimeStatsStartedAt = time.Now()

func RuntimeStatsHandler(c *gin.Context) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	lastGCUnix := int64(0)
	if mem.LastGC > 0 {
		lastGCUnix = int64(mem.LastGC / uint64(time.Second))
	}

	c.JSON(http.StatusOK, gin.H{
		"timestamp_unix":  time.Now().Unix(),
		"uptime_seconds":  int64(time.Since(runtimeStatsStartedAt).Seconds()),
		"started_at_unix": runtimeStatsStartedAt.Unix(),
		"go": gin.H{
			"goroutines": runtime.NumGoroutine(),
			"gomaxprocs": runtime.GOMAXPROCS(0),
			"num_cpu":    runtime.NumCPU(),
		},
		"memory": gin.H{
			"heap_alloc_bytes":  mem.HeapAlloc,
			"heap_inuse_bytes":  mem.HeapInuse,
			"stack_inuse_bytes": mem.StackInuse,
			"sys_bytes":         mem.Sys,
		},
		"gc": gin.H{
			"num_gc":       mem.NumGC,
			"last_gc_unix": lastGCUnix,
		},
	})
}
