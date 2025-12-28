package launcher

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// Alist (Using GhProxy for CN acceleration by default, or fallback)
	// Switch to ghproxy.net which might be more stable
	GhProxy = "https://ghproxy.net/"

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
	JellyfinUrlLinux   = "https://repo.jellyfin.org/files/server/linux/latest-stable/amd64/jellyfin_10.11.5_linux-amd64.tar.gz"
	JellyfinUrlMac     = "https://repo.jellyfin.org/files/server/macos/latest-stable/amd64/jellyfin_10.11.5_mac-os-amd64.tar.gz"

	// FFmpeg (Jellyfin version)
	FFmpegUrlWindows = "https://repo.jellyfin.org/files/ffmpeg/windows/latest-7.x/win64/jellyfin-ffmpeg_7.1.3-1_portable_win64-clang-gpl.zip"
	FFmpegUrlLinux   = "https://repo.jellyfin.org/files/ffmpeg/linux/latest-7.x/amd64/jellyfin-ffmpeg_7.0.2-7_portable_linux-amd64.tar.xz"
	FFmpegUrlMac     = "https://repo.jellyfin.org/files/ffmpeg/macos/latest-7.x/amd64/jellyfin-ffmpeg_7.0.2-7_portable_mac-amd64.tar.gz"
)

