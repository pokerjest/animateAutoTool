package appidentity

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/safeio"
)

const (
	CanonicalBinaryName = "AnimateAutoTool"
	LegacyBinaryName    = "animate-server"
	goosWindows         = "windows"
)

type LauncherMigration struct {
	CanonicalPath    string
	LegacyPath       string
	SourcePath       string
	InstallRoot      string
	GOOS             string
	RunningLegacy    bool
	RunningCanonical bool
	RunningVersioned bool
	InsideAppBundle  bool
}

func BinaryName(goos string) string {
	if goos == goosWindows {
		return CanonicalBinaryName + ".exe"
	}
	return CanonicalBinaryName
}

func LegacyExecutableName(goos string) string {
	if goos == goosWindows {
		return LegacyBinaryName + ".exe"
	}
	return LegacyBinaryName
}

func ExecutableCandidates(goos string) []string {
	return []string{BinaryName(goos), LegacyExecutableName(goos)}
}

func CanonicalExecutablePath(currentPath, goos string) string {
	return filepath.Join(filepath.Dir(currentPath), BinaryName(goos))
}

func PrepareLocalLauncher() (LauncherMigration, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return LauncherMigration{}, err
	}
	return prepareLocalLauncher(executablePath, runtime.GOOS)
}

func prepareLocalLauncher(executablePath, goos string) (LauncherMigration, error) {
	executablePath, err := filepath.Abs(executablePath)
	if err != nil {
		return LauncherMigration{}, err
	}

	baseName := filepath.Base(executablePath)
	canonicalName := BinaryName(goos)
	legacyName := LegacyExecutableName(goos)
	migration := LauncherMigration{
		CanonicalPath:    filepath.Join(filepath.Dir(executablePath), canonicalName),
		LegacyPath:       filepath.Join(filepath.Dir(executablePath), legacyName),
		SourcePath:       executablePath,
		InstallRoot:      launcherInstallRoot(executablePath),
		GOOS:             goos,
		RunningLegacy:    strings.EqualFold(baseName, legacyName),
		RunningCanonical: strings.EqualFold(baseName, canonicalName),
		RunningVersioned: isVersionedLauncherName(baseName, goos),
		InsideAppBundle:  insideMacOSAppBundle(executablePath),
	}

	if migration.InsideAppBundle || (!migration.RunningLegacy && !migration.RunningVersioned) {
		return migration, nil
	}
	if err := copyExecutableIfMissing(executablePath, migration.CanonicalPath, goos); err != nil {
		return migration, fmt.Errorf("create canonical launcher: %w", err)
	}
	return migration, nil
}

func (migration LauncherMigration) NeedsCanonicalRelaunch() bool {
	return migration.GOOS == goosWindows &&
		!migration.InsideAppBundle &&
		(migration.RunningLegacy || migration.RunningVersioned) &&
		!samePath(migration.SourcePath, migration.CanonicalPath)
}

func (migration LauncherMigration) RelaunchCanonical() error {
	if !migration.NeedsCanonicalRelaunch() {
		return nil
	}

	cmd := exec.Command(migration.CanonicalPath, os.Args[1:]...) //nolint:gosec // canonical path is derived from the running executable directory.
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if workingDir, err := os.Getwd(); err == nil {
		cmd.Dir = workingDir
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start canonical launcher: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release canonical launcher process: %w", err)
	}
	return nil
}

