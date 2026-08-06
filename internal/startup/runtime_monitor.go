package startup

import (
	"context"
	"log"
	"runtime"
	"time"
)

const runtimeMonitorInterval = 30 * time.Minute

func runRuntimeMonitor(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	logRuntimeSnapshot("startup")

	ticker := time.NewTicker(runtimeMonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			logRuntimeSnapshot("periodic")
		case <-ctx.Done():
			return
		}
	}
}

func logRuntimeSnapshot(stage string) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	log.Printf(
		"RuntimeMonitor[%s]: goroutines=%d heap_alloc=%dMB heap_inuse=%dMB stack_inuse=%dMB num_gc=%d",
		stage,
		runtime.NumGoroutine(),
		bytesToMB(mem.HeapAlloc),
		bytesToMB(mem.HeapInuse),
		bytesToMB(mem.StackInuse),
		mem.NumGC,
	)
}

func bytesToMB(v uint64) uint64 {
	return v / 1024 / 1024
}
