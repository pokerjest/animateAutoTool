package launcher

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/config"
)

const (
	// Alist (Using GhProxy for CN acceleration by default, or fallback)
	// Switch to ghproxy.net which might be more stable
	GhProxy = "https://gh-proxy.com/"

	AlistUrlWindows   = GhProxy + "https://github.com/alist-org/alist/releases/latest/download/alist-windows-amd64.zip"
	AlistUrlLinux     = GhProxy + "https://github.com/alist-org/alist/releases/latest/download/alist-linux-amd64.tar.gz"
	AlistUrlLinuxArm  = GhProxy + "https://github.com/alist-org/alist/releases/latest/download/alist-linux-arm64.tar.gz"
	AlistUrlDarwin    = GhProxy + "https://github.com/alist-org/alist/releases/latest/download/alist-darwin-amd64.tar.gz"
	AlistUrlDarwinArm = GhProxy + "https://github.com/alist-org/alist/releases/latest/download/alist-darwin-arm64.tar.gz"

	// qBittorrent (Enhanced Edition / Static)
	QBUrlWindowsPortable = GhProxy + "https://github.com/c0re100/qBittorrent-Enhanced-Edition/releases/download/release-4.6.1.10/qt6_x64_portable.zip"
	QBUrlLinuxAmd64      = GhProxy + "https://github.com/c0re100/qBittorrent-Enhanced-Edition/releases/download/release-4.6.1.10/qbittorrent-enhanced-nox_x86_64-linux-musl_static.zip"
	QBUrlLinuxArm64      = GhProxy + "https://github.com/c0re100/qBittorrent-Enhanced-Edition/releases/download/release-4.6.1.10/qbittorrent-enhanced-nox_aarch64-linux-musl_static.zip"

	// Jellyfin (Direct link)
	JellyfinUrlWindows = "https://repo.jellyfin.org/files/server/windows/latest-stable/amd64/jellyfin_10.11.5-amd64.zip"
	JellyfinUrlLinux   = "https://repo.jellyfin.org/files/server/linux/latest-stable/amd64/jellyfin_10.11.5-amd64.tar.gz"
	JellyfinUrlMac     = "https://repo.jellyfin.org/files/server/macos/latest-stable/amd64/jellyfin_10.11.5-amd64.tar.xz"

	// FFmpeg (Jellyfin version)
	FFmpegUrlWindows    = "https://repo.jellyfin.org/files/ffmpeg/windows/latest-7.x/win64/jellyfin-ffmpeg_7.1.3-1_portable_win64-clang-gpl.zip"
	FFmpegUrlLinuxAmd64 = "https://repo.jellyfin.org/files/ffmpeg/linux/latest-7.x/amd64/jellyfin-ffmpeg_7.1.3-1_portable_linux64-gpl.tar.xz"
	FFmpegUrlLinuxArm64 = "https://repo.jellyfin.org/files/ffmpeg/linux/latest-7.x/arm64/jellyfin-ffmpeg_7.1.3-1_portable_linuxarm64-gpl.tar.xz"
	FFmpegUrlMacAmd64   = "https://repo.jellyfin.org/files/ffmpeg/macos/latest-7.x/x86_64/jellyfin-ffmpeg_7.1.3-1_portable_mac64-gpl.tar.xz"
	FFmpegUrlMacArm64   = "https://repo.jellyfin.org/files/ffmpeg/macos/latest-7.x/arm64/jellyfin-ffmpeg_7.1.3-1_portable_macarm64-gpl.tar.xz"

	OSWindows = "windows"
	OSLinux   = "linux"
	OSDarwin  = "darwin"
	ArchAmd64 = "amd64"
	ArchArm64 = "arm64"

	ExtZip   = ".zip"
	ExtTarGz = ".tar.gz"
	ExtTarXz = ".tar.xz"

	qbExecutableWindows = "qbittorrent.exe"
	qbExecutableNox     = "qbittorrent-nox"
	qbEnhancedNox       = "qbittorrent-enhanced-nox"
	jellyfinExecutable  = "jellyfin.exe"
	jellyfinExecutableN = "jellyfin"
	ffmpegExecutable    = "ffmpeg.exe"
	ffmpegExecutableN   = "ffmpeg"
	ffprobeExecutableN  = "ffprobe"
)

