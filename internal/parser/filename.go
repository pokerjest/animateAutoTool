package parser

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Filename parsing runs once per discovered video. Keep the deterministic
// patterns compiled at package initialization so large libraries do not spend
// most of their scan time repeatedly compiling identical regular expressions.
var (
	filenameGroupPattern          = regexp.MustCompile(`^\[(.*?)\]`)
	filenameResolutionPattern     = regexp.MustCompile(`(?i)\b(1080p|720p|2160p|4k|360p|480p|FHD|HD|1920x1080|1280x720|3840x2160)\b`)
	filenameVideoCodecPattern     = regexp.MustCompile(`(?i)\b(h264|h265|x264|x265|av1|hevc|vp9)\b`)
	filenameAudioCodecPattern     = regexp.MustCompile(`(?i)\b(flac|aac|aacx2|aacx4|aacx3|ac3|eac3|dts|dts-hd|truehd|opus|mp3)\b`)
	filenameBitDepthPattern       = regexp.MustCompile(`(?i)\b(10bit|8bit|hi10p|ma10p)\b`)
	filenameSourcePattern         = regexp.MustCompile(`(?i)\b(web-?rip|bd-?rip|web-?dl|bluray|dvd-?rip|hdtv)\b`)
	filenameVersionPattern        = regexp.MustCompile(`(?i)(?:^|\d|[\s._\-\[])(v\d+(?:\.\d+)?)(?:$|[\s._\-\]])`)
	filenameLanguagePattern       = regexp.MustCompile(`(?i)(?:^|[\s._\-\[])(chs(?:_cht)?|cht|eng|jpn|双语|简|繁)(?:$|[\s._\-\]])`)
	filenameOpeningPattern        = regexp.MustCompile(`(?i)(?:^|[\s._\-\[(])(?:ncop|op|opening)(?:\d+)?(?:$|[\s._\-\])])`)
	filenameEndingPattern         = regexp.MustCompile(`(?i)(?:^|[\s._\-\[(])(?:nced|ed|ending)(?:\d+)?(?:$|[\s._\-\])])`)
	filenameTrailerPattern        = regexp.MustCompile(`(?i)(?:^|[\s._\-\[(])(?:trailer|pv)(?:\s*\d+)?(?:$|[\s._\-\])])`)
	filenameSpecialPattern        = regexp.MustCompile(`(?i)(?:^|[\s._\-\[(])(?:ova|oad|sp)(?:\s*\d+)?(?:$|[\s._\-\])])`)
	filenameNamedSpecialPattern   = regexp.MustCompile(`(?i)(?:^|[\s._\-\[(])special(?:\s+\d+|[\s._\-\[]\d+|$)`)
	filenameSxEPattern            = regexp.MustCompile(`(?i)\bS(\d+)\s*E(\d+)(?:\s*[-~]\s*(?:E\s*)?(\d+))?(?:v\d+(?:\.\d+)?)?\b`)
	filenameSeasonXEpisodePattern = regexp.MustCompile(`(?i)\b(\d{1,2})\s*x\s*(\d{1,3})(?:\s*[-~]\s*(\d{1,3}))?(?:v\d+(?:\.\d+)?)?\b`)
	filenameAbsolutePattern       = regexp.MustCompile(`(?i)(?:\babsolute[\s._-]*)#?(\d{1,3})\b|\babs[\s._-]*#?(\d{1,3})\b`)
	filenameSeasonTextPattern     = regexp.MustCompile(`(?i)\b(Season|S)\s*(\d+)\b`)
	filenameSeasonEpisodePattern  = regexp.MustCompile(`\b(\d{1,3})(\s|v\d|$|\[)`)
	filenameSeasonDashPattern     = regexp.MustCompile(`\s-\s(\d+)\b`)
	filenameChineseEpisodePattern = regexp.MustCompile(`第\s*(\d{1,3})(?:\s*[-~]\s*(\d{1,3}))?\s*[集话話]`)
	filenameDashEpisodePattern    = regexp.MustCompile(`(?i)\s-\s(\d{1,3})(?:\s*[-~]\s*(\d{1,3}))?(?:v\d+(?:\.\d+)?)?(\s|END|$)`)
	filenameBracketEpisodePattern = regexp.MustCompile(`\[(\d{1,3})(?:\s*[-~]\s*(\d{1,3}))?\]`)
	filenameStandalonePattern     = regexp.MustCompile(`\b(\d{1,3})(v\d)?\b`)
	filenameLeadingGroupPattern   = regexp.MustCompile(`^\[.*?\]\s*`)
	filenameTrailingParenPattern  = regexp.MustCompile(`\s*\(.*?\)$`)
	filenameTrailingTagPattern    = regexp.MustCompile(`\s*\[.*?\]$`)
)

