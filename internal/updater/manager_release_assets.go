package updater

import (
	"context"
	"crypto/sha256"
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
)

func pickAssetForCurrentPlatform(release *githubRelease) (*releaseAsset, error) {
	if release == nil {
		return nil, errors.New("release is nil")
	}

	exePath, err := os.Executable()
	if err != nil {
		return nil, err
	}
	candidates := platformAssetCandidates(runtime.GOOS, runtime.GOARCH, currentAppBundlePath(exePath) != "")
	if len(candidates) == 0 {
		return nil, fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	for _, suffix := range candidates {
		for i := range release.Assets {
			asset := &release.Assets[i]
			if strings.HasSuffix(strings.ToLower(asset.Name), strings.ToLower(suffix)) && strings.TrimSpace(asset.BrowserDownloadURL) != "" {
				return asset, nil
			}
		}
	}

	return nil, fmt.Errorf("asset candidates %q not found in release %s", strings.Join(candidates, ", "), release.TagName)
}

func platformAssetCandidates(goos, goarch string, inAppBundle bool) []string {
	switch goos {
	case "windows":
		return []string{fmt.Sprintf("_windows_%s.exe", goarch)}
	case goosDarwin:
		dmg := fmt.Sprintf("_darwin_%s.dmg", goarch)
		archive := fmt.Sprintf("_darwin_%s.tar.gz", goarch)
		if inAppBundle {
			return []string{dmg, archive}
		}
		return []string{archive, dmg}
	case goosLinux:
		return []string{fmt.Sprintf("_linux_%s.tar.gz", goarch)}
	default:
		return nil
	}
}

func fetchExpectedChecksum(release *githubRelease, targetAssetName string) (string, error) {
	candidates, err := pickChecksumCandidates(release, targetAssetName)
	if err != nil {
		return "", err
	}

	var failures []string
	for _, candidate := range candidates {
		text, err := downloadSmallTextAsset(candidate.Asset.BrowserDownloadURL)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s(download): %v", candidate.Asset.Name, err))
			continue
		}

		hash, err := parseChecksumTextWithPolicy(text, targetAssetName, candidate.AllowSingleHash)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s(parse): %v", candidate.Asset.Name, err))
			continue
		}
		return hash, nil
	}

	if len(failures) == 0 {
		return "", fmt.Errorf("checksum for %s not found", targetAssetName)
	}
	if len(failures) > 3 {
		failures = failures[:3]
	}
	return "", fmt.Errorf("checksum for %s not found (%s)", targetAssetName, strings.Join(failures, "; "))
}

type checksumCandidate struct {
	Asset           *releaseAsset
	AllowSingleHash bool
}

func pickChecksumCandidates(release *githubRelease, targetAssetName string) ([]checksumCandidate, error) {
	if release == nil {
		return nil, errors.New("release is nil")
	}
	targetAssetName = filepath.Base(strings.TrimSpace(targetAssetName))
	if targetAssetName == "" {
		return nil, errors.New("target asset name is empty")
	}

	targetLower := strings.ToLower(targetAssetName)
	preferred := map[string]struct{}{
		targetLower + ".sha256":     {},
		targetLower + ".sha256sum":  {},
		targetLower + ".sha256.txt": {},
	}

	var exact []checksumCandidate
	var generic []checksumCandidate
	seen := map[string]struct{}{}
	for i := range release.Assets {
		a := &release.Assets[i]
		name := strings.ToLower(strings.TrimSpace(a.Name))
		if name == "" || strings.TrimSpace(a.BrowserDownloadURL) == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		if _, ok := preferred[name]; ok {
			seen[name] = struct{}{}
			exact = append(exact, checksumCandidate{Asset: a, AllowSingleHash: true})
			continue
		}
		if strings.Contains(name, targetLower) && isChecksumLikeFile(name) {
			seen[name] = struct{}{}
			exact = append(exact, checksumCandidate{Asset: a, AllowSingleHash: true})
			continue
		}
		if isChecksumLikeFile(name) {
			seen[name] = struct{}{}
			generic = append(generic, checksumCandidate{Asset: a, AllowSingleHash: false})
			continue
		}
	}

	if len(exact) == 0 && len(generic) == 0 {
		return nil, fmt.Errorf("checksum asset for %s not found", targetAssetName)
	}
	return append(exact, generic...), nil
}

