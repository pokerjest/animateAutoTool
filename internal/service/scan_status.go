package service

import (
	"fmt"
	"sync"
	"time"
)

type ScanRunStatus struct {
	IsRunning            bool
	TotalDirectories     int
	ProcessedDirectories int
	AddedCount           int
	UpdatedCount         int
	FailedDirectories    int
	CurrentDirectory     string
	LastStartedAt        *time.Time
	LastFinishedAt       *time.Time
	LastDuration         string
	LastSummary          string
	LastError            string
}

type scanStatusTracker struct {
	mu     sync.RWMutex
	status ScanRunStatus
}

func (t *scanStatusTracker) Begin(total int) ScanRunStatus {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	t.status = ScanRunStatus{
		IsRunning:            true,
		TotalDirectories:     total,
		ProcessedDirectories: 0,
		AddedCount:           0,
		UpdatedCount:         0,
		FailedDirectories:    0,
		CurrentDirectory:     "",
		LastStartedAt:        &now,
		LastFinishedAt:       nil,
		LastDuration:         "",
		LastSummary:          fmt.Sprintf("正在扫描 %d 个目录", total),
		LastError:            "",
	}
	return t.status
}

func (t *scanStatusTracker) Skip(summary string) ScanRunStatus {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	t.status = ScanRunStatus{
		IsRunning:            false,
		TotalDirectories:     0,
		ProcessedDirectories: 0,
		AddedCount:           0,
		UpdatedCount:         0,
		FailedDirectories:    0,
		CurrentDirectory:     "",
		LastStartedAt:        &now,
		LastFinishedAt:       &now,
		LastDuration:         "0 秒",
		LastSummary:          summary,
		LastError:            "",
	}
	return t.status
}

func (t *scanStatusTracker) Advance(dir string, added, updated int, err error) ScanRunStatus {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.status.LastStartedAt == nil {
		now := time.Now()
		t.status.LastStartedAt = &now
	}

	t.status.IsRunning = true
	t.status.ProcessedDirectories++
	t.status.CurrentDirectory = dir
	t.status.AddedCount += added
	t.status.UpdatedCount += updated
	if err != nil {
		t.status.FailedDirectories++
		t.status.LastError = err.Error()
	}
	t.status.LastSummary = fmt.Sprintf(
		"正在扫描第 %d/%d 个目录，累计新增 %d，更新 %d，失败 %d",
		t.status.ProcessedDirectories,
		maxInt(t.status.TotalDirectories, 1),
		t.status.AddedCount,
		t.status.UpdatedCount,
		t.status.FailedDirectories,
	)

	return t.status
}

func (t *scanStatusTracker) Finish() ScanRunStatus {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	t.status.IsRunning = false
	if t.status.LastStartedAt == nil {
		t.status.LastStartedAt = &now
	}
	t.status.LastFinishedAt = &now
	t.status.LastDuration = humanizeScanDuration(now.Sub(*t.status.LastStartedAt))
	t.status.LastSummary = fmt.Sprintf(
		"最近一轮扫描了 %d 个目录：新增 %d，更新 %d，失败 %d",
		t.status.TotalDirectories,
		t.status.AddedCount,
		t.status.UpdatedCount,
		t.status.FailedDirectories,
	)
	t.status.CurrentDirectory = ""

	return t.status
}

func (t *scanStatusTracker) Snapshot() ScanRunStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.status
}

func humanizeScanDuration(d time.Duration) string {
	if d < time.Second {
		return "小于 1 秒"
	}
	if d < time.Minute {
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d 分 %d 秒", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%d 小时 %d 分", int(d.Hours()), int(d.Minutes())%60)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var GlobalScanStatus = &scanStatusTracker{}
