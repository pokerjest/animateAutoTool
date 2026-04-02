package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/jellyfin"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/security"
)

const managedJellyfinURL = "http://127.0.0.1:8096"
const managedAListURL = "http://127.0.0.1:5244"

func (m *Manager) startAlist() error {
	exeName := "alist"
	if runtime.GOOS == OSWindows {
		exeName += ".exe"
	}
	binPath := filepath.Join(m.BinDir, exeName)
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		fmt.Println("AList binary not found, skipping managed start.")
		return nil
	}
	dataDir := filepath.Join(m.DataDir, "alist")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create alist dir: %w", err)
	}

	if reason := managedAListConflictReason("127.0.0.1:5244", managedAListURL); reason != "" {
		fmt.Printf("AList managed start skipped: %s\n", reason)
		return nil
	}

	creds, err := bootstrap.LoadAListCredentials()
	if err != nil || creds.Password == "" {
		password, genErr := security.RandomPassword(24)
		if genErr != nil {
			return fmt.Errorf("failed to generate alist bootstrap password: %w", genErr)
		}
		creds = bootstrap.AListCredentials{
			URL:      managedAListURL,
			Username: "admin",
			Password: password,
		}
	}
	if creds.Username == "" {
		creds.Username = "admin"
	}
	if creds.URL == "" {
		creds.URL = managedAListURL
	}
	if err := bootstrap.SaveAListCredentials(creds); err != nil {
		return fmt.Errorf("failed to persist alist bootstrap credentials: %w", err)
	}

	cmdSetPass := exec.Command(binPath, "admin", "set", creds.Password, "--data", dataDir)
	if output, err := cmdSetPass.CombinedOutput(); err != nil {
		fmt.Printf("Alist set pass warning: %v, output: %s\n", err, string(output))
		// might fail if not initialized? usually works.
	}

	// 1.5. Patch config.json to ensure sqlite3 is used (fix for some environments)
	configFile := filepath.Join(dataDir, "config.json")
	if content, err := os.ReadFile(configFile); err == nil {
		newContent := strings.Replace(string(content), `"type": "sqlite"`, `"type": "sqlite3"`, 1)
		if newContent != string(content) {
			_ = os.WriteFile(configFile, []byte(newContent), 0600)
			fmt.Println("Patched alist config to use sqlite3")
		}
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
	binPath := filepath.Join(m.BinDir, qbExecutableName())

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

	if err := os.MkdirAll(confDir, 0755); err != nil {
		return err
	}

	configContent := `[Preferences]
WebUI\Enabled=true
WebUI\Port=8080
WebUI\LocalHostAuth=false
`

	return os.WriteFile(confFile, []byte(configContent), 0644) //nolint:gosec
}

func (m *Manager) startJellyfin() error {
	binDir := filepath.Join(m.BinDir, "jellyfin")
	binPath := filepath.Join(binDir, jellyfinExecutableName())

	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		fmt.Println("Jellyfin binary not found, skipping managed start.")
		return nil
	}

	if reason := managedJellyfinConflictReason("127.0.0.1:8096", managedJellyfinURL); reason != "" {
		fmt.Printf("Jellyfin managed start skipped: %s\n", reason)
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
	ffmpegPath := filepath.Join(m.BinDir, "ffmpeg", ffmpegExecutableName())

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
		creds, err := bootstrap.LoadJellyfinCredentials()
		if err != nil || creds.Password == "" {
			password, genErr := security.RandomPassword(24)
			if genErr != nil {
				fmt.Printf("Jellyfin bootstrap password generation failed: %v\n", genErr)
				return
			}
			creds = bootstrap.JellyfinCredentials{
				URL:      managedJellyfinURL,
				Username: "admin",
				Password: password,
			}
		}
		if creds.URL == "" {
			creds.URL = managedJellyfinURL
		}
		if creds.Username == "" {
			creds.Username = "admin"
		}
		if err := bootstrap.SaveJellyfinCredentials(creds); err != nil {
			fmt.Printf("Jellyfin bootstrap credential persist failed: %v\n", err)
		}
		key, err := jellyfin.AttemptZeroConfig(creds.URL, creds.Username, creds.Password)
		if err == nil && key != "" {
			fmt.Println("Jellyfin zero-config succeeded and stored a fresh API key.")
			creds.APIKey = key
			if err := bootstrap.SaveJellyfinCredentials(creds); err != nil {
				fmt.Printf("Jellyfin bootstrap credential update failed: %v\n", err)
			}
			if waitForDatabase(30 * time.Second) {
				_ = db.SaveGlobalConfig(model.ConfigKeyJellyfinUrl, creds.URL)
				_ = db.SaveGlobalConfig(model.ConfigKeyJellyfinUsername, creds.Username)
				_ = db.SaveGlobalConfig(model.ConfigKeyJellyfinPassword, creds.Password)
				_ = db.SaveGlobalConfig(model.ConfigKeyJellyfinApiKey, creds.APIKey)
			}
		} else if err != nil {
			if errors.Is(err, jellyfin.ErrAlreadyConfigured) {
				fmt.Printf("Jellyfin zero-config skipped: %v\n", err)
			} else {
				fmt.Printf("Jellyfin Zero-Config note: %v\n", err)
			}
		}
	}()

	return nil
}

func waitForDatabase(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if db.DB != nil {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}

	return false
}

func managedJellyfinConflictReason(address, baseURL string) string {
	client := jellyfin.NewClient(baseURL, "")
	info, err := client.GetPublicInfo()
	if err == nil {
		label := "Jellyfin"
		if name := strings.TrimSpace(info.ServerName); name != "" {
			label = name
		}
		if version := strings.TrimSpace(info.Version); version != "" {
			return fmt.Sprintf("%s %s is already listening on %s", label, version, address)
		}
		return fmt.Sprintf("%s is already listening on %s", label, address)
	}

	if listener, err := net.Listen("tcp", address); err == nil {
		_ = listener.Close()
		return ""
	}

	return fmt.Sprintf("address %s is already in use by another process", address)
}

func managedAListConflictReason(address, baseURL string) string {
	if summary, err := getAListSummary(baseURL); err == nil && summary != "" {
		return fmt.Sprintf("%s is already listening on %s", summary, address)
	}

	if listener, err := net.Listen("tcp", address); err == nil {
		_ = listener.Close()
		return ""
	}

	return fmt.Sprintf("address %s is already in use by another process", address)
}

func getAListSummary(baseURL string) (string, error) {
	client := &http.Client{Timeout: 2 * time.Second}

	resp, err := client.Get(strings.TrimRight(baseURL, "/") + "/api/public/settings")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
		Data struct {
			SiteTitle string `json:"site_title"`
			Version   string `json:"version"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.Code != 200 {
		return "", fmt.Errorf("alist api error: %s", payload.Msg)
	}

	label := strings.TrimSpace(payload.Data.SiteTitle)
	if label == "" {
		label = "AList"
	}
	if version := strings.TrimSpace(payload.Data.Version); version != "" {
		return fmt.Sprintf("%s %s", label, version), nil
	}
	return label, nil
}
