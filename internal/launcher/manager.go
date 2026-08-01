package launcher

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/config"
)

type Manager struct {
	BinDir            string
	DataDir           string
	Ctx               context.Context
	Cancel            context.CancelFunc
	wg                sync.WaitGroup
	stopOnce          sync.Once
	databaseReady     chan struct{}
	databaseReadyOnce sync.Once

	startAlistFn      func() error
	startQBFunc       func() error
	startJellyfinFunc func() error
}

func NewManager(parent context.Context) *Manager {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	binDir := config.BinDir()
	dataDir := config.DataDir()
	return &Manager{
		BinDir:        binDir,
		DataDir:       dataDir,
		Ctx:           ctx,
		Cancel:        cancel,
		databaseReady: make(chan struct{}),
	}
}

func (m *Manager) NotifyDatabaseReady() {
	if m == nil {
		return
	}
	m.databaseReadyOnce.Do(func() {
		if m.databaseReady == nil {
			m.databaseReady = make(chan struct{})
		}
		close(m.databaseReady)
	})
}

func (m *Manager) waitForDatabase(timeout time.Duration) bool {
	if m == nil {
		return false
	}
	if m.databaseReady == nil {
		m.databaseReady = make(chan struct{})
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-m.Ctx.Done():
		return false
	case <-m.databaseReady:
		return true
	case <-timer.C:
		return false
	}
}

func (m *Manager) EnsureBinaries() error {
	if err := os.MkdirAll(m.BinDir, 0755); err != nil {
		return fmt.Errorf("failed to create bin dir: %w", err)
	}

	// Check and download Alist
	if err := m.ensureAlist(); err != nil {
		fmt.Printf("AList setup warning: %v\n", err)
	}

	// Check and download QBittorrent
	if err := m.ensureQB(); err != nil {
		return err
	}

	// Check and download Jellyfin
	if err := m.EnsureJellyfin(); err != nil {
		fmt.Printf("Jellyfin setup warning: %v\n", err)
		// Don't fail the whole app for optional component
	}

	return nil
}

func (m *Manager) StartAll() error {
	startAlist := m.startAlist
	if m.startAlistFn != nil {
		startAlist = m.startAlistFn
	}
	if err := startAlist(); err != nil {
		return err
	}

	startQB := m.startQB
	if m.startQBFunc != nil {
		startQB = m.startQBFunc
	}
	if err := startQB(); err != nil {
		m.StopAll()
		return err
	}

	startJellyfin := m.startJellyfin
	if m.startJellyfinFunc != nil {
		startJellyfin = m.startJellyfinFunc
	}
	if err := startJellyfin(); err != nil {
		fmt.Printf("Jellyfin start warning: %v\n", err)
	}

	return nil
}

func (m *Manager) StopAll() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() {
		m.Cancel()
	})
	m.wg.Wait()
}
