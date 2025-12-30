package parser

import (
	"regexp"
	"strings"
)

// CleanTitle removes common tags like [Group] or [1080p] to get a search-friendly title
func CleanTitle(raw string) string {
	s := raw

	// 1. Remove all [...] content
	s = regexp.MustCompile(`\[.*?\]`).ReplaceAllString(s, "")

	// 2. Remove all (...) content
	s = regexp.MustCompile(`\(.*?\)`).ReplaceAllString(s, "")

	// 3. Remove Season info (Series/Season X, Sxx, 第x季, Part x)
	reSeason := regexp.MustCompile(`(?i)(season\s*\d+|s\d{1,2}|第\s*\d+\s*季|part\s*\d+)`)
	s = reSeason.ReplaceAllString(s, "")

	// 4. Cleanup: Remove extra spaces and leading/trailing dashes/spaces
	s = strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(s, " "))
	s = strings.Trim(s, "- ")

	if s == "" {
		return raw // Fallback if we stripped everything
	}
	return s
}