var ErrQBManualInstallRequired = errors.New("qBittorrent auto-download requires a manual install on this platform")

func (m *Manager) ensureAlist() error {
	exeName := "alist"
	if runtime.GOOS == OSWindows {
		exeName += ".exe"
	}
	targetPath := filepath.Join(m.BinDir, exeName)

	if _, err := os.Stat(targetPath); err == nil {
		return nil
	}
	if config.AppConfig != nil && !config.AppConfig.Managed.DownloadMissing {
		fmt.Println("AList auto-download disabled by configuration; skipping download.")
		return nil
	}

	fmt.Printf("Downloading Alist for %s/%s...\n", runtime.GOOS, runtime.GOARCH)

	url, isTarGz, err := getAlistUrl()
	if err != nil {
		return err
	}

	ext := ExtZip
	if isTarGz {
		ext = ExtTarGz
	}
	tmpFile := filepath.Join(m.BinDir, "alist"+ext)

	if err := downloadFile(url, tmpFile); err != nil {
		return err
	}
	defer os.Remove(tmpFile)

	if isTarGz {
		if err := untar(tmpFile, m.BinDir); err != nil {
			return err
		}
	} else {
		if err := unzip(tmpFile, m.BinDir); err != nil {
			return err
		}
	}

	// chmod +x
	if err := os.Chmod(targetPath, 0755); err != nil {
		return err
	}

	return nil
}

func (m *Manager) ensureQB() error {
	targetPath := filepath.Join(m.BinDir, qbExecutableName())
	if _, err := os.Stat(targetPath); err == nil {
		return nil
	}

	url, err := getQBUrl()
	if err != nil {
		if errors.Is(err, ErrQBManualInstallRequired) {
			return nil
		}
		// Fallback or warning
		fmt.Printf("Warning: %v. Please install qBittorrent manually.\n", err)
		return nil // Non-fatal
	}

	fmt.Printf("Downloading qBittorrent for %s/%s...\n", runtime.GOOS, runtime.GOARCH)

	tmpZip := filepath.Join(m.BinDir, "qb.zip")
	if err := downloadFile(url, tmpZip); err != nil {
		return err
	}
	defer os.Remove(tmpZip)

	// Linux static builds are also zips in c0re100 release
	if err := unzip(tmpZip, m.BinDir); err != nil {
		return err
	}

	// Post-processing for Linux: Rename binary
	if runtime.GOOS == OSLinux {
		// Find potential binary names
		candidates := []string{qbEnhancedNox, qbExecutableNox}
		for _, name := range candidates {
			path := filepath.Join(m.BinDir, name)
			if _, err := os.Stat(path); err == nil {
				if err := os.Rename(path, targetPath); err != nil {
					return err
				}
				break
			}
		}
		if err := os.Chmod(targetPath, 0755); err != nil {
			return err
		}
	}

	return nil
}

func (m *Manager) EnsureJellyfin() error {
	jellyfinDir := filepath.Join(m.BinDir, "jellyfin")
	if err := m.ensureManagedJellyfinServer(jellyfinDir); err != nil {
		return err
	}
	if _, err := os.Stat(jellyfinDir); err != nil {
		return nil
	}

	return m.ensureManagedFFmpeg(filepath.Join(m.BinDir, "ffmpeg"))
}

func (m *Manager) ensureManagedJellyfinServer(jellyfinDir string) error {
	if _, err := os.Stat(jellyfinDir); err == nil {
		return nil
	}
	if config.AppConfig != nil && !config.AppConfig.Managed.DownloadMissing {
		fmt.Println("Jellyfin auto-download disabled by configuration; skipping download.")
		return nil
	}

	fmt.Printf("Downloading Jellyfin for %s/%s...\n", runtime.GOOS, runtime.GOARCH)
	url, err := getJellyfinUrl()
	if err != nil {
		return err
	}
	if err := m.downloadAndInstallDir(url, "jellyfin_dl", "jellyfin_tmp", jellyfinDir); err != nil {
		return err
	}
	if runtime.GOOS == OSWindows {
		return nil
	}

	return os.Chmod(filepath.Join(jellyfinDir, jellyfinExecutableName()), 0755)
}

