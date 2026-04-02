package safeio

import (
	"io"
	"os"
)

// Close is for best-effort cleanup paths where there is no actionable recovery.
func Close(c io.Closer) {
	if c != nil {
		_ = c.Close()
	}
}

// Remove is for best-effort temp file cleanup.
func Remove(path string) {
	_ = os.Remove(path)
}

// RemoveAll is for best-effort temp directory cleanup.
func RemoveAll(path string) {
	_ = os.RemoveAll(path)
}
