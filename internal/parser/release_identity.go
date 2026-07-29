package parser

import (
	"regexp"
	"strconv"
	"strings"
)

// ReleaseIdentity helpers keep subscription de-duplication and live downloader
// progress matching consistent. Release feeds commonly append version labels
// such as [V2] without changing the actual season/episode.

var (
	releaseVersionTagPattern  = regexp.MustCompile(`(?i)(\[\s*v[0-9]+\s*\]|\(\s*v[0-9]+\s*\)|(^|[\s._-])v[0-9]+($|[\s._-]))`)
	releaseVersionOnlyPattern = regexp.MustCompile(`(?i)^v[0-9]+$`)
)

// NormalizeEpisodeNumber returns a stable representation for an episode
// number. For example, "01" and "1" both become "1", while decimal episodes
// such as "12.5" remain decimal values.
func NormalizeEpisodeNumber(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if number, err := strconv.ParseFloat(value, 64); err == nil && number >= 0 {
		return strconv.FormatFloat(number, 'f', -1, 64)
	}
	return strings.ToLower(value)
}

// NormalizeSeasonNumber returns a stable season number without the optional
// "S" prefix. An empty value means that no season could be determined.
func NormalizeSeasonNumber(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(value), "S"))
	if value == "" {
		return ""
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < 0 {
		return strings.ToLower(value)
	}
	return strconv.Itoa(number)
}

// EpisodeNumberFromTitle extracts the most useful episode number available in
// a release title. Explicit filename-style markers are preferred, followed by
// the Mikan title parser.
func EpisodeNumberFromTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	if parsed := ParseFilename(title); parsed.Episode > 0 {
		return NormalizeEpisodeNumber(strconv.Itoa(parsed.Episode))
	}
	if parsed := ParseTitle(title); parsed.EpisodeNum != "" {
		return NormalizeEpisodeNumber(parsed.EpisodeNum)
	}
	return ""
}

// SeasonNumberFromTitle extracts a season number from a release title. Most
// feeds omit it, so the filename parser's default season 1 is intentional.
func SeasonNumberFromTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	if parsed := ParseFilename(title); parsed.Season > 0 {
		return NormalizeSeasonNumber(strconv.Itoa(parsed.Season))
	}
	if parsed := ParseTitle(title); parsed.Season != "" {
		return NormalizeSeasonNumber(parsed.Season)
	}
	return ""
}

// ReleaseSubgroup returns the leading subtitle-group label when one exists.
func ReleaseSubgroup(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	if parsed := ParseTitle(title); strings.TrimSpace(parsed.SubGroup) != "" {
		if releaseVersionOnlyPattern.MatchString(strings.TrimSpace(parsed.SubGroup)) {
			return ""
		}
		return NormalizeReleaseTitle(parsed.SubGroup)
	}
	if parsed := ParseFilename(title); strings.TrimSpace(parsed.Group) != "" {
		if releaseVersionOnlyPattern.MatchString(strings.TrimSpace(parsed.Group)) {
			return ""
		}
		return NormalizeReleaseTitle(parsed.Group)
	}
	return ""
}

// NormalizeReleaseTitle removes version tags and normalizes whitespace for
// releases that do not expose a usable episode number.
func NormalizeReleaseTitle(title string) string {
	title = strings.ToLower(strings.TrimSpace(title))
	if title == "" {
		return ""
	}
	title = releaseVersionTagPattern.ReplaceAllString(title, " ")
	title = strings.ReplaceAll(title, "][", "] [")
	return strings.Join(strings.Fields(title), " ")
}

// EpisodeIdentityFromTitle returns the stable season/episode key components
// used by the subscription manager and qBittorrent progress enrichment.
func EpisodeIdentityFromTitle(title string) (season, episode string) {
	return SeasonNumberFromTitle(title), EpisodeNumberFromTitle(title)
}
