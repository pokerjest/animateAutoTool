package scheduler

import (
	"fmt"
	"sync"
	"time"
)

type RunStatus struct {
	IsRunning          bool
	LastRunSource      string
	LastStartedAt      *time.Time
	LastFinishedAt     *time.Time
	TotalSubscriptions int
	SuccessCount       int
	WarningCount       int
	ErrorCount         int
	SkippedCount       int
	LastSummary        string
	LastError          string
}

type statusTracker struct {
	mu     sync.RWMutex
	status RunStatus
}

func (t *statusTracker) Begin(source string, total int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	t.status.IsRunning = true
	t.status.LastRunSource = source
	t.status.LastStartedAt = &now
	t.status.TotalSubscriptions = total
	t.status.SuccessCount = 0
	t.status.WarningCount = 0
	t.status.ErrorCount = 0
	t.status.SkippedCount = 0
	t.status.LastSummary = ""
	t.status.LastError = ""
}

func (t *statusTracker) Skip(source, summary string) RunStatus {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	t.status.IsRunning = false
	t.status.LastRunSource = source
	t.status.LastStartedAt = &now
	t.status.LastFinishedAt = &now
	t.status.TotalSubscriptions = 0
	t.status.SuccessCount = 0
	t.status.WarningCount = 0
	t.status.ErrorCount = 0
	t.status.SkippedCount = 0
	t.status.LastSummary = summary
	t.status.LastError = ""

	return t.status
}

func (t *statusTracker) Finish(success, warning, failure, total int, source, lastErr string) RunStatus {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	t.status.IsRunning = false
	t.status.LastRunSource = source
	if t.status.LastStartedAt == nil {
		t.status.LastStartedAt = &now
	}
	t.status.LastFinishedAt = &now
	t.status.TotalSubscriptions = total
	t.status.SuccessCount = success
	t.status.WarningCount = warning
	t.status.ErrorCount = failure
	t.status.SkippedCount = max(total-success-warning-failure, 0)
	t.status.LastError = lastErr
	t.status.LastSummary = fmt.Sprintf("最近一轮共检查 %d 个订阅：成功 %d，警告 %d，失败 %d", total, success, warning, failure)

	return t.status
}

func (t *statusTracker) Snapshot() RunStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.status
}

var GlobalRunStatus = &statusTracker{}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