func (m *Manager) ensureManagedFFmpeg(ffmpegDir string) error {
	if _, err := os.Stat(ffmpegDir); err == nil {
		return nil
	}

	fmt.Printf("Downloading FFmpeg for %s...\n", runtime.GOOS)
	url, err := getFFmpegUrl()
	if err != nil {
		return err
	}
	if err := m.downloadAndInstallDir(url, "ffmpeg_dl", "ffmpeg_tmp", ffmpegDir); err != nil {
		return err
	}
	if runtime.GOOS == OSWindows {
		return nil
	}
	if err := os.Chmod(filepath.Join(ffmpegDir, ffmpegExecutableName()), 0755); err != nil {
		return err
	}
	return os.Chmod(filepath.Join(ffmpegDir, ffprobeExecutableN), 0755)
}

func (m *Manager) downloadAndInstallDir(url, downloadPrefix, extractDirName, targetDir string) error {
	ext := archiveExtension(url)
	tmpFile := filepath.Join(m.BinDir, downloadPrefix+ext)
	if err := downloadFile(url, tmpFile); err != nil {
		return err
	}
	defer os.Remove(tmpFile)

	tmpExtract := filepath.Join(m.BinDir, extractDirName)
	_ = os.RemoveAll(tmpExtract)
	defer os.RemoveAll(tmpExtract)

	if ext == ExtZip {
		if err := unzip(tmpFile, tmpExtract); err != nil {
			return err
		}
	} else {
		if err := untar(tmpFile, tmpExtract); err != nil {
			return err
		}
	}

	srcDir, err := extractedRootDir(tmpExtract)
	if err != nil {
		return err
	}

	return os.Rename(srcDir, targetDir)
}

func archiveExtension(url string) string {
	switch {
	case strings.HasSuffix(url, ExtTarGz):
		return ExtTarGz
	case strings.HasSuffix(url, ExtTarXz):
		return ExtTarXz
	default:
		return ExtZip
	}
}

func extractedRootDir(extractDir string) (string, error) {
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return "", err
	}
	if len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(extractDir, entries[0].Name()), nil
	}
	return extractDir, nil
}

func qbExecutableName() string {
	if runtime.GOOS == OSWindows {
		return qbExecutableWindows
	}
	return qbExecutableNox
}

func jellyfinExecutableName() string {
	if runtime.GOOS == OSWindows {
		return jellyfinExecutable
	}
	return jellyfinExecutableN
}

func ffmpegExecutableName() string {
	if runtime.GOOS == OSWindows {
		return ffmpegExecutable
	}
	return ffmpegExecutableN
}

func getAlistUrl() (string, bool, error) {
	os := runtime.GOOS
	arch := runtime.GOARCH

	switch os {
	case OSWindows:
		return AlistUrlWindows, false, nil
	case OSLinux:
		if arch == ArchArm64 {
			return AlistUrlLinuxArm, true, nil
		}
		return AlistUrlLinux, true, nil
	case OSDarwin:
		if arch == ArchArm64 {
			return AlistUrlDarwinArm, true, nil
		}
		return AlistUrlDarwin, true, nil
	default:
		return "", false, fmt.Errorf("unsupported OS: %s", os)
	}
}

func getQBUrl() (string, error) {
	return getQBUrlForPlatform(runtime.GOOS, runtime.GOARCH)
}