const (
	filenameEpisodeTypeEpisode = "episode"
	filenameEpisodeTypeSpecial = "special"
)

// IsVideoFile checks if the file is a video based on extension
func IsVideoFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp4", ".mkv", ".avi", ".mov", ".flv", ".wmv", ".ts", ".rmvb", ".webm", ".m2ts", ".m4v", ".mpg", ".mpeg", ".3gp", ".rm", ".mts", ".mxf", ".vob", ".ogm", ".ogv", ".divx", ".asf", ".strm":
		return true
	}
	return false
}

// ParsedInfo 包含从文件名解析出的信息
type ParsedInfo struct {
	Title           string
	Season          int
	Episode         int
	EpisodeEnd      int
	EpisodeType     string
	AbsoluteEpisode int
	Version         string
	Language        string
	Resolution      string
	Group           string
	Extension       string
	VideoCodec      string
	AudioCodec      string
	BitDepth        string // "10bit" or empty
	Source          string // "WebRip", "BDRip", etc.
	ParseSource     string
	Confidence      float64

	episodeExplicit bool
	seasonExplicit  bool
}

// HasExplicitEpisode reports whether the episode came from an unambiguous
// filename marker such as S01E02, " - 02", or [02], rather than a fallback
// standalone-number heuristic.
func (info ParsedInfo) HasExplicitEpisode() bool {
	return info.episodeExplicit
}

// HasExplicitSeason reports whether the season was encoded alongside the
// episode or in an explicit Season/S token in the filename.
func (info ParsedInfo) HasExplicitSeason() bool {
	return info.seasonExplicit
}

