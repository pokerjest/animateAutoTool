package parser

import (
	"path/filepath"
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
	// RSS titles commonly end with a container marker such as [MP4], while
	// qBittorrent may expose the materialized file name with a .mp4 suffix.
	releaseContainerSuffixPattern = regexp.MustCompile(`(?i)(?:\s*[\[(](?:mp4|mkv|avi|mov|webm|ts|m2ts|m4v|wmv|flv|rmvb)[\])]\s*|\.(?:mp4|mkv|avi|mov|webm|ts|m2ts|m4v|wmv|flv|rmvb)\s*)$`)
	explicitSeasonPatterns        = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^(?:s|season|series)[\s._-]*0*(\d+)$`),
		regexp.MustCompile(`(?i)^0*(\d+)(?:st|nd|rd|th)[\s._-]+season$`),
		regexp.MustCompile(`^第\s*([0-9一二三四五六七八九十百零]+)\s*[季期]$`),
	}
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
	// A feed often advertises the container as a final [MP4] tag, whereas
	// qBittorrent's task name can be the actual file name ending in .mp4.
	for {
		normalized := releaseContainerSuffixPattern.ReplaceAllString(title, "")
		if normalized == title {
			break
		}
		title = strings.TrimSpace(normalized)
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

// ExplicitSeasonNumberFromText returns a season encoded by a directory or
// filename segment. Unlike ParseSeason, it does not treat an unqualified
// trailing number as a season. This distinction matters when a downloader
// exposes only "Show - 03.mkv" as its task name while the actual content path
// contains "Season 02".
func ExplicitSeasonNumberFromText(value string) (string, bool) {
	value = strings.TrimSpace(strings.Trim(filepath.Base(strings.ReplaceAll(value, `\`, "/")), "/"))
	if value == "" {
		return "", false
	}
	lower := strings.ToLower(value)
	for _, pattern := range explicitSeasonPatterns {
		if match := pattern.FindStringSubmatch(value); len(match) > 1 {
			if season := normalizeChineseOrArabicNumber(match[1]); season != "" {
				return season, true
			}
		}
	}
	if lower == filenameEpisodeTypeSpecial || lower == "specials" || lower == "ova" || lower == "oad" || lower == "extra" || lower == "extras" ||
		lower == "特典" || lower == "特别篇" || lower == "番外" {
		return "0", true
	}
	return "", false
}

// EpisodeIdentityFromPath resolves an episode using both the materialized
// filename and its directory hierarchy. Jellyfin/Emby treat season folders as
// first-class evidence; qBittorrent commonly omits that evidence from the
// task name, so matching only the basename can lose an entire season.
func EpisodeIdentityFromPath(path string) (season, episode string) {
	season, episodes := EpisodeIdentitiesFromPath(path)
	if len(episodes) == 0 {
		return season, ""
	}
	return season, episodes[0]
}

// EpisodeIdentitiesFromPath returns every episode represented by a path. A
// bounded range is expanded so a single multi-episode torrent can satisfy each
// corresponding subscription row, just like Jellyfin's ending episode
// handling. Unbounded or absurd ranges are intentionally left as the first
// episode only.
func EpisodeIdentitiesFromPath(path string) (season string, episodes []string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	parsed := ParseFilename(strings.ReplaceAll(path, `\`, "/"))
	episode := ""
	if parsed.Episode > 0 {
		episode = NormalizeEpisodeNumber(strconv.Itoa(parsed.Episode))
	}
	if episode == "" && parsed.AbsoluteEpisode > 0 {
		episode = NormalizeEpisodeNumber(strconv.Itoa(parsed.AbsoluteEpisode))
	}
	if episode == "" {
		episode = EpisodeNumberFromTitle(filepath.Base(strings.ReplaceAll(path, `\`, "/")))
	}

	if parsed.HasExplicitSeason() {
		season = NormalizeSeasonNumber(strconv.Itoa(parsed.Season))
	}
	normalized := strings.ReplaceAll(path, `\`, "/")
	segments := strings.FieldsFunc(normalized, func(r rune) bool { return r == '/' })
	for i := len(segments) - 1; i >= 0 && season == ""; i-- {
		if candidate, ok := ExplicitSeasonNumberFromText(segments[i]); ok {
			season = candidate
		}
	}
	if season == "" {
		// Keep the historical default for ordinary anime releases, but return
		// an explicit zero for specials when their folder is named accordingly.
		season = "1"
	}
	if episode == "" {
		return season, nil
	}
	episodes = append(episodes, episode)
	end := parsed.EpisodeEnd
	if end > parsed.Episode && end-parsed.Episode <= 100 {
		for value := parsed.Episode + 1; value <= end; value++ {
			episodes = append(episodes, NormalizeEpisodeNumber(strconv.Itoa(value)))
		}
	}
	return season, episodes
}

func normalizeChineseOrArabicNumber(value string) string {
	if number, err := strconv.Atoi(value); err == nil && number >= 0 {
		return strconv.Itoa(number)
	}
	chinese := map[rune]int{'零': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	total := 0
	current := 0
	for _, r := range value {
		if r == '十' {
			if current == 0 {
				current = 1
			}
			total += current * 10
			current = 0
			continue
		}
		digit, ok := chinese[r]
		if !ok {
			return ""
		}
		current = current*10 + digit
	}
	total += current
	if total == 0 && value != "零" {
		return ""
	}
	return strconv.Itoa(total)
}
