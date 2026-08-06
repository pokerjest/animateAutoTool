//go:build !windows

package tray

import (
	"context"
	"log"
)

// Run falls back to directly starting the server on non-Windows builds.
// The application already defaults to headless mode on these platforms,
// but this keeps the package buildable in Linux CI even when imported.
func Run(parent context.Context, startServerFunc func(context.Context) error) error {
	log.Println("System tray integration is unavailable on this platform build; starting without tray.")
	return startServerFunc(parent)
}
