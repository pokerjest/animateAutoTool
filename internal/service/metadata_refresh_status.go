package service

import "sync"

type RefreshStatus struct {
	Total        int    `json:"total"`
	Current      int    `json:"current"`
	CurrentTitle string `json:"current_title"`
	IsRunning    bool   `json:"is_running"`
	LastResult   string `json:"last_result"`
}

type refreshStatusTracker struct {
	mu     sync.RWMutex
	status RefreshStatus
}

const (
	metadataSourceBangumi = "bangumi"
	metadataSourceTMDB    = "tmdb"
	metadataSourceAniList = "anilist"
)

func (t *refreshStatusTracker) TryStart() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.status.IsRunning {
		return false
	}

	t.status = RefreshStatus{IsRunning: true}
	return true
}

func (t *refreshStatusTracker) SetTotal(total int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.status.Total = total
	t.status.Current = 0
	t.status.CurrentTitle = ""
	t.status.LastResult = ""
}

func (t *refreshStatusTracker) UpdateProgress(current int, title string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.status.Current = current
	t.status.CurrentTitle = title
}

func (t *refreshStatusTracker) Finish(result string) RefreshStatus {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.status.IsRunning = false
	t.status.CurrentTitle = ""
	t.status.LastResult = result

	return t.status
}

func (t *refreshStatusTracker) Snapshot() RefreshStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.status
}

var GlobalRefreshStatus = &refreshStatusTracker{}
