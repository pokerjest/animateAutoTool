//go:build !windows

package pathutil

func equalClean(a, b string) bool {
	return a == b
}