func isChecksumLikeFile(assetName string) bool {
	name := strings.ToLower(strings.TrimSpace(filepath.Base(assetName)))
	if name == "" {
		return false
	}
	if strings.Contains(name, "sha256") || strings.Contains(name, "checksum") {
		return true
	}
	return strings.HasSuffix(name, ".sha256") || strings.HasSuffix(name, ".sha256sum")
}

func parseChecksumText(text, assetName string) (string, error) {
	return parseChecksumTextWithPolicy(text, assetName, true)
}

func parseChecksumTextWithPolicy(text, assetName string, allowSingleHash bool) (string, error) {
	assetName = filepath.Base(strings.TrimSpace(assetName))
	if assetName == "" {
		return "", errors.New("asset name is empty")
	}

	var singleHash string
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 1 && isHexSHA256(fields[0]) {
			if singleHash == "" {
				singleHash = strings.ToLower(fields[0])
			}
			continue
		}
		if len(fields) < 2 || !isHexSHA256(fields[0]) {
			continue
		}

		nameField := fields[len(fields)-1]
		nameField = strings.TrimPrefix(nameField, "*")
		nameField = strings.TrimPrefix(nameField, "./")
		nameField = filepath.Base(nameField)
		if strings.EqualFold(nameField, assetName) {
			return strings.ToLower(fields[0]), nil
		}
	}

	if allowSingleHash && singleHash != "" {
		return singleHash, nil
	}
	return "", fmt.Errorf("checksum for %s not found", assetName)
}

func downloadSmallTextAsset(url string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", defaultUserAgent)

	resp, err := updaterHTTPClient(httpTimeout).Do(req)
	if err != nil {
		return "", err
	}
	defer safeio.Close(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("checksum download failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func isHexSHA256(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

func (m *Manager) downloadAsset(url, assetName string) (string, error) {
	assetName = filepath.Base(strings.TrimSpace(assetName))
	if assetName == "" {
		assetName = "update_artifact"
	}

	updateDir := filepath.Join(config.DataDir(), "updates")
	if err := os.MkdirAll(updateDir, 0755); err != nil {
		return "", err
	}

	targetPath := filepath.Join(updateDir, assetName)
	tempPath := targetPath + ".part"
	_ = os.Remove(tempPath)

	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", defaultUserAgent)

	resp, err := updaterHTTPClient(downloadTimeout).Do(req)
	if err != nil {
		return "", err
	}
	defer safeio.Close(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("download failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	file, err := os.Create(tempPath) //nolint:gosec
	if err != nil {
		return "", err
	}

	totalBytes := resp.ContentLength
	var written int64
	buffer := make([]byte, 32*1024)
	lastUpdate := time.Time{}
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			chunk := buffer[:n]
			wn, writeErr := file.Write(chunk)
			written += int64(wn)
			now := time.Now()
			if now.Sub(lastUpdate) >= 250*time.Millisecond || (totalBytes > 0 && written >= totalBytes) {
				m.updateProgress("下载更新包", "正在下载更新包...", written, totalBytes)
				lastUpdate = now
			}
			if writeErr != nil {
				_ = file.Close()
				_ = os.Remove(tempPath)
				return "", writeErr
			}
			if wn != len(chunk) {
				_ = file.Close()
				_ = os.Remove(tempPath)
				return "", io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = file.Close()
			_ = os.Remove(tempPath)
			return "", readErr
		}
	}
	m.updateProgress("下载更新包", "正在下载更新包...", written, totalBytes)
	if err := file.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", err
	}

	if err := os.Rename(tempPath, targetPath); err != nil {
		_ = os.Remove(tempPath)
		return "", err
	}

	return targetPath, nil
}

func verifyFileSHA256(path, expectedLowerHex string) error {
	expectedLowerHex = strings.ToLower(strings.TrimSpace(expectedLowerHex))
	if !isHexSHA256(expectedLowerHex) {
		return fmt.Errorf("invalid expected sha256: %q", expectedLowerHex)
	}

	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return err
	}
	defer safeio.Close(f)

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := fmt.Sprintf("%x", h.Sum(nil))
	if actual != expectedLowerHex {
		return fmt.Errorf("sha256 mismatch, expected %s got %s", expectedLowerHex, actual)
	}
	return nil
}
