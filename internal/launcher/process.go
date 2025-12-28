package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func (m *Manager) startAlist() error {
	exeName := "alist"
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	binPath := filepath.Join(m.BinDir, exeName)
	dataDir := filepath.Join(m.DataDir, "alist")

	// Create data dir
	os.MkdirAll(dataDir, 0755)

	// 1. Force set admin password to 'admin' (or random) for zero-setup
	// We use 'admin' for simplicity in this local tool context, user can change later
	// But actually, we should store it in our config or use a fixed one.
	cmdSetPass := exec.Command(binPath, "admin", "set", "admin", "--data", dataDir)
	if output, err := cmdSetPass.CombinedOutput(); err != nil {
		fmt.Printf("Alist set pass warning: %v, output: %s\n", err, string(output))
		// might fail if not initialized? usually works.
	}

	// 2. Start Server
	cmd := exec.CommandContext(m.Ctx, binPath, "server", "--data", dataDir)
	// Redirect stdout/stderr so we can debug, or suppress
	// Logging to file would be better
	logFile, _ := os.Create(filepath.Join(m.DataDir, "alist.log"))
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start alist: %w", err)
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		cmd.Wait()
	}()

	fmt.Println("Alist started (Port 5244)")
	return nil
}

func (m *Manager) startQB() error {
	exeName := "qbittorrent.exe"
	if runtime.GOOS != "windows" {
		exeName = "qbittorrent-nox"
	}
	binPath := filepath.Join(m.BinDir, exeName)

	// Check if binary exists (might be skipped on macOS)
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		fmt.Println("qBittorrent binary not found, skipping managed start.")
		return nil
	}

	// Use isolated profile directory
	profileDir := filepath.Join(m.DataDir, "qbittorrent")
	os.MkdirAll(profileDir, 0755)

	// QBittorrent portable arguments?
	// The portable version usually uses "profile" folder in current dir if not specified.
	// But we can specify "--profile=..."
	// Also ensure webui is on.
	// But QB doesn't have a simple CLI flag to force enable WebUI on first run without config file manipulation.
	// We might need to pre-write a qBittorrent.conf!

	// Create default config if not exists to enable WebUI
	m.ensureQBConfig(profileDir)

	cmd := exec.CommandContext(m.Ctx, binPath, "--profile="+filepath.Clean(profileDir))
	// For Linux/Mac nox, usually it stays in foreground unless -d is passed.
	// We want it in foreground (as child of manager) to control it.

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start qBittorrent: %w", err)
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		cmd.Wait()
	}()

	fmt.Println("qBittorrent started (Port 8080)")
	return nil
}

func (m *Manager) ensureQBConfig(profilePath string) {
	// Path: profilePath/qBittorrent/config/qBittorrent.conf
	confDir := filepath.Join(profilePath, "qBittorrent", "config")
	confFile := filepath.Join(confDir, "qBittorrent.conf")

	if _, err := os.Stat(confFile); err == nil {
		return
	}

	os.MkdirAll(confDir, 0755)

	// Minimal config to enable WebUI on 8080, admin/adminadmin
	// QB 4.x+ uses INI format
	configContent := `[Preferences]
WebUI\Enabled=true
WebUI\Port=8080
WebUI\Username=admin
WebUI\Password_PBKDF2="@ByteArray(ARQ77eY1NUZaQsuDHbIMCA==:0WMRkYTUWVT9wVvdDtHAjU9b3b7uB8NR1Gur2hmQCvCDpm39Q+PsJRJPaCU51gEi)"
`
	// Password is 'adminadmin' (PBKDF2 hash)

	os.WriteFile(confFile, []byte(configContent), 0644)
}
