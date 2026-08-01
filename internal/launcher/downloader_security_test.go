package launcher

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ulikunitz/xz"
)

func checksumFor(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestDownloadFileOnceValidatesChecksum(t *testing.T) {
	body := []byte("trusted managed asset")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "asset.bin")
	if err := downloadFileOnce(downloadSpec{URL: server.URL, SHA256: checksumFor(body), MaxBytes: int64(len(body))}, target); err != nil {
		t.Fatalf("download valid asset: %v", err)
	}
	if data, err := os.ReadFile(target); err != nil || !bytes.Equal(data, body) { //nolint:gosec // target is inside t.TempDir.
		t.Fatalf("downloaded data = %q, err = %v", data, err)
	}

	if err := downloadFileOnce(downloadSpec{URL: server.URL, SHA256: checksumFor([]byte("different")), MaxBytes: int64(len(body))}, target); err == nil {
		t.Fatal("expected SHA-256 mismatch to fail")
	}
}

func TestDownloadFileOnceRejectsDeclaredAndStreamingSizeOverflow(t *testing.T) {
	t.Run("content length", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "9")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()
		err := downloadFileOnce(downloadSpec{URL: server.URL, SHA256: checksumFor([]byte("unused")), MaxBytes: 8}, filepath.Join(t.TempDir(), "asset.bin"))
		if err == nil {
			t.Fatal("expected declared content length overflow to fail")
		}
	})

	t.Run("streaming body", func(t *testing.T) {
		body := []byte("123456789")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			_, _ = w.Write(body)
		}))
		defer server.Close()
		err := downloadFileOnce(downloadSpec{URL: server.URL, SHA256: checksumFor(body), MaxBytes: 8}, filepath.Join(t.TempDir(), "asset.bin"))
		if err == nil {
			t.Fatal("expected streaming body overflow to fail")
		}
	})
}

func TestUntarExtractsTarXz(t *testing.T) {
	var archive bytes.Buffer
	xzWriter, err := xz.NewWriter(&archive)
	if err != nil {
		t.Fatalf("create xz writer: %v", err)
	}
	tarWriter := tar.NewWriter(xzWriter)
	body := []byte("tar xz content")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "nested/file.txt", Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tarWriter.Write(body); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := xzWriter.Close(); err != nil {
		t.Fatalf("close xz writer: %v", err)
	}

	root := t.TempDir()
	archivePath := filepath.Join(root, "sample.tar.xz")
	if err := os.WriteFile(archivePath, archive.Bytes(), 0o600); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	dest := filepath.Join(root, "out")
	if err := untar(archivePath, dest); err != nil {
		t.Fatalf("extract tar.xz: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "nested", "file.txt")) //nolint:gosec // dest is inside t.TempDir.
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if !bytes.Equal(data, body) {
		t.Fatalf("extracted data = %q, want %q", data, body)
	}
}
