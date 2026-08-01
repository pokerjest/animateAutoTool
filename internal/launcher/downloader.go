package launcher

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
	"github.com/ulikunitz/xz"
)

const (
	// Managed binaries are pinned to official release assets. Do not replace
	// these with proxy or latest-download URLs: checksum verification depends
	// on the exact asset being installed.
	GhProxy = "https://gh-proxy.com/"

	AlistUrlWindows   = "https://github.com/AlistGo/alist/releases/download/v3.62.0/alist-windows-amd64.zip"
	AlistUrlLinux     = "https://github.com/AlistGo/alist/releases/download/v3.62.0/alist-linux-amd64.tar.gz"
	AlistUrlLinuxArm  = "https://github.com/AlistGo/alist/releases/download/v3.62.0/alist-linux-arm64.tar.gz"
	AlistUrlDarwin    = "https://github.com/AlistGo/alist/releases/download/v3.62.0/alist-darwin-amd64.tar.gz"
	AlistUrlDarwinArm = "https://github.com/AlistGo/alist/releases/download/v3.62.0/alist-darwin-arm64.tar.gz"

	// qBittorrent (Enhanced Edition / Static)
	QBUrlWindowsPortable = "https://github.com/c0re100/qBittorrent-Enhanced-Edition/releases/download/release-4.6.1.10/qt6_x64_portable.zip"
	QBUrlLinuxAmd64      = "https://github.com/c0re100/qBittorrent-Enhanced-Edition/releases/download/release-4.6.1.10/qbittorrent-enhanced-nox_x86_64-linux-musl_static.zip"
	QBUrlLinuxArm64      = "https://github.com/c0re100/qBittorrent-Enhanced-Edition/releases/download/release-4.6.1.10/qbittorrent-enhanced-nox_aarch64-linux-musl_static.zip"

	// Jellyfin (fixed asset names under the official stable repository)
	JellyfinUrlWindows = "https://repo.jellyfin.org/files/server/windows/latest-stable/amd64/jellyfin_10.11.11-amd64.zip"
	JellyfinUrlLinux   = "https://repo.jellyfin.org/files/server/linux/latest-stable/amd64/jellyfin_10.11.11-amd64.tar.gz"
	JellyfinUrlMac     = "https://repo.jellyfin.org/files/server/macos/latest-stable/amd64/jellyfin_10.11.11-amd64.tar.xz"

	// FFmpeg (Jellyfin 7.1.4-3 fixed assets)
	FFmpegUrlWindows    = "https://repo.jellyfin.org/files/ffmpeg/windows/latest-7.x/win64/jellyfin-ffmpeg_7.1.4-3_portable_win64-clang-gpl.zip"
	FFmpegUrlLinuxAmd64 = "https://repo.jellyfin.org/files/ffmpeg/linux/latest-7.x/amd64/jellyfin-ffmpeg_7.1.4-3_portable_linux64-gpl.tar.xz"
	FFmpegUrlLinuxArm64 = "https://repo.jellyfin.org/files/ffmpeg/linux/latest-7.x/arm64/jellyfin-ffmpeg_7.1.4-3_portable_linuxarm64-gpl.tar.xz"
	FFmpegUrlMacAmd64   = "https://repo.jellyfin.org/files/ffmpeg/macos/latest-7.x/x86_64/jellyfin-ffmpeg_7.1.4-3_portable_mac64-gpl.tar.xz"
	FFmpegUrlMacArm64   = "https://repo.jellyfin.org/files/ffmpeg/macos/latest-7.x/arm64/jellyfin-ffmpeg_7.1.4-3_portable_macarm64-gpl.tar.xz"

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

const (
	maxDownloadBytes      int64 = 512 * 1024 * 1024
	maxExtractedFileBytes int64 = 512 * 1024 * 1024
	maxExtractedTotal     int64 = 2 * 1024 * 1024 * 1024
	downloadTimeout             = 20 * time.Minute
)

type downloadSpec struct {
	URL      string
	SHA256   string
	MaxBytes int64
}

var managedDownloadSpecs = map[string]downloadSpec{
	AlistUrlWindows:     {URL: AlistUrlWindows, SHA256: "218121d76f8d2c82f8336707091c2bd0921c5a9b65c3484529c58edf83cc0b38", MaxBytes: 64 * 1024 * 1024},
	AlistUrlLinux:       {URL: AlistUrlLinux, SHA256: "2ce6b4eccdda166eff5c575c956e64d983b1b7ac68b694dc7c159971267ca3c7", MaxBytes: 64 * 1024 * 1024},
	AlistUrlLinuxArm:    {URL: AlistUrlLinuxArm, SHA256: "7dc3a8d7cedb5d4595b1017baa98ccbbcfddb81dca97450040106990ada81618", MaxBytes: 64 * 1024 * 1024},
	AlistUrlDarwin:      {URL: AlistUrlDarwin, SHA256: "88ca5514c420b513b88e05accd27fd47ec4e7923467fe4dfdfa06d0f8ddfa7e4", MaxBytes: 64 * 1024 * 1024},
	AlistUrlDarwinArm:   {URL: AlistUrlDarwinArm, SHA256: "b26038b0579f3d8a808d3ae891e9c07ca6ecdc758b20fc960e366f9e87c36550", MaxBytes: 64 * 1024 * 1024},
	QBUrlLinuxAmd64:     {URL: QBUrlLinuxAmd64, SHA256: "b8b50a97ae62a57f710309bf1f90dd8a393a8f5a234b32e66cb3b51d81479fb5", MaxBytes: 128 * 1024 * 1024},
	QBUrlLinuxArm64:     {URL: QBUrlLinuxArm64, SHA256: "a04e112e2ded6c46452ac93d132f6b60dff9e40e8e8ffd382112c11cf15fb3bf", MaxBytes: 128 * 1024 * 1024},
	JellyfinUrlWindows:  {URL: JellyfinUrlWindows, SHA256: "3b5d3e904415ac6bd2c0ee9fe93235b4145d41c4c85ae782d244ac9767f77374", MaxBytes: 300 * 1024 * 1024},
	JellyfinUrlLinux:    {URL: JellyfinUrlLinux, SHA256: "9f7f194a7e37777cfde0d107c088fc47e81c7904440046ac0ceb7a289546cf79", MaxBytes: 300 * 1024 * 1024},
	JellyfinUrlMac:      {URL: JellyfinUrlMac, SHA256: "4289c359560d563a5725ed10ee68bd601ff2abe47861380f438bce3bd3272e43", MaxBytes: 300 * 1024 * 1024},
	FFmpegUrlWindows:    {URL: FFmpegUrlWindows, SHA256: "113adeb702683c38be40a65d859f8ef7ffb07bae9df16dfdfb6c3df5ac3d95ef3c", MaxBytes: 128 * 1024 * 1024},
	FFmpegUrlLinuxAmd64: {URL: FFmpegUrlLinuxAmd64, SHA256: "cab9ff40a47e4232d231e4eb7e4e85fabfeec56c6905266bc94291fc0881f83f", MaxBytes: 128 * 1024 * 1024},
	FFmpegUrlLinuxArm64: {URL: FFmpegUrlLinuxArm64, SHA256: "77e4b5d044ab73e1f26c9aadaa5d6014d1782500bf2c29afb3ab81f5bea98b1f", MaxBytes: 128 * 1024 * 1024},
	FFmpegUrlMacAmd64:   {URL: FFmpegUrlMacAmd64, SHA256: "943f78e94d2760d3925fc0d9cc15f8329b11dbcdae7b0fd0d225b64e5a1aae29", MaxBytes: 128 * 1024 * 1024},
	FFmpegUrlMacArm64:   {URL: FFmpegUrlMacArm64, SHA256: "99d689816a41075574928a0b3059101fd454fc58f465c99105a73b5c415ac86d", MaxBytes: 128 * 1024 * 1024},
}

func downloadSpecForURL(rawURL string) (downloadSpec, error) {
	spec, ok := managedDownloadSpecs[rawURL]
	if !ok || strings.TrimSpace(spec.SHA256) == "" {
		return downloadSpec{}, fmt.Errorf("no trusted checksum is configured for managed asset %q", rawURL)
	}
	if spec.MaxBytes <= 0 || spec.MaxBytes > maxDownloadBytes {
		return downloadSpec{}, fmt.Errorf("invalid download limit for managed asset %q", rawURL)
	}
	return spec, nil
}

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
	spec, err := downloadSpecForURL(url)
	if err != nil {
		return err
	}

	if err := downloadFile(spec, tmpFile); err != nil {
		return err
	}
	defer safeio.Remove(tmpFile)

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
	spec, err := downloadSpecForURL(url)
	if err != nil {
		return err
	}
	if err := downloadFile(spec, tmpZip); err != nil {
		return err
	}
	defer safeio.Remove(tmpZip)

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
	spec, err := downloadSpecForURL(url)
	if err != nil {
		return err
	}
	tmpFile := filepath.Join(m.BinDir, downloadPrefix+ext)
	if err := downloadFile(spec, tmpFile); err != nil {
		return err
	}
	defer safeio.Remove(tmpFile)

	tmpExtract := filepath.Join(m.BinDir, extractDirName)
	safeio.RemoveAll(tmpExtract)
	defer safeio.RemoveAll(tmpExtract)

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
		switch arch {
		case ArchAmd64:
			return FFmpegUrlLinuxAmd64, nil
		case ArchArm64:
			return FFmpegUrlLinuxArm64, nil
		default:
			return "", fmt.Errorf("unsupported architecture for Linux: %s", arch)
		}
	case OSDarwin:
		if arch == ArchArm64 {
			return FFmpegUrlMacArm64, nil
		}
		return FFmpegUrlMacAmd64, nil
	default:
		return "", fmt.Errorf("unsupported OS")
	}
}

