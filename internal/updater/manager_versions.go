package updater

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/config"
	appversion "github.com/pokerjest/animateAutoTool/internal/version"
)

func currentVersion() string {
	v := strings.TrimSpace(appversion.AppVersion)
	if isBuildVersionUnset(v) {
		if fileVersion, err := readVersionFile(); err == nil && fileVersion != "" {
			v = fileVersion
		}
	}
	if v == "" {
		v = versionZero
	}
	return normalizeVersion(v)
}

func isBuildVersionUnset(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return v == "" || v == "dev" || v == "development" || v == "unknown"
}

func readVersionFile() (string, error) {
	candidates := []string{
		filepath.Join(config.RootDir(), "VERSION"),
		filepath.Join(filepath.Dir(config.ConfigFilePath()), "VERSION"),
	}

	for _, path := range candidates {
		content, err := os.ReadFile(path) //nolint:gosec
		if err != nil {
			continue
		}
		v := strings.TrimSpace(string(content))
		if v != "" {
			return v, nil
		}
	}

	return "", errors.New("VERSION file not found")
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return versionZero
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

type semVer struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
	Valid      bool
}

func compareVersions(a, b string) int {
	av := parseSemVer(a)
	bv := parseSemVer(b)
	if !av.Valid || !bv.Valid {
		ap := parseVersionParts(a)
		bp := parseVersionParts(b)
		for i := 0; i < maxVersionParts; i++ {
			if ap[i] < bp[i] {
				return -1
			}
			if ap[i] > bp[i] {
				return 1
			}
		}
		return 0
	}

	if av.Major != bv.Major {
		if av.Major < bv.Major {
			return -1
		}
		return 1
	}
	if av.Minor != bv.Minor {
		if av.Minor < bv.Minor {
			return -1
		}
		return 1
	}
	if av.Patch != bv.Patch {
		if av.Patch < bv.Patch {
			return -1
		}
		return 1
	}
	return comparePrerelease(av.Prerelease, bv.Prerelease)
}

func parseVersionParts(v string) [maxVersionParts]int {
	v = normalizeVersion(v)
	v = strings.TrimPrefix(v, "v")
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		v = v[:idx]
	}
	if idx := strings.IndexByte(v, '+'); idx >= 0 {
		v = v[:idx]
	}

	parts := strings.Split(v, ".")
	var out [maxVersionParts]int
	for i := 0; i < maxVersionParts && i < len(parts); i++ {
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil || n < 0 {
			continue
		}
		out[i] = n
	}
	return out
}

func parseSemVer(v string) semVer {
	v = normalizeVersion(v)
	v = strings.TrimPrefix(v, "v")
	if idx := strings.IndexByte(v, '+'); idx >= 0 {
		v = v[:idx]
	}

	pr := ""
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		pr = v[idx+1:]
		v = v[:idx]
	}

	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return semVer{}
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	patch, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil || major < 0 || minor < 0 || patch < 0 {
		return semVer{}
	}

	return semVer{Major: major, Minor: minor, Patch: patch, Prerelease: pr, Valid: true}
}

func comparePrerelease(a, b string) int {
	if a == "" && b == "" {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}

	ai := strings.Split(a, ".")
	bi := strings.Split(b, ".")
	maxLen := len(ai)
	if len(bi) > maxLen {
		maxLen = len(bi)
	}

	for i := 0; i < maxLen; i++ {
		if i >= len(ai) {
			return -1
		}
		if i >= len(bi) {
			return 1
		}

		x := ai[i]
		y := bi[i]
		xn, xNum := parseNumericIdentifier(x)
		yn, yNum := parseNumericIdentifier(y)

		switch {
		case xNum && yNum:
			if xn < yn {
				return -1
			}
			if xn > yn {
				return 1
			}
		case xNum && !yNum:
			return -1
		case !xNum && yNum:
			return 1
		default:
			if x < y {
				return -1
			}
			if x > y {
				return 1
			}
		}
	}

	return 0
}

func parseNumericIdentifier(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}
