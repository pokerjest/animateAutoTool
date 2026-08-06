package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultAppRootUsesLocalAppDataOnWindows(t *testing.T) {
	preserveConfigState(t)

	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	appRootOverride = ""
	goosOverride = goosWindows
	executablePathFunc = func() (string, error) {
		return filepath.Join("C:", "Program Files", appName, "AnimateAutoTool.exe"), nil
	}

	got := defaultAppRoot()
	want := filepath.Join(localAppData, appName)
	if got != want {
		t.Fatalf("defaultAppRoot() = %q, want %q", got, want)
	}
}

func TestResolveAppPathsPreservesExistingWindowsPortableConfig(t *testing.T) {
	preserveConfigState(t)

	portableRoot := t.TempDir()
	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	appRootOverride = ""
	goosOverride = goosWindows
	executablePathFunc = func() (string, error) {
		return filepath.Join(portableRoot, "AnimateAutoTool.exe"), nil
	}
	if err := os.WriteFile(filepath.Join(portableRoot, defaultConfigFileName), []byte("server:\n  port: 8306\n"), 0o600); err != nil {
		t.Fatalf("write portable config: %v", err)
	}

	paths := resolveAppPaths("")
	if paths.RootDir != portableRoot {
		t.Fatalf("portable config root = %q, want %q", paths.RootDir, portableRoot)
	}
}
