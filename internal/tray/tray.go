//go:build windows

package tray

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/getlantern/systray"
	"github.com/pokerjest/animateAutoTool/internal/config"
)

// Run initializes the system tray and runs the application.
// startServerFunc is the function to start the main application logic (server).
// It must run in a separate goroutine because systray.Run blocks the main thread.
func Run(parent context.Context, startServerFunc func(context.Context) error) error {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	serverDone := make(chan error, 1)
	systray.Run(func() {
		onReady(ctx, cancel, func() {
			defer cancel()
			serverDone <- startServerFunc(ctx)
		})
	}, cancel)
	cancel()
	return <-serverDone
}

func onReady(ctx context.Context, cancel context.CancelFunc, startServerFunc func()) {
	fmt.Printf("DEBUG: Embedded IconData size: %d bytes\n", len(IconData))

	// Adapting Icon for Windows
	icon := IconData
	if runtime.GOOS == "windows" {
		if ico, err := PngToIco(IconData); err == nil {
			fmt.Printf("DEBUG: Converted to ICO successfully. Size: %d bytes\n", len(ico))
			icon = ico
		} else {
			fmt.Printf("DEBUG: Failed to convert icon to ICO: %v\n", err)
		}
	} else {
		fmt.Println("DEBUG: Skipping ICO conversion (not Windows)")
	}
	systray.SetIcon(icon)
	systray.SetTitle("AnimateAutoTool")
	systray.SetTooltip("Animate Auto Tool")

	// Menu Items
	mOpen := systray.AddMenuItem("Open Web UI", "Open the web dashboard")
	mOpenData := systray.AddMenuItem("Open Data Folder", "Open the application data folder")
	systray.AddSeparator()
	// mRestart := systray.AddMenuItem("Restart Service", "Restart the background server") // TODO: Implement restart logic
	mQuit := systray.AddMenuItem("Quit", "Quit the application")

	// Start the main server logic in a separate goroutine
	go startServerFunc()
	go func() {
		timer := time.NewTimer(1500 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return
		}
		port := config.AppConfig.Server.Port
		if port == 0 {
			port = 8306
		}
		openBrowser(fmt.Sprintf("http://127.0.0.1:%d/login", port))
	}()

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				port := config.AppConfig.Server.Port
				if port == 0 {
					port = 8080
				}
				openBrowser(fmt.Sprintf("http://localhost:%d", port))
			case <-mOpenData.ClickedCh:
				// Open Data Directory
				dataDir := config.AppConfig.Database.Path
				// dataDir is a file path to sqlite db, get dir
				dir := filepath.Dir(dataDir)
				openBrowser(dir)
			case <-mQuit.ClickedCh:
				cancel()
				systray.Quit()
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		<-ctx.Done()
		systray.Quit()
	}()
}

func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		//nolint:gosec // url points to the local app UI or a local data folder chosen by the app.
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		//nolint:gosec // url points to the local app UI or a local data folder chosen by the app.
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		//nolint:gosec // url points to the local app UI or a local data folder chosen by the app.
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		fmt.Printf("Error opening browser: %v\n", err)
	}
}
