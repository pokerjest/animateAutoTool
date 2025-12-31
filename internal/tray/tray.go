package tray

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/getlantern/systray"
	"github.com/pokerjest/animateAutoTool/internal/config"
)

// Run initializes the system tray and runs the application.
// startServerFunc is the function to start the main application logic (server).
// It must run in a separate goroutine because systray.Run blocks the main thread.
func Run(startServerFunc func()) {
	systray.Run(func() { onReady(startServerFunc) }, onExit)
}

func onReady(startServerFunc func()) {
	systray.SetIcon(IconData)
	systray.SetTitle("AnimateAutoTool")
	systray.SetTooltip("Animate Auto Tool")

	mOpen := systray.AddMenuItem("Open Web UI", "Open the web dashboard")
	mQuit := systray.AddMenuItem("Quit", "Quit the application")

	// Start the main server logic in a separate goroutine
	go startServerFunc()

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				port := config.AppConfig.Server.Port
				// If port is 0, it might not be initialized yet or using default.
				// Assuming config is loaded before tray starts or during startServerFunc.
				// Ideally config should be loaded before calling tray.Run.
				// Refactoring of main.go ensures config is loaded.
				if port == 0 {
					port = 8080 // Fallback or handling
				}
				openBrowser(fmt.Sprintf("http://localhost:%d", port))
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func onExit() {
	// Cleanup here if needed.
	// Since systray.Quit() terminates the app, we might want to ensure graceful shutdown of server.
	// But simple termination is often acceptable for client apps.
}

func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		fmt.Printf("Error opening browser: %v\n", err)
	}
}