func downloadFile(spec downloadSpec, filePath string) error {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			fmt.Printf("Retrying download (%d/3)...\n", attempt)
		}
		lastErr = downloadFileOnce(spec, filePath)
		if lastErr == nil {
			return nil
		}
		fmt.Printf("Download failed: %v\n", lastErr)
		safeio.Remove(filePath)
	}
	return fmt.Errorf("failed after 3 retries: %w", lastErr)
}

func downloadFileOnce(spec downloadSpec, filePath string) error {
	if strings.TrimSpace(spec.URL) == "" || strings.TrimSpace(spec.SHA256) == "" {
		return errors.New("managed download requires a URL and SHA-256 checksum")
	}
	maxBytes := spec.MaxBytes
	if maxBytes <= 0 || maxBytes > maxDownloadBytes {
		maxBytes = maxDownloadBytes
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, spec.URL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Do(request)
	if err != nil {
		return err
	}
	defer safeio.Close(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}
	if resp.ContentLength > maxBytes {
		return fmt.Errorf("download exceeds configured limit: %d > %d bytes", resp.ContentLength, maxBytes)
	}

	cleanPath := filepath.Clean(filePath)
	out, err := os.OpenFile(cleanPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // managed temporary download target.
	if err != nil {
		return err
	}
	defer safeio.Close(out)

	hash := sha256.New()
	limited := io.LimitReader(resp.Body, maxBytes+1)
	n, err := io.Copy(io.MultiWriter(out, hash), limited)
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("downloaded 0 bytes")
	}
	if n > maxBytes {
		return fmt.Errorf("download exceeds configured limit: more than %d bytes", maxBytes)
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(got, strings.TrimSpace(spec.SHA256)) {
		return fmt.Errorf("SHA-256 mismatch: got %s, want %s", got, spec.SHA256)
	}
	return out.Sync()
}

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer safeio.Close(r)

	return extractZip(&r.Reader, dest, maxExtractedFileBytes, maxExtractedTotal)
}

func extractZip(r *zip.Reader, dest string, maxFileBytes, maxTotalBytes int64) error {
	if r == nil {
		return errors.New("zip reader is nil")
	}
	if maxFileBytes <= 0 || maxTotalBytes <= 0 {
		return errors.New("invalid extraction limits")
	}
	if err := ensureSafeDirectory(dest, dest); err != nil {
		return err
	}

	var total int64
	for _, f := range r.File {
		target, ok := safeArchiveTarget(dest, f.Name)
		if !ok {
			return fmt.Errorf("archive entry %q has an unsafe path", f.Name)
		}
		mode := f.Mode()
		if mode&os.ModeSymlink != 0 {
			return fmt.Errorf("archive entry %q is a symbolic link", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := ensureSafeDirectory(target, dest); err != nil {
				return err
			}
			continue
		}
		if !mode.IsRegular() {
			return fmt.Errorf("archive entry %q has unsupported mode %s", f.Name, mode)
		}
		size := int64(f.UncompressedSize64)
		if size < 0 || size > maxFileBytes {
			return fmt.Errorf("archive entry %q exceeds the file size limit", f.Name)
		}
		if size > maxTotalBytes-total {
			return fmt.Errorf("archive exceeds the total extraction limit")
		}
		if err := ensureSafeDirectory(filepath.Dir(target), dest); err != nil {
			return err
		}
		if err := ensureSafeRegularTarget(target); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		perm := mode.Perm()
		if perm == 0 {
			perm = 0o600
		}
		outFile, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm) //nolint:gosec // target is validated under the managed extraction directory.
		if err != nil {
			safeio.Close(rc)
			return err
		}
		written, copyErr := io.Copy(outFile, io.LimitReader(rc, maxFileBytes+1))
		closeErr := outFile.Close()
		readerCloseErr := rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if readerCloseErr != nil {
			return readerCloseErr
		}
		if written != size {
			return fmt.Errorf("archive entry %q has unexpected extracted size: got %d, want %d", f.Name, written, size)
		}
		total += written
	}
	return nil
}
func untar(src, dest string) error {
	file, err := os.Open(src) //nolint:gosec // src is a managed archive downloaded into the app-controlled bin directory.
	if err != nil {
		return err
	}
	defer safeio.Close(file)

	var input io.Reader = file
	var closeInput io.Closer
	switch strings.ToLower(filepath.Ext(src)) {
	case ".xz":
		xzr, xzErr := xz.NewReader(file)
		if xzErr != nil {
			return xzErr
		}
		input = xzr
	case ".gz":
		gzr, gzErr := gzip.NewReader(file)
		if gzErr != nil {
			return gzErr
		}
		input = gzr
		closeInput = gzr
	}
	if closeInput != nil {
		defer safeio.Close(closeInput)
	}

	return extractTar(tar.NewReader(input), dest, maxExtractedFileBytes, maxExtractedTotal)
}

func extractTar(reader *tar.Reader, dest string, maxFileBytes, maxTotalBytes int64) error {
	if reader == nil {
		return errors.New("tar reader is nil")
	}
	if maxFileBytes <= 0 || maxTotalBytes <= 0 {
		return errors.New("invalid extraction limits")
	}
	if err := ensureSafeDirectory(dest, dest); err != nil {
		return err
	}

	var total int64
	for {
		header, nextErr := reader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return nextErr
		}
		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			return fmt.Errorf("archive entry %q is a link", header.Name)
		}
		target, ok := safeArchiveTarget(dest, header.Name)
		if !ok {
			return fmt.Errorf("archive entry %q has an unsafe path", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := ensureSafeDirectory(target, dest); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA: //nolint:staticcheck // TypeRegA keeps compatibility with legacy tar archives.
			if header.Size < 0 || header.Size > maxFileBytes {
				return fmt.Errorf("archive entry %q exceeds the file size limit", header.Name)
			}
			if header.Size > maxTotalBytes-total {
				return fmt.Errorf("archive exceeds the total extraction limit")
			}
			if err := ensureSafeDirectory(filepath.Dir(target), dest); err != nil {
				return err
			}
			if err := ensureSafeRegularTarget(target); err != nil {
				return err
			}
			perm := os.FileMode(header.Mode).Perm()
			if perm == 0 {
				perm = 0o600
			}
			output, openErr := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm) //nolint:gosec // target is validated under the managed extraction directory.
			if openErr != nil {
				return openErr
			}
			written, copyErr := io.Copy(output, io.LimitReader(reader, maxFileBytes+1))
			closeErr := output.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			if written != header.Size {
				return fmt.Errorf("archive entry %q has truncated data", header.Name)
			}
			total += written
		default:
			return fmt.Errorf("archive entry %q has unsupported type %d", header.Name, header.Typeflag)
		}
	}
	return nil
}
func safeArchiveTarget(dest, name string) (string, bool) {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if name == "" {
		return "", false
	}
	firstSegment := strings.SplitN(name, "/", 2)[0]
	convertedName := filepath.FromSlash(name)
	if strings.HasPrefix(name, "/") ||
		strings.Contains(firstSegment, ":") ||
		filepath.IsAbs(convertedName) ||
		filepath.VolumeName(convertedName) != "" {
		return "", false
	}
	cleanDest := filepath.Clean(dest)
	target := filepath.Clean(filepath.Join(cleanDest, convertedName))
	rel, err := filepath.Rel(cleanDest, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return target, true
}

func ensureSafeDirectory(target, root string) error {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("unsafe extraction directory %q", target)
	}

	rootInfo, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(root, 0o755); err != nil {
			return err
		}
		rootInfo, err = os.Lstat(root)
	}
	if err != nil {
		return err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return fmt.Errorf("unsafe extraction root %q", root)
	}
	if target == root {
		return nil
	}

	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			if mkdirErr := os.Mkdir(current, 0o755); mkdirErr != nil {
				return mkdirErr
			}
			info, statErr = os.Lstat(current)
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("unsafe extraction directory component %q", current)
		}
	}
	return nil
}

func ensureSafeRegularTarget(target string) error {
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("unsafe extraction file target %q", target)
	}
	return nil
}
