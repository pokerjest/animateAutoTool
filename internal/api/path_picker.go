package api

import (
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gin-gonic/gin"
)

var errPickerCancelled = errors.New("directory picker cancelled")

var pickDirectoryFunc = pickDirectoryNative

type pickDirectoryRequest struct {
	Title       string `form:"title" json:"title"`
	DefaultPath string `form:"default_path" json:"default_path"`
}

func PickDirectoryHandler(c *gin.Context) {
	if !requestIsDirectLoopback(c) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "目录选择窗口仅支持在本机通过 localhost 直接访问时使用",
		})
		return
	}

	var req pickDirectoryRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "选择目录"
	}

	selected, err := pickDirectoryFunc(title, strings.TrimSpace(req.DefaultPath))
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{
			"path": selected,
		})
	case errors.Is(err, errPickerCancelled):
		c.JSON(http.StatusOK, gin.H{
			"cancelled": true,
		})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": err.Error(),
		})
	}
}

func pickDirectoryNative(title, defaultPath string) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return pickDirectoryDarwin(title, defaultPath)
	case "windows":
		return pickDirectoryWindows(title, defaultPath)
	case "linux":
		return pickDirectoryLinux(title, defaultPath)
	default:
		return "", fmt.Errorf("当前系统(%s)暂不支持目录选择器", runtime.GOOS)
	}
}

func pickDirectoryDarwin(title, defaultPath string) (string, error) {
	prompt := escapeAppleScriptString(strings.TrimSpace(title))
	defaultPath = strings.TrimSpace(defaultPath)

	call := func(path string) (string, error) {
		args := []string{
			"-e",
			`set selectedFolder to choose folder with prompt "` + prompt + `"`,
		}
		if path != "" {
			args = []string{
				"-e",
				`set selectedFolder to choose folder with prompt "` + prompt + `" default location POSIX file "` + escapeAppleScriptString(path) + `"`,
			}
		}
		args = append(args, "-e", "POSIX path of selectedFolder")
		return runPickerCommand("osascript", args, 1)
	}

	if defaultPath == "" {
		return call("")
	}

	path, err := call(filepath.Clean(defaultPath))
	if err == nil || errors.Is(err, errPickerCancelled) {
		return path, err
	}
	return call("")
}

func pickDirectoryWindows(title, defaultPath string) (string, error) {
	prompt := escapePowerShellString(strings.TrimSpace(title))
	selected := escapePowerShellString(strings.TrimSpace(defaultPath))

	script := strings.Join([]string{
		"$ErrorActionPreference = 'Stop'",
		"Add-Type -AssemblyName System.Windows.Forms",
		"$dialog = New-Object System.Windows.Forms.FolderBrowserDialog",
		"$dialog.ShowNewFolderButton = $true",
		"$dialog.Description = '" + prompt + "'",
		"if ('" + selected + "' -ne '') { $dialog.SelectedPath = '" + selected + "' }",
		"$result = $dialog.ShowDialog()",
		"if ($result -eq [System.Windows.Forms.DialogResult]::OK) {",
		"  [Console]::OutputEncoding = [System.Text.Encoding]::UTF8",
		"  Write-Output $dialog.SelectedPath",
		"  exit 0",
		"}",
		"exit 2",
	}, "; ")

	commands := []string{"powershell", "pwsh"}
	for i, command := range commands {
		path, err := runPickerCommand(command, []string{"-NoProfile", "-NonInteractive", "-Command", script}, 2)
		if err == nil || errors.Is(err, errPickerCancelled) {
			return path, err
		}
		if errors.Is(err, exec.ErrNotFound) && i < len(commands)-1 {
			continue
		}
		return "", err
	}

	return "", fmt.Errorf("未找到可用的 PowerShell 运行时")
}

func pickDirectoryLinux(title, defaultPath string) (string, error) {
	defaultPath = strings.TrimSpace(defaultPath)

	zenityArgs := []string{
		"--file-selection",
		"--directory",
		"--title=" + strings.TrimSpace(title),
	}
	if defaultPath != "" {
		zenityArgs = append(zenityArgs, "--filename="+ensureTrailingPathSeparator(defaultPath))
	}

	path, err := runPickerCommand("zenity", zenityArgs, 1)
	if err == nil || errors.Is(err, errPickerCancelled) {
		return path, err
	}
	if !errors.Is(err, exec.ErrNotFound) {
		return "", err
	}

	kdialogArgs := []string{"--getexistingdirectory"}
	if defaultPath != "" {
		kdialogArgs = append(kdialogArgs, defaultPath)
	} else {
		kdialogArgs = append(kdialogArgs, "~")
	}
	kdialogArgs = append(kdialogArgs, "--title", strings.TrimSpace(title))

	path, err = runPickerCommand("kdialog", kdialogArgs, 1)
	if err == nil || errors.Is(err, errPickerCancelled) {
		return path, err
	}
	if errors.Is(err, exec.ErrNotFound) {
		return "", fmt.Errorf("未找到可用目录选择器，请安装 zenity 或 kdialog")
	}
	return "", err
}

func runPickerCommand(command string, args []string, cancelExitCodes ...int) (string, error) {
	output, err := executePickerCommand(command, args)
	if err == nil {
		path := normalizePickedDirectory(string(output))
		if path == "" {
			return "", fmt.Errorf("%s 未返回目录路径", command)
		}
		return path, nil
	}

	if errors.Is(err, exec.ErrNotFound) {
		return "", err
	}

	message := strings.TrimSpace(string(output))
	lower := strings.ToLower(message)

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		for _, code := range cancelExitCodes {
			if exitErr.ExitCode() == code {
				return "", errPickerCancelled
			}
		}
		if strings.Contains(lower, "cancel") || strings.Contains(lower, "canceled") {
			return "", errPickerCancelled
		}
	}

	if message == "" {
		return "", fmt.Errorf("%s 执行失败: %w", command, err)
	}
	return "", fmt.Errorf("%s 执行失败: %s", command, message)
}

func executePickerCommand(command string, args []string) ([]byte, error) {
	switch command {
	case "osascript":
		//nolint:gosec // command and args are from an internal whitelist and passed directly without shell expansion.
		return exec.Command("osascript", args...).CombinedOutput()
	case "powershell":
		//nolint:gosec // command and args are from an internal whitelist and passed directly without shell expansion.
		return exec.Command("powershell", args...).CombinedOutput()
	case "pwsh":
		//nolint:gosec // command and args are from an internal whitelist and passed directly without shell expansion.
		return exec.Command("pwsh", args...).CombinedOutput()
	case "zenity":
		//nolint:gosec // command and args are from an internal whitelist and passed directly without shell expansion.
		return exec.Command("zenity", args...).CombinedOutput()
	case "kdialog":
		//nolint:gosec // command and args are from an internal whitelist and passed directly without shell expansion.
		return exec.Command("kdialog", args...).CombinedOutput()
	default:
		return nil, fmt.Errorf("unsupported picker command: %s", command)
	}
}

func normalizePickedDirectory(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	return filepath.Clean(value)
}

func ensureTrailingPathSeparator(path string) string {
	if strings.HasSuffix(path, "/") || strings.HasSuffix(path, `\`) {
		return path
	}
	return path + string(filepath.Separator)
}

func escapeAppleScriptString(raw string) string {
	return strings.ReplaceAll(raw, `"`, `\"`)
}

func escapePowerShellString(raw string) string {
	return strings.ReplaceAll(raw, `'`, `''`)
}
