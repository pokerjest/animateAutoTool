package appidentity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecutableNames(t *testing.T) {
	t.Parallel()

	if got := BinaryName("linux"); got != "AnimateAutoTool" {
		t.Fatalf("unexpected Unix binary name: %q", got)
	}
	if got := BinaryName("windows"); got != "AnimateAutoTool.exe" {
		t.Fatalf("unexpected Windows binary name: %q", got)
	}
	if got := ExecutableCandidates("windows"); len(got) != 2 || got[1] != "animate-server.exe" {
		t.Fatalf("unexpected Windows candidates: %#v", got)
	}
}

func TestVersionedLauncherNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		goos string
		want bool
	}{
		{name: "AnimateAutoTool_v1.0.0-beta.10_windows_amd64.exe", goos: "windows", want: true},
		{name: "animate-server_v0.7.0_windows_amd64.exe", goos: "windows", want: true},
		{name: "AnimateAutoTool_v1.0.0-beta.10_linux_arm64", goos: "linux", want: true},
		{name: "AnimateAutoTool.exe", goos: "windows", want: false},
		{name: "AnimateAutoTool_v1.0.0-beta.10_windows_amd64.zip", goos: "windows", want: false},
		{name: "AnimateAutoTool_v1.0.0-beta.10_linux_amd64", goos: "windows", want: false},
		{name: "AnimateAutoTool_v1.0.0-beta.10_linux_amd64.tar.gz", goos: "linux", want: false},
		{name: "AnimateAutoTool_v1.0.0-beta.10_darwin_arm64.dmg", goos: "darwin", want: false},
	}
	for _, test := range tests {
		if got := isVersionedLauncherName(test.name, test.goos); got != test.want {
			t.Errorf("isVersionedLauncherName(%q, %q) = %v, want %v", test.name, test.goos, got, test.want)
		}
	}
}

func TestPrepareLegacyLauncherCopiesCanonicalExecutable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(binDir, LegacyBinaryName)
	if err := os.WriteFile(legacyPath, []byte("new-version"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(legacyPath, 0o755); err != nil {
		t.Fatal(err)
	}

	migration, err := prepareLocalLauncher(legacyPath, "linux")
	if err != nil {
		t.Fatalf("prepareLocalLauncher: %v", err)
	}
	if !migration.RunningLegacy || migration.InstallRoot != root {
		t.Fatalf("unexpected migration: %#v", migration)
	}
	content, err := os.ReadFile(filepath.Clean(filepath.Join(binDir, CanonicalBinaryName))) //nolint:gosec // path is under t.TempDir().
	if err != nil {
		t.Fatalf("read canonical executable: %v", err)
	}
	if string(content) != "new-version" {
		t.Fatalf("unexpected canonical content: %q", content)
	}
}

func TestPrepareLegacyLauncherDoesNotOverwriteNewerCanonical(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	legacyPath := filepath.Join(dir, LegacyBinaryName)
	canonicalPath := filepath.Join(dir, CanonicalBinaryName)
	if err := os.WriteFile(legacyPath, []byte("old-version"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonicalPath, []byte("new-version"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(legacyPath, now.Add(-time.Minute), now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(canonicalPath, now, now); err != nil {
		t.Fatal(err)
	}

	if _, err := prepareLocalLauncher(legacyPath, "linux"); err != nil {
		t.Fatalf("prepareLocalLauncher: %v", err)
	}
	content, err := os.ReadFile(filepath.Clean(canonicalPath)) //nolint:gosec // path is under t.TempDir().
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new-version" {
		t.Fatalf("newer canonical executable was overwritten: %q", content)
	}
}

func TestPrepareVersionedWindowsLauncherCreatesCanonicalExecutable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	versionedPath := filepath.Join(dir, "animate-server_v0.7.0_windows_amd64.exe")
	if err := os.WriteFile(versionedPath, []byte("versioned-binary"), 0o600); err != nil {
		t.Fatal(err)
	}

	migration, err := prepareLocalLauncher(versionedPath, "windows")
	if err != nil {
		t.Fatalf("prepare versioned launcher: %v", err)
	}
	if !migration.RunningVersioned || !migration.NeedsCanonicalRelaunch() {
		t.Fatalf("versioned launcher was not recognized: %#v", migration)
	}
	content, err := os.ReadFile(filepath.Join(dir, "AnimateAutoTool.exe")) //nolint:gosec // path is under t.TempDir().
	if err != nil {
		t.Fatalf("read canonical launcher: %v", err)
	}
	if string(content) != "versioned-binary" {
		t.Fatalf("unexpected canonical launcher content: %q", content)
	}
}

func TestPrepareOldVersionedLauncherDoesNotOverwriteCanonicalExecutable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	versionedPath := filepath.Join(dir, "animate-server_v0.7.0_windows_amd64.exe")
	canonicalPath := filepath.Join(dir, "AnimateAutoTool.exe")
	if err := os.WriteFile(versionedPath, []byte("old-version"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonicalPath, []byte("current-version"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(versionedPath, now.Add(time.Minute), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	if _, err := prepareLocalLauncher(versionedPath, "windows"); err != nil {
		t.Fatalf("prepare old versioned launcher: %v", err)
	}
	content, err := os.ReadFile(canonicalPath) //nolint:gosec // path is under t.TempDir().
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "current-version" {
		t.Fatalf("canonical executable was overwritten: %q", content)
	}
}

func TestCompleteMigrationUpdatesScriptsAndCleansLegacyLauncher(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	scriptsDir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	canonicalPath := filepath.Join(binDir, CanonicalBinaryName)
	legacyPath := filepath.Join(binDir, LegacyBinaryName)
	if err := os.WriteFile(canonicalPath, []byte("canonical"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	versionedPath := filepath.Join(binDir, "AnimateAutoTool_v1.0.0-beta.10_linux_amd64")
	if err := os.WriteFile(versionedPath, []byte("versioned"), 0o600); err != nil {
		t.Fatal(err)
	}
	managePath := filepath.Join(scriptsDir, "manage.sh")
	if err := os.WriteFile(managePath, []byte("APP_NAME=\"animate-server\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	startPath := filepath.Join(root, "start.bat")
	if err := os.WriteFile(startPath, []byte("bin\\animate-server.exe\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	migration, err := prepareLocalLauncher(canonicalPath, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Complete(); err != nil {
		t.Fatalf("complete migration: %v", err)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy launcher still exists: %v", err)
	}
	if _, err := os.Stat(versionedPath); !os.IsNotExist(err) {
		t.Fatalf("versioned launcher still exists: %v", err)
	}
	for _, scriptPath := range []string{managePath, startPath} {
		content, readErr := os.ReadFile(filepath.Clean(scriptPath)) //nolint:gosec // paths are under t.TempDir().
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(string(content), LegacyBinaryName) {
			t.Fatalf("legacy launcher reference remains in %s: %q", scriptPath, content)
		}
		if !strings.Contains(string(content), CanonicalBinaryName) {
			t.Fatalf("canonical launcher reference missing in %s: %q", scriptPath, content)
		}
	}
}
