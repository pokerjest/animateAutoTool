package parser

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// IsVideoFile checks if the file is a video based on extension
func IsVideoFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp4", ".mkv", ".avi", ".mov", ".flv", ".wmv", ".ts", ".rmvb", ".webm", ".m2ts":
		return true
	}
	return false
}

// ParsedInfo 包含从文件名解析出的信息
type ParsedInfo struct {
	Title      string
	Season     int
	Episode    int
	Resolution string
	Group      string
	Extension  string
	VideoCodec string
	AudioCodec string
	BitDepth   string // "10bit" or empty
	Source     string // "WebRip", "BDRip", etc.
}

// ParseFilename 解析文件名
func ParseFilename(path string) ParsedInfo {
	filename := filepath.Base(path)
	ext := filepath.Ext(filename)
	cleanName := strings.TrimSuffix(filename, ext)

	info := ParsedInfo{
		Title:     cleanName, // Default fallback
		Season:    1,         // Default season
		Episode:   0,
		Extension: strings.ToLower(strings.TrimPrefix(ext, ".")),
	}

	// 1. Pre-process: Replace delimiters with spaces for easier tokenizing locally
	// But keep original string for regex mapping
	// Common delimiters: . _
	normalized := strings.ReplaceAll(cleanName, "_", " ")
	// normalized = strings.ReplaceAll(normalized, ".", " ") // Avoid replacing dots inside version numbers? usually safe for anime titles.

	// 2. Extract Group (usually at start in [])
	// [Group] Title ...
	groupRegex := regexp.MustCompile(`^\[(.*?)\]`)
	if match := groupRegex.FindStringSubmatch(cleanName); len(match) > 1 {
		info.Group = match[1]
	}

	// 3. Extract Resolution
	resRegex := regexp.MustCompile(`(?i)\b(1080p|720p|2160p|4k|360p|480p|FHD|HD|1920x1080|1280x720|3840x2160)\b`)
	if match := resRegex.FindStringSubmatch(normalized); len(match) > 1 {
		info.Resolution = strings.ToLower(match[1])
	}

	// 3b. Extract Technical Tags
	// Video Codec
	codecRegex := regexp.MustCompile(`(?i)\b(h264|h265|x264|x265|av1|hevc|vp9)\b`)
	if match := codecRegex.FindStringSubmatch(normalized); len(match) > 1 {
		info.VideoCodec = strings.ToUpper(match[1])
	}
	// Audio Codec
	audioRegex := regexp.MustCompile(`(?i)\b(flac|aac|aacx2|aacx4|aacx3|ac3|eac3|dts|dts-hd|truehd|opus|mp3)\b`)
	if match := audioRegex.FindStringSubmatch(normalized); len(match) > 1 {
		info.AudioCodec = strings.ToUpper(match[1])
	}
	// Bit Depth
	bitRegex := regexp.MustCompile(`(?i)\b(10bit|8bit|hi10p|ma10p)\b`)
	if match := bitRegex.FindStringSubmatch(normalized); len(match) > 1 {
		if strings.Contains(strings.ToLower(match[1]), "10") {
			info.BitDepth = "10bit"
		} else {
			info.BitDepth = "8bit"
		}
	}
	// Source
	sourceRegex := regexp.MustCompile(`(?i)\b(web-?rip|bd-?rip|web-?dl|bluray|dvd-?rip|hdtv)\b`)
	if match := sourceRegex.FindStringSubmatch(normalized); len(match) > 1 {
		info.Source = match[1]
	}

	// 4. Extract Season and Episode
	// Try the new ParseSeason logic for a first pass at season
	info.Season = ParseSeason(cleanName)

	// Pattern A: "S01E02" / "S1E2"
	sxeRegex := regexp.MustCompile(`(?i)\bS(\d+)\s*E(\d+)\b`)
	if match := sxeRegex.FindStringSubmatch(normalized); len(match) > 1 {
		s, _ := strconv.Atoi(match[1])
		e, _ := strconv.Atoi(match[2])
		info.Season = s
		info.Episode = e
		// Title is everything before the match
		idx := strings.Index(normalized, match[0])
		if idx > 0 {
			info.Title = cleanTitle(normalized[:idx])
		}
		return info
	}

	// Pattern B: "Season 1 - 02" or "2nd Season - 02"
	// This handles explicit season text.
	seasonTextRegex := regexp.MustCompile(`(?i)\b(Season|S)\s*(\d+)\b`)
	seasonMatch := seasonTextRegex.FindStringSubmatch(normalized)
	if len(seasonMatch) > 0 {
		sNum, _ := strconv.Atoi(seasonMatch[2])
		info.Season = sNum
		// Remove this season part to help finding episode number locally
		// e.g. "Title Season 2 05" -> "Title  05"
		tempName := strings.Replace(normalized, seasonMatch[0], "", 1)

		// Now look for standalone number as episode
		// Look for number at end or after " - "
		epRegex := regexp.MustCompile(`\b(\d{1,3})(\s|v\d|$|\[)`) // simple number
		// Check for " - 05" specific pattern first
		dashEpRegex := regexp.MustCompile(`\s-\s(\d+)\b`)
		if m := dashEpRegex.FindStringSubmatch(tempName); len(m) > 1 {
			e, _ := strconv.Atoi(m[1])
			info.Episode = e
		} else {
			// Find last number? Or number followed by standard tags?
			// This is risky. Let's limit to specific structure.
			// "Title 05"
			// Using existing logic for episode number if season is found
			matches := epRegex.FindAllStringSubmatch(tempName, -1)
			if len(matches) > 0 {
				// Take the last number that looks like an episode?
				// Often the last number is the episode if resolution is stripped.
				// For now, let's take the one that was found.
				// A bit heuristic-heavy.
				lastM := matches[len(matches)-1]
				e, _ := strconv.Atoi(lastM[1])
				info.Episode = e
			}
		}

		// Title is strictly before Season text
		idx := strings.Index(normalized, seasonMatch[0])
		if idx > 0 {
			info.Title = cleanTitle(normalized[:idx])
		}
		return info
	}

	// Pattern C: Standard " - 01" or "[01]" (Assumes Season 1)
	// Title - 01
	dashEpRegex := regexp.MustCompile(`\s-\s(\d+)(\s|v\d|END|$)`)
	if match := dashEpRegex.FindStringSubmatch(cleanName); len(match) > 1 {
		e, _ := strconv.Atoi(match[1])
		info.Episode = e
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

	// Bracket pattern [01]
	bracketEpRegex := regexp.MustCompile(`\[(\d{1,3})\]`)
	matches := bracketEpRegex.FindAllStringSubmatch(cleanName, -1)
	for _, m := range matches {
		num, _ := strconv.Atoi(m[1])
		if isLikelyEpisodeNumber(num) {
			info.Episode = num
			info.Title = cleanTitle(cleanName) // Cannot easily strip for brackets inside title
			return info
		}
	}

	// Fallback: If filename contains just "01" or "01v2" surrounded by spaces?
	// Heuristic: "Title 01"
	// Find the last standalone number.
	// Avoid years (1900-2099)
	spaceNumRegex := regexp.MustCompile(`\b(\d{1,3})(v\d)?\b`)
	allNums := spaceNumRegex.FindAllStringSubmatch(normalized, -1)
	if len(allNums) > 0 {
		// Find candidates
		var candidate int = 0
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

			return info
		}
	}

	// 5. Special tags
	if strings.Contains(strings.ToLower(cleanName), " ova ") || strings.Contains(strings.ToLower(cleanName), "special") {
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
	s = regexp.MustCompile(`^\[.*?\]\s*`).ReplaceAllString(s, "")
	// Remove trailing meta info
	s = regexp.MustCompile(`\s*\(.*?\)$`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`\s*\[.*?\]$`).ReplaceAllString(s, "")
	// Remove file extension just in case
	s = strings.TrimSuffix(s, filepath.Ext(s))
	// normalize spacing
	return strings.TrimSpace(s)
}