func (m *Manager) ensureAlist() error {
	exeName := "alist"
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	targetPath := filepath.Join(m.BinDir, exeName)

	if _, err := os.Stat(targetPath); err == nil {
		return nil
	}

	fmt.Printf("Downloading Alist for %s/%s...\n", runtime.GOOS, runtime.GOARCH)

	url, isTarGz, err := getAlistUrl()
	if err != nil {
		return err
	}

	ext := ".zip"
	if isTarGz {
		ext = ".tar.gz"
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
	os.Chmod(targetPath, 0755)

	return nil
}

func (m *Manager) ensureQB() error {
	exeName := "qbittorrent.exe"
	if runtime.GOOS != "windows" {
		exeName = "qbittorrent-nox"
	}

	targetPath := filepath.Join(m.BinDir, exeName)
	if _, err := os.Stat(targetPath); err == nil {
		return nil
	}

	fmt.Printf("Downloading qBittorrent for %s/%s...\n", runtime.GOOS, runtime.GOARCH)

	url, err := getQBUrl()
	if err != nil {
		if strings.Contains(err.Error(), "manual_install_required") {
			fmt.Println("Info: qBittorrent auto-download not available for Windows. Please install it manually if needed.")
			return nil
		}
		// Fallback or warning
		fmt.Printf("Warning: %v. Please install qBittorrent manualy.\n", err)
		return nil // Non-fatal
	}

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
	if runtime.GOOS == "linux" {
		// Find potential binary names
		candidates := []string{"qbittorrent-enhanced-nox", "qbittorrent-nox"}
		for _, name := range candidates {
			path := filepath.Join(m.BinDir, name)
			if _, err := os.Stat(path); err == nil {
				os.Rename(path, targetPath)
				break
			}
		}
		os.Chmod(targetPath, 0755)
	}

	return nil
}

func (m *Manager) EnsureJellyfin() error {
	jellyfinDir := filepath.Join(m.BinDir, "jellyfin")
	ffmpegDir := filepath.Join(m.BinDir, "ffmpeg")

	// 1. Jellyfin Server
	jfExe := "jellyfin.exe"
	if runtime.GOOS != "windows" {
		jfExe = "jellyfin"
	}

	if _, err := os.Stat(jellyfinDir); err != nil {
		fmt.Printf("Downloading Jellyfin for %s/%s...\n", runtime.GOOS, runtime.GOARCH)
		url, err := getJellyfinUrl()
		if err != nil {
			return err
		}

		ext := ".zip"
		if strings.HasSuffix(url, ".tar.gz") {
			ext = ".tar.gz"
		}
		tmpFile := filepath.Join(m.BinDir, "jellyfin_dl"+ext)
		if err := downloadFile(url, tmpFile); err != nil {
			return err
		}
		defer os.Remove(tmpFile)

		tmpExtract := filepath.Join(m.BinDir, "jellyfin_tmp")
		os.RemoveAll(tmpExtract)

		if ext == ".zip" {
			if err := unzip(tmpFile, tmpExtract); err != nil {
				return err
			}
		} else {
			if err := untar(tmpFile, tmpExtract); err != nil {
				return err
			}
		}

		entries, _ := os.ReadDir(tmpExtract)
		srcDir := tmpExtract
		if len(entries) == 1 && entries[0].IsDir() {
			srcDir = filepath.Join(tmpExtract, entries[0].Name())
		}

		os.Rename(srcDir, jellyfinDir)
		os.RemoveAll(tmpExtract)

		if runtime.GOOS != "windows" {
			os.Chmod(filepath.Join(jellyfinDir, jfExe), 0755)
		}
	}

	// 2. FFmpeg
	if _, err := os.Stat(ffmpegDir); err != nil {
		fmt.Printf("Downloading FFmpeg for %s...\n", runtime.GOOS)
		url, err := getFFmpegUrl()
		if err != nil {
			return err
		}

		ext := ".zip"
		if strings.HasSuffix(url, ".tar.gz") {
			ext = ".tar.gz"
		} else if strings.HasSuffix(url, ".tar.xz") {
			ext = ".tar.xz"
		}

		tmpFile := filepath.Join(m.BinDir, "ffmpeg_dl"+ext)
		if err := downloadFile(url, tmpFile); err != nil {
			return err
		}
		defer os.Remove(tmpFile)

		tmpExtract := filepath.Join(m.BinDir, "ffmpeg_tmp")
		os.RemoveAll(tmpExtract)

		if ext == ".zip" {
			if err := unzip(tmpFile, tmpExtract); err != nil {
				return err
			}
		} else {
			if err := untar(tmpFile, tmpExtract); err != nil {
				return err
			}
		}

		entries, _ := os.ReadDir(tmpExtract)
		srcDir := tmpExtract
		if len(entries) == 1 && entries[0].IsDir() {
			srcDir = filepath.Join(tmpExtract, entries[0].Name())
		}
		os.Rename(srcDir, ffmpegDir)
		os.RemoveAll(tmpExtract)

		if runtime.GOOS != "windows" {
			os.Chmod(filepath.Join(ffmpegDir, "ffmpeg"), 0755)
			os.Chmod(filepath.Join(ffmpegDir, "ffprobe"), 0755)
		}
	}

	return nil
}

func getAlistUrl() (string, bool, error) {
	os := runtime.GOOS
	arch := runtime.GOARCH

	switch os {
	case "windows":
		return AlistUrlWindows, false, nil
	case "linux":
		if arch == "arm64" {
			return AlistUrlLinuxArm, true, nil
		}
		return AlistUrlLinux, true, nil
	case "darwin":
		if arch == "arm64" {
			return AlistUrlDarwinArm, true, nil
		}
		return AlistUrlDarwin, true, nil
	default:
		return "", false, fmt.Errorf("unsupported OS: %s", os)
	}
}

func getQBUrl() (string, error) {
	os := runtime.GOOS
	arch := runtime.GOARCH

	switch os {
	case "windows":
		// Portable zip not consistently available for newer versions.
		// Fallback to manual install for now to avoid Startup Failure.
		return "", fmt.Errorf("manual_install_required")
	case "linux":
		if arch == "arm64" {
			return QBUrlLinuxArm64, nil
		}
		return QBUrlLinuxAmd64, nil
	case "darwin":
		return "", fmt.Errorf("auto-download for macOS qbittorrent not supported yet")
	default:
		return "", fmt.Errorf("unsupported OS: %s", os)
	}
}

func getJellyfinUrl() (string, error) {
	switch runtime.GOOS {
	case "windows":
		return JellyfinUrlWindows, nil
	case "linux":
		return JellyfinUrlLinux, nil
	case "darwin":
		return JellyfinUrlMac, nil
	default:
		return "", fmt.Errorf("unsupported OS")
	}
}

func getFFmpegUrl() (string, error) {
	switch runtime.GOOS {
	case "windows":
		return FFmpegUrlWindows, nil
	case "linux":
		return FFmpegUrlLinux, nil
	case "darwin":
		return FFmpegUrlMac, nil
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

	// Use custom client to skip verify for mirror
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

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
			os.MkdirAll(fpath, os.ModePerm)
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

		_, err = io.Copy(outFile, rc)

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
		file.Seek(0, 0)
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
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
	return nil
}

func untarSystem(src, dest string) error {
	os.MkdirAll(dest, 0755)
	cmd := exec.Command("tar", "-xf", src, "-C", dest)
	// tar usually handles auto detection of compression (z, J, etc) on modern versions
	return cmd.Run()
}
