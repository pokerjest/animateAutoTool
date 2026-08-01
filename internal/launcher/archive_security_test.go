package launcher

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeArchiveTargetRejectsTraversalAndWindowsPaths(t *testing.T) {
	t.Parallel()

	dest := t.TempDir()
	for _, name := range []string{
		"../outside",
		"..\\outside",
		"C:\\Windows\\system32",
		"C:/Windows/system32",
		"\\\\server\\share\\file",
		"/absolute/file",
	} {
		_, ok := safeArchiveTarget(dest, name)
		assert.Falsef(t, ok, "expected %q to be rejected", name)
	}

	target, ok := safeArchiveTarget(dest, "nested\\file.txt")
	require.True(t, ok)
	assert.Equal(t, filepath.Join(dest, "nested", "file.txt"), target)
}

func TestExtractZipRejectsArchiveSymlink(t *testing.T) {
	t.Parallel()

	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	header := &zip.FileHeader{Name: "link"}
	header.SetMode(os.ModeSymlink | 0o777)
	entry, err := writer.CreateHeader(header)
	require.NoError(t, err)
	_, err = entry.Write([]byte("target"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	require.NoError(t, err)
	err = extractZip(reader, t.TempDir(), 1024, 4096)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symbolic link")
}

func TestExtractZipRejectsExistingParentSymlink(t *testing.T) {
	if runtime.GOOS == OSWindows {
		t.Skip("creating symlinks on Windows may require Developer Mode or elevated privileges")
	}

	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "nested")))

	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create("nested/file.txt")
	require.NoError(t, err)
	_, err = entry.Write([]byte("blocked"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	require.NoError(t, err)
	require.Error(t, extractZip(reader, root, 1024, 4096))
	_, err = os.Stat(filepath.Join(outside, "file.txt"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestExtractZipEnforcesFileAndTotalLimits(t *testing.T) {
	t.Parallel()

	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, name := range []string{"one.bin", "two.bin"} {
		entry, err := writer.Create(name)
		require.NoError(t, err)
		_, err = entry.Write(bytes.Repeat([]byte("x"), 8))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	require.NoError(t, err)
	err = extractZip(reader, t.TempDir(), 7, 32)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file size limit")

	reader, err = zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	require.NoError(t, err)
	err = extractZip(reader, t.TempDir(), 8, 12)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "total extraction limit")
}

func TestExtractTarRejectsLinksAndEnforcesLimits(t *testing.T) {
	t.Parallel()

	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	require.NoError(t, writer.WriteHeader(&tar.Header{
		Name:     "link",
		Typeflag: tar.TypeSymlink,
		Linkname: "target",
		Mode:     0o777,
	}))
	require.NoError(t, writer.Close())

	err := extractTar(tar.NewReader(bytes.NewReader(archive.Bytes())), t.TempDir(), 1024, 4096)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is a link")

	archive.Reset()
	writer = tar.NewWriter(&archive)
	for _, name := range []string{"one.bin", "two.bin"} {
		require.NoError(t, writer.WriteHeader(&tar.Header{
			Name:     name,
			Typeflag: tar.TypeReg,
			Mode:     0o600,
			Size:     8,
		}))
		_, err = writer.Write(bytes.Repeat([]byte("x"), 8))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	err = extractTar(tar.NewReader(bytes.NewReader(archive.Bytes())), t.TempDir(), 8, 12)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "total extraction limit")
}