// ParseFilename 解析文件名.
//
//nolint:gocyclo // The ordered parser intentionally preserves deterministic precedence.
func ParseFilename(path string) ParsedInfo {
	filename := filepath.Base(path)
	ext := filepath.Ext(filename)
	cleanName := strings.TrimSuffix(filename, ext)

	info := ParsedInfo{
		Title:       cleanName, // Default fallback
		Season:      1,         // Default season
		Episode:     0,
		EpisodeType: filenameEpisodeTypeEpisode,
		ParseSource: "fallback",
		Confidence:  0.1,
		Extension:   strings.ToLower(strings.TrimPrefix(ext, ".")),
	}

	// 1. Pre-process: Replace delimiters with spaces for easier tokenizing locally
	// But keep original string for regex mapping
	// Common delimiters: . _
	normalized := strings.ReplaceAll(cleanName, "_", " ")
	// normalized = strings.ReplaceAll(normalized, ".", " ") // Avoid replacing dots inside version numbers? usually safe for anime titles.

	// 2. Extract Group (usually at start in [])
	// [Group] Title ...
	if match := filenameGroupPattern.FindStringSubmatch(cleanName); len(match) > 1 {
		info.Group = match[1]
	}

	// 3. Extract Resolution
	if match := filenameResolutionPattern.FindStringSubmatch(normalized); len(match) > 1 {
		info.Resolution = strings.ToLower(match[1])
	}

	// 3b. Extract Technical Tags
	// Video Codec
	if match := filenameVideoCodecPattern.FindStringSubmatch(normalized); len(match) > 1 {
		info.VideoCodec = strings.ToUpper(match[1])
	}
	// Audio Codec
	if match := filenameAudioCodecPattern.FindStringSubmatch(normalized); len(match) > 1 {
		info.AudioCodec = strings.ToUpper(match[1])
	}
	// Bit Depth
	if match := filenameBitDepthPattern.FindStringSubmatch(normalized); len(match) > 1 {
		if strings.Contains(strings.ToLower(match[1]), "10") {
			info.BitDepth = "10bit"
		} else {
			info.BitDepth = "8bit"
		}
	}
	// Source
	if match := filenameSourcePattern.FindStringSubmatch(normalized); len(match) > 1 {
		info.Source = match[1]
	}
	if match := filenameVersionPattern.FindStringSubmatch(normalized); len(match) > 1 {
		info.Version = strings.ToLower(match[1])
	}
	if match := filenameLanguagePattern.FindStringSubmatch(normalized); len(match) > 1 {
		info.Language = strings.ToLower(match[1])
	}
	switch {
	case filenameOpeningPattern.MatchString(cleanName):
		info.EpisodeType = "opening"
	case filenameEndingPattern.MatchString(cleanName):
		info.EpisodeType = "ending"
	case filenameTrailerPattern.MatchString(cleanName):
		info.EpisodeType = "trailer"
	case filenameSpecialPattern.MatchString(cleanName), filenameNamedSpecialPattern.MatchString(cleanName):
		info.EpisodeType = filenameEpisodeTypeSpecial
	}

	// 4. Extract Season and Episode
	// Try the new ParseSeason logic for a first pass at season
	info.Season = ParseSeason(cleanName)

	// Pattern A: "S01E02" / "S1E2", including common multi-episode forms
	// such as S01E01-E02 and S01E01-02.
	if match := filenameSxEPattern.FindStringSubmatch(normalized); len(match) > 1 {
		s, _ := strconv.Atoi(match[1])
		e, _ := strconv.Atoi(match[2])
		info.Season = s
		info.Episode = e
		info.EpisodeEnd = e
		if strings.TrimSpace(match[3]) != "" {
			info.EpisodeEnd, _ = strconv.Atoi(match[3])
		}
		info.seasonExplicit = true
		info.episodeExplicit = true
		info.ParseSource = "filename:sxe"
		info.Confidence = 0.98
		// Title is everything before the match
		idx := strings.Index(normalized, match[0])
		if idx > 0 {
			info.Title = cleanTitle(normalized[:idx])
		}
		return info
	}

	// Pattern A2: "1x02" / "1x02-03".
	if match := filenameSeasonXEpisodePattern.FindStringSubmatch(normalized); len(match) > 2 {
		info.Season, _ = strconv.Atoi(match[1])
		info.Episode, _ = strconv.Atoi(match[2])
		info.EpisodeEnd = info.Episode
		if strings.TrimSpace(match[3]) != "" {
			info.EpisodeEnd, _ = strconv.Atoi(match[3])
		}
		info.seasonExplicit = true
		info.episodeExplicit = true
		info.ParseSource = "filename:season-x-episode"
		info.Confidence = 0.97
		idx := strings.Index(normalized, match[0])
		if idx > 0 {
			info.Title = cleanTitle(normalized[:idx])
		}
		return info
	}

	if match := filenameAbsolutePattern.FindStringSubmatch(normalized); len(match) > 0 {
		value := match[1]
		if value == "" {
			value = match[2]
		}
		info.AbsoluteEpisode, _ = strconv.Atoi(value)
		info.ParseSource = "filename:absolute"
		info.Confidence = 0.9
	}

	// Pattern B: "Season 1 - 02" or "2nd Season - 02"
	// This handles explicit season text.
	seasonMatch := filenameSeasonTextPattern.FindStringSubmatch(normalized)
	if len(seasonMatch) > 0 {
		sNum, _ := strconv.Atoi(seasonMatch[2])
		info.Season = sNum
		info.seasonExplicit = true
		// Remove this season part to help finding episode number locally
		// e.g. "Title Season 2 05" -> "Title  05"
		tempName := strings.Replace(normalized, seasonMatch[0], "", 1)

		// Now look for standalone number as episode
		// Look for number at end or after " - "
		// Check for " - 05" specific pattern first
		if m := filenameSeasonDashPattern.FindStringSubmatch(tempName); len(m) > 1 {
			e, _ := strconv.Atoi(m[1])
			info.Episode = e
			info.EpisodeEnd = e
		} else {
			// Find last number? Or number followed by standard tags?
			// This is risky. Let's limit to specific structure.
			// "Title 05"
			// Using existing logic for episode number if season is found
			matches := filenameSeasonEpisodePattern.FindAllStringSubmatch(tempName, -1)
			if len(matches) > 0 {
				// Take the last number that looks like an episode?
				// Often the last number is the episode if resolution is stripped.
				// For now, let's take the one that was found.
				// A bit heuristic-heavy.
				lastM := matches[len(matches)-1]
				e, _ := strconv.Atoi(lastM[1])
				info.Episode = e
				info.EpisodeEnd = e
			}
		}
		info.episodeExplicit = info.Episode > 0
		info.ParseSource = "filename:season"
		info.Confidence = 0.9

		// Title is strictly before Season text
		idx := strings.Index(normalized, seasonMatch[0])
		if idx > 0 {
			info.Title = cleanTitle(normalized[:idx])
		}
		return info
	}

	// Pattern B2: Chinese episode markers, e.g. "第01集" or "第01-02话".
	if match := filenameChineseEpisodePattern.FindStringSubmatch(normalized); len(match) > 1 {
		// Parse the season after removing the episode marker. Otherwise the
		// generic Chinese-season heuristic can mistake the episode number
		// itself for the season (for example, 第07-08集).
		seasonName := strings.TrimSpace(strings.Replace(normalized, match[0], "", 1))
		info.Season = ParseSeason(seasonName)
		info.Episode, _ = strconv.Atoi(match[1])
		info.EpisodeEnd = info.Episode
		if strings.TrimSpace(match[2]) != "" {
			info.EpisodeEnd, _ = strconv.Atoi(match[2])
		}
		info.episodeExplicit = true
		info.ParseSource = "filename:chinese-episode"
		info.Confidence = 0.92
		idx := strings.Index(normalized, match[0])
		if idx > 0 {
			info.Title = cleanTitle(normalized[:idx])
		}
		return info
	}

	// Pattern C: Standard " - 01" or "[01]" (Assumes Season 1)
	// Title - 01
	if match := filenameDashEpisodePattern.FindStringSubmatch(cleanName); len(match) > 1 {
		e, _ := strconv.Atoi(match[1])
		info.Episode = e
		info.EpisodeEnd = e
		if strings.TrimSpace(match[2]) != "" {
			info.EpisodeEnd, _ = strconv.Atoi(match[2])
		}
		info.episodeExplicit = true
		info.ParseSource = "filename:dash"
		info.Confidence = 0.88
		idx := strings.Index(cleanName, match[0])
		if idx > 0 {
			info.Title = cleanTitle(cleanName[:idx])
		}
		return info
	}

	// Pattern D: "Title 01.mkv" (Space Separated)
	// This is very ambiguous (could be year 2012).
	// We only use this if we stripped resolution/year already.
	// Logic: Look for the last number in the string that fits 1-999 range.

	// Bracket pattern [01] and [01-02].
	matches := filenameBracketEpisodePattern.FindAllStringSubmatch(cleanName, -1)
	for _, m := range matches {
		num, _ := strconv.Atoi(m[1])
		if isLikelyEpisodeNumber(num) {
			info.Episode = num
			info.EpisodeEnd = num
			if strings.TrimSpace(m[2]) != "" {
				info.EpisodeEnd, _ = strconv.Atoi(m[2])
			}
			info.episodeExplicit = true
			info.ParseSource = "filename:bracket"
			info.Confidence = 0.86
			info.Title = cleanTitle(cleanName) // Cannot easily strip for brackets inside title
			return info
		}
	}

	// Fallback: If filename contains just "01" or "01v2" surrounded by spaces?
	// Heuristic: "Title 01"
	// Find the last standalone number.
	// Avoid years (1900-2099)
	allNums := filenameStandalonePattern.FindAllStringSubmatch(normalized, -1)
	if len(allNums) > 0 {
		// Find candidates
		candidate := 0
		found := false

		// Iterate backwards
		for i := len(allNums) - 1; i >= 0; i-- {
			nStr := allNums[i][1]
			val, _ := strconv.Atoi(nStr)
			if isLikelyEpisodeNumber(val) {
				candidate = val
				found = true
				break
			}
		}

		if found {
			info.Episode = candidate
			info.EpisodeEnd = candidate
			// Attempt to clean title: Assume Title is everything before this number?
			// Only if this number is near the end.
			// Let's just run CleanTitle on full name.
			info.Title = cleanTitle(cleanName)

			// Refine: if title ends with the episode number, strip it
			// e.g. "Naruto 01" -> "Naruto"
			trimPatt := fmt.Sprintf(`\s%02d(\s|$)`, candidate) // 01
			trimPatt2 := fmt.Sprintf(`\s%d(\s|$)`, candidate)  // 1

			tmpTitle := regexp.MustCompile(trimPatt).ReplaceAllString(info.Title, " ")
			tmpTitle = regexp.MustCompile(trimPatt2).ReplaceAllString(tmpTitle, " ")
			info.Title = strings.TrimSpace(tmpTitle)
			info.ParseSource = "filename:standalone"
			info.Confidence = 0.62

			return info
		}
	}

	// 5. Special tags
	if strings.Contains(strings.ToLower(cleanName), " ova ") || strings.Contains(strings.ToLower(cleanName), filenameEpisodeTypeSpecial) {
		info.Season = 0
	}

	// Final cleanup
	info.Title = cleanTitle(cleanName)
	return info
}

func isLikelyEpisodeNumber(num int) bool {
	if num == 0 {
		return false
	}
	// Filter out common resolutions if they appear as standalone numbers (rare but possible)
	if num == 480 || num == 720 || num == 1080 || num == 2160 {
		return false
	}
	// Filter out years
	if num > 1900 && num < 2100 {
		return false
	}
	// Filter out video codecs
	if num == 264 || num == 265 {
		return false
	}
	return true
}

func cleanTitle(raw string) string {
	s := raw
	// Remove leading [...]
	s = filenameLeadingGroupPattern.ReplaceAllString(s, "")
	// Remove trailing meta info
	s = filenameTrailingParenPattern.ReplaceAllString(s, "")
	s = filenameTrailingTagPattern.ReplaceAllString(s, "")
	// Remove file extension just in case
	s = strings.TrimSuffix(s, filepath.Ext(s))
	// normalize spacing
	return strings.TrimSpace(s)
}
