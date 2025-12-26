package parser

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ParsedInfo 包含从文件名解析出的信息
type ParsedInfo struct {
	Title      string
	Season     int
	Episode    int
	Resolution string
	Group      string
	Extension  string
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

	// 1. Extract Group (usually at start in [])
	// [Group] Title ...
	groupRegex := regexp.MustCompile(`^\[(.*?)\]`)
	if match := groupRegex.FindStringSubmatch(cleanName); len(match) > 1 {
		info.Group = match[1]
	}

	// 2. Extract Resolution
	resRegex := regexp.MustCompile(`(?i)(1080p|720p|2160p|4k)`)
	if match := resRegex.FindStringSubmatch(cleanName); len(match) > 1 {
		info.Resolution = match[1]
	}

	// 3. Extract Season and Episode
	// Priority 1: S01E02 standard format
	sxeRegex := regexp.MustCompile(`(?i)S(\d+)E(\d+)`)
	if match := sxeRegex.FindStringSubmatch(cleanName); len(match) > 1 {
		s, _ := strconv.Atoi(match[1])
		e, _ := strconv.Atoi(match[2])
		info.Season = s
		info.Episode = e
		// Title is usually before SxxExx
		// [Group] Title S01E02 ...
		idx := strings.Index(cleanName, match[0])
		if idx > 0 {
			t := cleanName[:idx]
			info.Title = cleanTitle(t)
		}
		return info
	}

	// Priority 2: Episode number only (often with - or [] or space)
	// Title - 01
	// Title [01]
	// Title 01 (risky)

	// Try " - 01" usually safe
	dashEpRegex := regexp.MustCompile(`\s-\s(\d+)(\s|$)`)
	if match := dashEpRegex.FindStringSubmatch(cleanName); len(match) > 1 {
		e, _ := strconv.Atoi(match[1])
		info.Episode = e
		idx := strings.Index(cleanName, match[0])
		if idx > 0 {
			info.Title = cleanTitle(cleanName[:idx])
		}
		return info
	}

	// Try "[01]" but avoid [1080p] [AVC] etc.
	// Heuristic: Episode number is usually small (< 1000) or we check context
	bracketEpRegex := regexp.MustCompile(`\[(\d{1,3})\]`) // 1-3 digits
	matches := bracketEpRegex.FindAllStringSubmatch(cleanName, -1)
	for _, m := range matches {
		num, _ := strconv.Atoi(m[1])
		// Check if it's likely a year or resolution
		if num > 1900 && num < 2100 {
			continue // Year
		}
		if num == 720 || num == 1080 {
			continue // Resolution
		}
		if num == 264 || num == 265 {
			continue // Codec
		}
		// Likely episode
		info.Episode = num
		// For title, we just strip known tags and brackets
		// This is harder to split cleanly, so we keep using cleanTitle on entire string
		info.Title = cleanTitle(cleanName)
		return info
	}

	// Priority 3: Check Folder Name for Season
	// If the file is inside "Season 2", we force Season = 2
	// This logic needs to be handled by the caller or passed in context.
	// Here we just parse the file string.

	// Final fallback cleanup for title if not set by SxxExx
	info.Title = cleanTitle(cleanName)

	return info
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
