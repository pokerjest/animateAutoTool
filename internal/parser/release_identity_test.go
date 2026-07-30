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
	if got := NormalizeReleaseTitle(second); got != "[ani] demo show - 01 [1080p]" {
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

func TestReleaseIdentityTreatsContainerTagAndExtensionAsEquivalent(t *testing.T) {
	rss := "[ANi] Demo Show - 01 [1080P][AAC][MP4]"
	qb := "[ANi] Demo Show - 01 [1080P][AAC].mp4"
	if got, want := NormalizeReleaseTitle(rss), NormalizeReleaseTitle(qb); got != want {
		t.Fatalf("container variants should normalize equally: rss=%q qb=%q", got, want)
	}
}

func TestEpisodeIdentitiesFromPathUsesSeasonDirectory(t *testing.T) {
	season, episodes := EpisodeIdentitiesFromPath(`E:\Anime\Example Show\Season 02\Example Show - 03.mkv`)
	if season != "2" || len(episodes) != 1 || episodes[0] != "3" {
		t.Fatalf("EpisodeIdentitiesFromPath = %q/%#v, want 2/[3]", season, episodes)
	}
}

func TestEpisodeIdentitiesFromPathExpandsBoundedMultiEpisodeRange(t *testing.T) {
	season, episodes := EpisodeIdentitiesFromPath(`/media/Example Show/Season 01/Example Show S01E03-E05.mkv`)
	if season != "1" {
		t.Fatalf("season = %q, want 1", season)
	}
	want := []string{"3", "4", "5"}
	if len(episodes) != len(want) {
		t.Fatalf("episodes = %#v, want %#v", episodes, want)
	}
	for i := range want {
		if episodes[i] != want[i] {
			t.Fatalf("episodes = %#v, want %#v", episodes, want)
		}
	}
}

func TestEpisodeIdentitiesFromPathDoesNotTreatResolutionAsRange(t *testing.T) {
	season, episodes := EpisodeIdentitiesFromPath(`/media/Example Show/Season 01/Example Show S01E14-1080p.mkv`)
	if season != "1" || len(episodes) != 1 || episodes[0] != "14" {
		t.Fatalf("resolution was treated as an episode range: %q/%#v", season, episodes)
	}
}
