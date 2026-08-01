//go:build !windows

package pathutil

import "testing"

func TestEqualRemainsCaseSensitiveOutsideWindows(t *testing.T) {
	if Equal("/media/Anime/Show.mkv", "/media/anime/Show.mkv") {
		t.Fatal("non-Windows paths must remain case-sensitive")
	}
}