func getQBUrlForPlatform(goos, goarch string) (string, error) {
	switch goos {
	case OSWindows:
		// Portable zip not consistently available for newer versions.
		return "", fmt.Errorf("%w: please install qBittorrent manually (e.g. from PortableApps or official installer)", ErrQBManualInstallRequired)
	case OSLinux:
		if goarch == ArchArm64 {
			return QBUrlLinuxArm64, nil
		}
		return QBUrlLinuxAmd64, nil
	case OSDarwin:
		return "", fmt.Errorf("%w: auto-download for macOS qbittorrent not supported yet. Please install it to /Applications/qbittorrent.app", ErrQBManualInstallRequired)
	default:
		return "", fmt.Errorf("unsupported OS: %s", goos)
	}
}

func getJellyfinUrl() (string, error) {
	switch runtime.GOOS {
	case OSWindows:
		return JellyfinUrlWindows, nil
	case OSLinux:
		return JellyfinUrlLinux, nil
	case OSDarwin:
		return JellyfinUrlMac, nil
	default:
		return "", fmt.Errorf("unsupported OS")
	}
}

func getFFmpegUrl() (string, error) {
	os := runtime.GOOS
	arch := runtime.GOARCH

	switch os {
	case OSWindows:
		return FFmpegUrlWindows, nil
	case OSLinux:
		if arch == ArchAmd64 {
			return FFmpegUrlLinuxAmd64, nil
		} else if arch == ArchArm64 {
			return FFmpegUrlLinuxArm64, nil
		}
		return "", fmt.Errorf("unsupported architecture for Linux: %s", arch)
	case OSDarwin:
		if arch == ArchArm64 {
			return FFmpegUrlMacArm64, nil
		}
		return FFmpegUrlMacAmd64, nil
	default:
		return "", fmt.Errorf("unsupported OS")
	}
}

func downloadFile(url string, filepath string) error {
	var lastErr error
	for i := 0; i < 3; i++ {
		if i > 0 {
			fmt.Printf("Retrying download (%d/3)...\n", i+1)
			// Add a small delay
			// time.Sleep(time.Second) // need import time if not present
		}

		lastErr = downloadFileOnce(url, filepath)
		if lastErr == nil {
			return nil
		}
		fmt.Printf("Download failed: %v\n", lastErr)
		// Clean up partial file
		os.Remove(filepath)
	}
	return fmt.Errorf("failed after 3 retries: %w", lastErr)
}

func downloadFileOnce(url string, filepath string) error {
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Use standard client
	client := &http.Client{}

	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	n, err := io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	if n == 0 {
		return fmt.Errorf("downloaded 0 bytes")
	}

	return nil
}

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			continue // Skip illegal paths
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(fpath, os.ModePerm); err != nil {
				return err
			}
			continue
		}

		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		// G110: Decompression bomb mitigation
		// Limit max file size to 10GB
		const maxFileSize = 10 * 1024 * 1024 * 1024
		if f.FileInfo().Size() > maxFileSize {
			outFile.Close()
			rc.Close()
			return fmt.Errorf("file %s too large (potential zip bomb)", f.Name)
		}

		_, err = io.Copy(outFile, io.LimitReader(rc, maxFileSize))

		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}
	return nil
}

func untar(src, dest string) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	var tr *tar.Reader
	if err == nil {
		defer gzr.Close()
		tr = tar.NewReader(gzr)
	} else {
		// Not gzip? maybe just tar or tar.xz
		// Reset file
		if _, err := file.Seek(0, 0); err != nil {
			return err
		}
		if strings.HasSuffix(src, ".tar.xz") {
			// Quick hack: use system tar if available, since pure go xz is not in stdlib
			// This is "cheating" but effective for zero-dependency portability
			return untarSystem(src, dest)
		}
		// Try plain tar
		tr = tar.NewReader(file)
	}

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(dest, header.Name)
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			out, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			// G110: Decompression bomb mitigation
			const maxFileSize = 10 * 1024 * 1024 * 1024 // 10GB
			if _, err := io.Copy(out, io.LimitReader(tr, maxFileSize)); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
	return nil
}

func untarSystem(src, dest string) error {
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	cmd := exec.Command("tar", "-xf", src, "-C", dest)
	// tar usually handles auto detection of compression (z, J, etc) on modern versions
	return cmd.Run()
}
