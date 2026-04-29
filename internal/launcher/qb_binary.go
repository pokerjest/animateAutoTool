package launcher

import (
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

func ManagedQBExecutablePath(binDir string) string {
	exeName := "qbittorrent.exe"
	if runtime.GOOS != OSWindows {
		exeName = "qbittorrent-nox"
	}

	if strings.Contains(binDir, "/") && !strings.Contains(binDir, `\`) {
		return path.Join(binDir, exeName)
	}
	return filepath.Join(binDir, exeName)
}

func HasManagedQBBinary(binDir string) bool {
	_, err := os.Stat(ManagedQBExecutablePath(binDir))
	return err == nil
}
