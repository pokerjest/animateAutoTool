package launcher

import (
	"os"
	"path/filepath"
	"runtime"
)

func ManagedQBExecutablePath(binDir string) string {
	exeName := "qbittorrent.exe"
	if runtime.GOOS != OSWindows {
		exeName = "qbittorrent-nox"
	}

	return filepath.Join(binDir, exeName)
}

func HasManagedQBBinary(binDir string) bool {
	_, err := os.Stat(ManagedQBExecutablePath(binDir))
	return err == nil
}
