//go:build !windows

package tray

import "log"

// Run falls back to directly starting the server on non-Windows builds.
// The application already defaults to headless mode on these platforms,
// but this keeps the package buildable in Linux CI even when imported.
func Run(startServerFunc func()) {
	log.Println("System tray integration is unavailable on this platform build; starting without tray.")
	startServerFunc()
}
