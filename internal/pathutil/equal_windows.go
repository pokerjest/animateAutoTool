//go:build windows

package pathutil

import "strings"

func equalClean(a, b string) bool {
	return strings.EqualFold(a, b)
}
