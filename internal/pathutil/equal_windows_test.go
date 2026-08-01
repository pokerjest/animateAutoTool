//go:build windows

package pathutil

import "testing"

func TestEqualUsesWindowsCaseAndSeparatorRules(t *testing.T) {
	if !Equal(`C:\Media\Anime\Show.mkv`, `c:/media/anime/SHOW.mkv`) {
		t.Fatal("Windows paths should compare case-insensitively after cleaning")
	}
}
