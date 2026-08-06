package pathutil

import (
	"path/filepath"
	"strings"
)

// Equal compares filesystem paths using the current platform's case rules.
func Equal(a, b string) bool {
	cleanA := filepath.Clean(strings.TrimSpace(a))
	cleanB := filepath.Clean(strings.TrimSpace(b))
	return equalClean(cleanA, cleanB)
}
