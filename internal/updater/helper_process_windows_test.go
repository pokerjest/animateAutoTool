//go:build windows

package updater

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPowerShellScriptCommandPreservesPathsWithSpaces(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Animate Tool Update Test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create script directory: %v", err)
	}
	scriptPath := filepath.Join(dir, "echo args.ps1")
	outputPath := filepath.Join(dir, "received args.txt")
	script := `param([string]$OutputPath, [string]$Value)
Set-Content -LiteralPath $OutputPath -Value $Value -Encoding ASCII
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatalf("write PowerShell script: %v", err)
	}

	cmd := powershellScriptCommand(scriptPath, "-OutputPath", outputPath, "-Value", "value with spaces")
	configureUpdateHelper(cmd)
	if err := cmd.Run(); err != nil {
		t.Fatalf("run PowerShell helper: %v", err)
	}
	data, err := os.ReadFile(outputPath) //nolint:gosec // outputPath is created under t.TempDir().
	if err != nil {
		t.Fatalf("read PowerShell output: %v", err)
	}
	if strings.TrimSpace(string(data)) != "value with spaces" {
		t.Fatalf("PowerShell argument was corrupted: %q", string(data))
	}
}
