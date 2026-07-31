package appidentity

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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
	InstallRoot      string
	RunningLegacy    bool
	RunningCanonical bool
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
		InstallRoot:      launcherInstallRoot(executablePath),
		RunningLegacy:    strings.EqualFold(baseName, legacyName),
		RunningCanonical: strings.EqualFold(baseName, canonicalName),
		InsideAppBundle:  insideMacOSAppBundle(executablePath),
	}

	if migration.InsideAppBundle || !migration.RunningLegacy {
		return migration, nil
	}
	if err := copyExecutableWhenNewer(executablePath, migration.CanonicalPath, goos); err != nil {
		return migration, fmt.Errorf("create canonical launcher: %w", err)
	}
	return migration, nil
}

func (migration LauncherMigration) Complete() error {
	if migration.InsideAppBundle || (!migration.RunningLegacy && !migration.RunningCanonical) {
		return nil
	}

	var errs []error
	if err := migrateLauncherScripts(migration.InstallRoot); err != nil {
		errs = append(errs, err)
	}
	if migration.RunningCanonical && !samePath(migration.CanonicalPath, migration.LegacyPath) {
		if err := os.Remove(migration.LegacyPath); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("remove legacy launcher: %w", err))
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

func copyExecutableWhenNewer(sourcePath, destinationPath, goos string) error {
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}
	if destinationInfo, statErr := os.Stat(destinationPath); statErr == nil {
		if !sourceInfo.ModTime().After(destinationInfo.ModTime()) && destinationInfo.Size() > 0 {
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