func (migration LauncherMigration) Complete() error {
	if migration.InsideAppBundle || (!migration.RunningLegacy && !migration.RunningCanonical && !migration.RunningVersioned) {
		return nil
	}

	var errs []error
	if err := migrateLauncherScripts(migration.InstallRoot); err != nil {
		errs = append(errs, err)
	}
	if migration.RunningCanonical {
		if err := removeObsoleteLaunchers(filepath.Dir(migration.CanonicalPath), migration.GOOS, migration.CanonicalPath); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func launcherInstallRoot(executablePath string) string {
	executableDir := filepath.Dir(executablePath)
	if strings.EqualFold(filepath.Base(executableDir), "bin") {
		return filepath.Dir(executableDir)
	}
	return executableDir
}

func insideMacOSAppBundle(executablePath string) bool {
	normalized := filepath.ToSlash(filepath.Clean(executablePath))
	return strings.Contains(normalized, ".app/Contents/MacOS/")
}

func copyExecutableIfMissing(sourcePath, destinationPath, goos string) error {
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}
	if destinationInfo, statErr := os.Stat(destinationPath); statErr == nil {
		// Once a canonical launcher exists, a later double-click on an older
		// versioned executable must never replace it merely because the old
		// file has a newer download timestamp.
		if destinationInfo.Size() > 0 {
			return nil
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}

	source, err := os.Open(sourcePath) //nolint:gosec // sourcePath is the current executable.
	if err != nil {
		return err
	}
	defer safeio.Close(source)

	temp, err := os.CreateTemp(filepath.Dir(destinationPath), "."+filepath.Base(destinationPath)+".migrate-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		safeio.Close(temp)
		_ = os.Remove(tempPath)
	}()

	if _, err := io.Copy(temp, source); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempPath, sourceInfo.Mode().Perm()); err != nil {
		return err
	}
	if goos == goosWindows {
		if err := os.Remove(destinationPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(tempPath, destinationPath); err != nil {
		return err
	}
	return nil
}

func isVersionedLauncherName(name, goos string) bool {
	name = strings.ToLower(strings.TrimSpace(filepath.Base(name)))
	if goos == goosWindows {
		if !strings.HasSuffix(name, ".exe") {
			return false
		}
		name = strings.TrimSuffix(name, ".exe")
	}

	platformMarker := "_" + strings.ToLower(strings.TrimSpace(goos)) + "_"
	for _, prefix := range []string{
		strings.ToLower(CanonicalBinaryName) + "_v",
		strings.ToLower(LegacyBinaryName) + "_v",
	} {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		versionAndPlatform := strings.TrimPrefix(name, prefix)
		markerIndex := strings.LastIndex(versionAndPlatform, platformMarker)
		if markerIndex <= 0 {
			continue
		}
		arch := versionAndPlatform[markerIndex+len(platformMarker):]
		if arch != "" && !strings.ContainsAny(arch, `./\`) {
			return true
		}
	}
	return false
}

func removeObsoleteLaunchers(directory, goos, canonicalPath string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("list launcher directory: %w", err)
	}

	var errs []error
	legacyName := LegacyExecutableName(goos)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		if samePath(path, canonicalPath) {
			continue
		}
		if !strings.EqualFold(entry.Name(), legacyName) && !isVersionedLauncherName(entry.Name(), goos) {
			continue
		}
		if err := removeLauncherWithRetry(path, goos); err != nil {
			errs = append(errs, fmt.Errorf("remove obsolete launcher %s: %w", entry.Name(), err))
		}
	}
	return errors.Join(errs...)
}

func removeLauncherWithRetry(path, goos string) error {
	attempts := 1
	if goos == goosWindows {
		// A canonical child can begin before its versioned parent has fully
		// exited. Give Windows a brief window to release the old executable.
		attempts = 20
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := os.Remove(path); err == nil || os.IsNotExist(err) {
			return nil
		} else {
			lastErr = err
		}
		if attempt+1 < attempts {
			time.Sleep(50 * time.Millisecond)
		}
	}
	return lastErr
}

func migrateLauncherScripts(installRoot string) error {
	replacements := []struct {
		old string
		new string
	}{
		{old: LegacyBinaryName + ".exe", new: CanonicalBinaryName + ".exe"},
		{old: LegacyBinaryName, new: CanonicalBinaryName},
	}
	scriptPaths := []string{
		filepath.Join(installRoot, "scripts", "manage.sh"),
		filepath.Join(installRoot, "start.bat"),
		filepath.Join(installRoot, "run.bat"),
	}

	var errs []error
	for _, scriptPath := range scriptPaths {
		content, err := os.ReadFile(scriptPath) //nolint:gosec // paths are fixed files under the install root.
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("read %s: %w", scriptPath, err))
			continue
		}

		updated := string(content)
		for _, replacement := range replacements {
			updated = strings.ReplaceAll(updated, replacement.old, replacement.new)
		}
		if updated == string(content) {
			continue
		}

		info, err := os.Stat(scriptPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("stat %s: %w", scriptPath, err))
			continue
		}
		if err := os.WriteFile(scriptPath, []byte(updated), info.Mode().Perm()); err != nil {
			errs = append(errs, fmt.Errorf("update %s: %w", scriptPath, err))
		}
	}
	return errors.Join(errs...)
}

func samePath(left, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}
