package launcher

import (
	"context"
	"fmt"
	"os"
	"sync"
)

type Manager struct {
	BinDir  string
	DataDir string
	Ctx     context.Context
	Cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		BinDir:  "bin",
		DataDir: "data",
		Ctx:     ctx,
		Cancel:  cancel,
	}
}

func (m *Manager) EnsureBinaries() error {
	if err := os.MkdirAll(m.BinDir, 0755); err != nil {
		return fmt.Errorf("failed to create bin dir: %w", err)
	}

	// Check and download Alist
	if err := m.ensureAlist(); err != nil {
		return err
	}

	// Check and download QBittorrent
	if err := m.ensureQB(); err != nil {
		return err
	}

	return nil
}

func (m *Manager) StartAll() error {
	// Start Alist
	if err := m.startAlist(); err != nil {
		return err
	}

	// Start QBittorrent
	if err := m.startQB(); err != nil {
		return err
	}

	return nil
}

func (m *Manager) StopAll() {
	m.Cancel()
	m.wg.Wait()
}
