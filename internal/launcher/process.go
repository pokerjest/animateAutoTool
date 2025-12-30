package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/jellyfin"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm/clause"
)

func (m *Manager) startAlist() error {
	exeName := "alist"
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	binPath := filepath.Join(m.BinDir, exeName)
	dataDir := filepath.Join(m.DataDir, "alist")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create alist dir: %w", err)
	}

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
		if err := cmd.Wait(); err != nil {
			fmt.Printf("Alist process exited with error: %v\n", err)
		}
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
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		return fmt.Errorf("failed to create qb dir: %w", err)
	}

	// QBittorrent portable arguments?
	// The portable version usually uses "profile" folder in current dir if not specified.
	// But we can specify "--profile=..."
	// Also ensure webui is on.
	// But QB doesn't have a simple CLI flag to force enable WebUI on first run without config file manipulation.
	// We might need to pre-write a qBittorrent.conf!

	// Create default config if not exists to enable WebUI
	if err := m.ensureQBConfig(profileDir); err != nil {
		return err
	}

	cmd := exec.CommandContext(m.Ctx, binPath, "--profile="+filepath.Clean(profileDir))
	// For Linux/Mac nox, usually it stays in foreground unless -d is passed.
	// We want it in foreground (as child of manager) to control it.

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start qBittorrent: %w", err)
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		if err := cmd.Wait(); err != nil {
			fmt.Printf("qBittorrent process exited with error: %v\n", err)
		}
	}()

	fmt.Println("qBittorrent started (Port 8080)")
	return nil
}

func (m *Manager) ensureQBConfig(profilePath string) error {
	// Path: profilePath/qBittorrent/config/qBittorrent.conf
	confDir := filepath.Join(profilePath, "qBittorrent", "config")
	confFile := filepath.Join(confDir, "qBittorrent.conf")

	if _, err := os.Stat(confFile); err == nil {
		return nil
	}

	if err := os.MkdirAll(confDir, 0755); err != nil {
		return err
	}

	// Minimal config to enable WebUI on 8080, admin/adminadmin
	// QB 4.x+ uses INI format
	configContent := `[Preferences]
WebUI\Enabled=true
WebUI\Port=8080
WebUI\Username=admin
WebUI\Password_PBKDF2="@ByteArray(ARQ77eY1NUZaQsuDHbIMCA==:0WMRkYTUWVT9wVvdDtHAjU9b3b7uB8NR1Gur2hmQCvCDpm39Q+PsJRJPaCU51gEi)"
`
	// Password is 'adminadmin' (PBKDF2 hash)

	return os.WriteFile(confFile, []byte(configContent), 0644) //nolint:gosec
}

func (m *Manager) startJellyfin() error {
	exeName := "jellyfin.exe"
	if runtime.GOOS != "windows" {
		exeName = "jellyfin"
	}
	binDir := filepath.Join(m.BinDir, "jellyfin")
	binPath := filepath.Join(binDir, exeName)

	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		fmt.Println("Jellyfin binary not found, skipping managed start.")
		return nil
	}

	dataDir := filepath.Join(m.DataDir, "jellyfin", "data")
	configDir := filepath.Join(m.DataDir, "jellyfin", "config")
	logDir := filepath.Join(m.DataDir, "jellyfin", "log")
	cacheDir := filepath.Join(m.DataDir, "jellyfin", "cache")

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}

	// FFmpeg path
	ffmpegName := "ffmpeg.exe"
	if runtime.GOOS != "windows" {
		ffmpegName = "ffmpeg"
	}
	ffmpegPath := filepath.Join(m.BinDir, "ffmpeg", ffmpegName)

	// If custom ffmpeg exists, use it. Otherwise let Jellyfin find system ffmpeg.
	ffmpegArg := ""
	if _, err := os.Stat(ffmpegPath); err == nil {
		ffmpegArg = ffmpegPath
	}

	args := []string{
		"--datadir", dataDir,
		"--configdir", configDir,
		"--logdir", logDir,
		"--cachedir", cacheDir,
	}

	// Pass ffmpeg path if we found it.
	// Note: Jellyfin flag for ffmpeg path might be specific or set in config.
	// CLI flag --ffmpeg might work for some versions, or ensure it's in PATH.
	// Actually, best way for portable is to ensure it finds it.
	// We can set environment variable JELLYFIN_FFMPEG_PATH? Or just simple config.
	// Let's rely on config. But for first run, maybe passing it is hard.
	// However, if we put ffmpeg inside binDir/jellyfin/ffmpeg, it might auto detect?
	// Let's append --ffmpeg if valid flag. Documentation says --ffmpeg <path> is valid for some generic builds.
	if ffmpegArg != "" {
		args = append(args, "--ffmpeg", ffmpegArg)
	}

	cmd := exec.CommandContext(m.Ctx, binPath, args...)

	logFile, _ := os.Create(filepath.Join(logDir, "startup.log"))
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Jellyfin: %w", err)
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		if err := cmd.Wait(); err != nil {
			fmt.Printf("Jellyfin process exited with error: %v\n", err)
		}
	}()

	fmt.Println("Jellyfin started (Port 8096)")

	// Attempt Zero-Config
	// We do this asynchronously so we don't block the main flow waiting for startup
	go func() {
		// Use default credentials or maybe configurable ones in future?
		// For now: admin / admin
		// Note: This password is weak, but user can change it.
		// Ideally we instruct user to change it.
		key, err := jellyfin.AttemptZeroConfig("http://localhost:8096", "admin", "admin")
		if err == nil && key != "" {
			fmt.Printf("✨ Jellyfin Zero-Config Success! API Key: %s\n", key)
			// Save to DB
			db.DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&model.GlobalConfig{Key: model.ConfigKeyJellyfinUrl, Value: "http://localhost:8096"})
			db.DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&model.GlobalConfig{Key: model.ConfigKeyJellyfinApiKey, Value: key})
		} else if err != nil {
			// Quietly fail if it's just not a fresh install
			// But can log verify errors
			fmt.Printf("Jellyfin Zero-Config note: %v\n", err)
		}
	}()

	return nil
}
