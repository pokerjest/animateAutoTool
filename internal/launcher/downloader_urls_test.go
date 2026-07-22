package launcher

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAlistUrlAllPlatforms(t *testing.T) {
	t.Parallel()
	url, _, err := getAlistUrl()
	require.NoError(t, err, "current OS/arch should always have an Alist URL")
	require.NotEmpty(t, url)
}

func TestGetJellyfinUrlAllPlatforms(t *testing.T) {
	t.Parallel()
	url, err := getJellyfinUrl()
	require.NoError(t, err)
	require.NotEmpty(t, url)
	switch runtime.GOOS {
	case OSWindows:
		assert.Equal(t, JellyfinUrlWindows, url)
	case OSLinux:
		assert.Equal(t, JellyfinUrlLinux, url)
	case OSDarwin:
		assert.Equal(t, JellyfinUrlMac, url)
	}
}

func TestGetFFmpegUrlAllPlatforms(t *testing.T) {
	t.Parallel()
	url, err := getFFmpegUrl()
	require.NoError(t, err)
	require.NotEmpty(t, url)
	assert.True(t, strings.Contains(url, "jellyfin-ffmpeg"), "should be jellyfin's ffmpeg build")
}

func TestGetQBUrlMatchesPlatform(t *testing.T) {
	t.Parallel()
	url, err := getQBUrl()
	if runtime.GOOS == OSWindows || runtime.GOOS == OSDarwin {
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrQBManualInstallRequired))
		assert.Empty(t, url)
	} else {
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(url, GhProxy) || strings.Contains(url, "qbittorrent"))
	}
}

func TestJellyfinExecutableName(t *testing.T) {
	t.Parallel()
	got := jellyfinExecutableName()
	if runtime.GOOS == OSWindows {
		assert.Equal(t, jellyfinExecutable, got)
	} else {
		assert.Equal(t, jellyfinExecutableN, got)
	}
}

func TestFFmpegExecutableName(t *testing.T) {
	t.Parallel()
	got := ffmpegExecutableName()
	if runtime.GOOS == OSWindows {
		assert.Equal(t, ffmpegExecutable, got)
	} else {
		assert.Equal(t, ffmpegExecutableN, got)
	}
}

// TestUnzipExtractsFiles writes a small in-memory zip to disk, unzips it,
// and verifies file contents — exercises the happy path including the
// path-traversal guard, directory creation and limited copy.
func TestUnzipExtractsFiles(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "sample.zip")

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	files := map[string]string{
		"top.txt":           "top-content",
		"sub/inner.txt":     "inner-content",
		"sub/nested/dx.txt": "deep-content",
	}
	for name, content := range files {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, os.WriteFile(zipPath, buf.Bytes(), 0o600))

	dest := filepath.Join(tmp, "out")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	require.NoError(t, unzip(zipPath, dest))

	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(dest, name))
		require.NoError(t, err, "missing extracted file: %s", name)
		assert.Equal(t, want, string(got))
	}
}

// TestUnzipRejectsPathTraversal ensures entries that escape the
// destination directory are silently skipped, not written outside.
func TestUnzipRejectsPathTraversal(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "evil.zip")

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	w, err := zw.Create("../escape.txt")
	require.NoError(t, err)
	_, err = w.Write([]byte("pwn"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, os.WriteFile(zipPath, buf.Bytes(), 0o600))

	dest := filepath.Join(tmp, "out")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	require.NoError(t, unzip(zipPath, dest))

	_, err = os.Stat(filepath.Join(tmp, "escape.txt"))
	assert.True(t, os.IsNotExist(err), "path-traversal entry must not be written")
}

// TestUntarExtractsTarGz writes a small tar.gz to disk, extracts it,
// and verifies contents — exercises gzip detection, regular files and dirs.
func TestUntarExtractsTarGz(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	archivePath := filepath.Join(tmp, "sample.tar.gz")

	buf := &bytes.Buffer{}
	gz := gzip.NewWriter(buf)
	tw := tar.NewWriter(gz)

	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "dir/",
		Typeflag: tar.TypeDir,
		Mode:     0o755,
	}))

	body := []byte("hello world")
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "dir/file.txt",
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     int64(len(body)),
	}))
	_, err := tw.Write(body)
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	require.NoError(t, os.WriteFile(archivePath, buf.Bytes(), 0o600))

	dest := filepath.Join(tmp, "out")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	require.NoError(t, untar(archivePath, dest))

	got, err := os.ReadFile(filepath.Join(dest, "dir/file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(got))
}

// TestUntarRejectsPathTraversal verifies that tar entries which escape
// the destination directory are silently skipped.
func TestUntarRejectsPathTraversal(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	archivePath := filepath.Join(tmp, "evil.tar.gz")

	buf := &bytes.Buffer{}
	gz := gzip.NewWriter(buf)
	tw := tar.NewWriter(gz)
	body := []byte("pwn")
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "../escape.txt",
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     int64(len(body)),
	}))
	_, err := tw.Write(body)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	require.NoError(t, os.WriteFile(archivePath, buf.Bytes(), 0o600))

	dest := filepath.Join(tmp, "out")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	require.NoError(t, untar(archivePath, dest))

	_, err = os.Stat(filepath.Join(tmp, "escape.txt"))
	assert.True(t, os.IsNotExist(err), "path-traversal entry must not escape dest")
}
