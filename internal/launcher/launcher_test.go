package launcher

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestArchiveExtension(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://example.com/foo.tar.gz", ExtTarGz},
		{"https://example.com/foo.tar.xz", ExtTarXz},
		{"https://example.com/foo.zip", ExtZip},
		{"https://example.com/no-extension", ExtZip},
		{"https://example.com/foo.exe", ExtZip},
	}
	for _, tc := range cases {
		if got := archiveExtension(tc.url); got != tc.want {
			t.Errorf("archiveExtension(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestExtractedRootDirSingleSubdir(t *testing.T) {
	tmp := t.TempDir()
	inner := filepath.Join(tmp, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := extractedRootDir(tmp)
	if err != nil {
		t.Fatalf("extractedRootDir: %v", err)
	}
	if got != inner {
		t.Fatalf("expected %s, got %s", inner, got)
	}
}

func TestExtractedRootDirMultipleEntries(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "b.txt"), []byte("y"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := extractedRootDir(tmp)
	if err != nil {
		t.Fatalf("extractedRootDir: %v", err)
	}
	if got != tmp {
		t.Fatalf("expected %s, got %s", tmp, got)
	}
}

func TestQbExecutableName(t *testing.T) {
	got := qbExecutableName()
	if runtime.GOOS == OSWindows {
		if got != qbExecutableWindows {
			t.Errorf("expected %q on windows, got %q", qbExecutableWindows, got)
		}
	} else if got != qbExecutableNox {
		t.Errorf("expected %q on non-windows, got %q", qbExecutableNox, got)
	}
}

func TestManagedQBExecutablePath(t *testing.T) {
	binDir := "/opt/animate/bin"
	got := ManagedQBExecutablePath(binDir)
	if !strings.HasPrefix(got, binDir) {
		t.Errorf("expected path under %s, got %s", binDir, got)
	}
	if runtime.GOOS == OSWindows {
		if !strings.HasSuffix(got, "qbittorrent.exe") {
			t.Errorf("expected qbittorrent.exe suffix, got %s", got)
		}
	} else if !strings.HasSuffix(got, "qbittorrent-nox") {
		t.Errorf("expected qbittorrent-nox suffix, got %s", got)
	}
}

func TestHasManagedQBBinary(t *testing.T) {
	tmp := t.TempDir()
	if HasManagedQBBinary(tmp) {
		t.Fatal("expected false on empty bin dir")
	}

	target := ManagedQBExecutablePath(tmp)
	if err := os.WriteFile(target, []byte("stub"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	if !HasManagedQBBinary(tmp) {
		t.Fatal("expected true after creating stub binary")
	}
}

func TestQBPasswordPBKDF2Format(t *testing.T) {
	out, err := qBPasswordPBKDF2("hunter2")
	if err != nil {
		t.Fatalf("qBPasswordPBKDF2: %v", err)
	}
	// Format expected by qBittorrent: @ByteArray(<base64 salt>:<base64 hash>)
	if !strings.HasPrefix(out, "@ByteArray(") || !strings.HasSuffix(out, ")") {
		t.Fatalf("unexpected wrapper, got %s", out)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(out, "@ByteArray("), ")")
	parts := strings.Split(inner, ":")
	if len(parts) != 2 {
		t.Fatalf("expected salt:hash, got %s", inner)
	}
	if parts[0] == "" || parts[1] == "" {
		t.Fatalf("empty salt or hash: %s", inner)
	}

	// Same password with a fresh random salt should produce a different hash.
	out2, err := qBPasswordPBKDF2("hunter2")
	if err != nil {
		t.Fatalf("qBPasswordPBKDF2: %v", err)
	}
	if out == out2 {
		t.Fatal("expected different output across calls (fresh random salt)")
	}
}

func TestManagedQBConfigNeedsMigration(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "empty content does not need migration", content: "", want: false},
		{name: "config without managed marker untouched", content: "[Preferences]\nWebUI\\Username=admin\n", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := managedQBConfigNeedsMigration(tc.content); got != tc.want {
				t.Errorf("managedQBConfigNeedsMigration() = %v, want %v", got, tc.want)
			}
		})
	}
}
