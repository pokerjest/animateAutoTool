package parser

import "testing"

func TestReleaseIdentityNormalizesVersionTagsAndEpisodeNumbers(t *testing.T) {
	first := "[ANi] Demo Show - 01 [1080P][MP4]"
	second := "[ANi] Demo Show - 01 [1080P][V2][MP4]"

	if got := EpisodeNumberFromTitle(first); got != "1" {
		t.Fatalf("EpisodeNumberFromTitle(first) = %q, want 1", got)
	}
	if got := EpisodeNumberFromTitle(second); got != "1" {
		t.Fatalf("EpisodeNumberFromTitle(second) = %q, want 1", got)
	}
	if got := NormalizeReleaseTitle(second); got != "[ani] demo show - 01 [1080p] [mp4]" {
		t.Fatalf("NormalizeReleaseTitle(second) = %q", got)
	}
	if got := ReleaseSubgroup(second); got != "ani" {
		t.Fatalf("ReleaseSubgroup(second) = %q, want ani", got)
	}
	season, episode := EpisodeIdentityFromTitle(second)
	if season != "1" || episode != "1" {
		t.Fatalf("EpisodeIdentityFromTitle(second) = %q/%q, want 1/1", season, episode)
	}
}
