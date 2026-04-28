package service

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/pokerjest/animateAutoTool/internal/parser"
)

var seasonNoisePattern = regexp.MustCompile(`(?i)\b(?:season|s)\s*\d+\b|\b\d+(?:st|nd|rd|th)\s+season\b|第[一二三四五六七八九十百零0-9]+季`)

func normalizedRuleTitle(raw string) string {
	var best string
	for _, variant := range titleRuleVariants(raw) {
		normalized := compactRuleTitle(variant)
		if len([]rune(normalized)) > len([]rune(best)) {
			best = normalized
		}
	}
	return best
}

func titleMatchScore(a, b string) int {
	best := 0
	for _, av := range titleRuleVariants(a) {
		na := compactRuleTitle(av)
		if na == "" {
			continue
		}
		for _, bv := range titleRuleVariants(b) {
			nb := compactRuleTitle(bv)
			if nb == "" {
				continue
			}
			score := compareNormalizedRuleTitles(na, nb)
			if score > best {
				best = score
			}
		}
	}
	return best
}

func titlesLookRelated(a, b string) bool {
	return titleMatchScore(a, b) >= 45
}

func titleRuleVariants(raw string) []string {
	cleaned := strings.TrimSpace(parser.CleanTitle(raw))
	if cleaned == "" {
		return nil
	}

	seen := make(map[string]bool)
	var variants []string
	add := func(value string) {
		value = strings.TrimSpace(strings.Join(strings.Fields(value), " "))
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		variants = append(variants, value)
	}

	add(cleaned)
	add(seasonNoisePattern.ReplaceAllString(cleaned, " "))

	splitter := strings.NewReplacer("／", "/", "｜", "|", "｜", "|")
	for _, segment := range strings.FieldsFunc(splitter.Replace(cleaned), func(r rune) bool {
		return r == '/' || r == '|' || r == '｜'
	}) {
		add(segment)
		add(seasonNoisePattern.ReplaceAllString(segment, " "))
	}

	return variants
}

func compactRuleTitle(raw string) string {
	cleaned := strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	for _, r := range cleaned {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func compareNormalizedRuleTitles(na, nb string) int {
	if na == "" || nb == "" {
		return 0
	}
	if na == nb {
		return 100
	}
	if strings.Contains(na, nb) || strings.Contains(nb, na) {
		return 80
	}

	common := longestCommonSubstringLen(na, nb)
	shorter := len([]rune(na))
	if other := len([]rune(nb)); other < shorter {
		shorter = other
	}
	if shorter == 0 || common == 0 {
		return 0
	}

	switch {
	case common >= 6:
		return 70
	case common >= 4 && common*2 >= shorter:
		return 60
	case common >= 3 && common*3 >= shorter*2:
		return 45
	case sharedMeaningfulRuneCount(na, nb) >= 3 && sharedMeaningfulRuneCount(na, nb)*2 >= shorter:
		return 45
	default:
		return 0
	}
}

func sharedMeaningfulRuneCount(a, b string) int {
	counts := make(map[rune]int)
	for _, r := range []rune(a) {
		if unicode.IsDigit(r) {
			continue
		}
		counts[r]++
	}

	seen := make(map[rune]bool)
	shared := 0
	for _, r := range []rune(b) {
		if unicode.IsDigit(r) || seen[r] || counts[r] == 0 {
			continue
		}
		seen[r] = true
		shared++
	}
	return shared
}

func longestCommonSubstringLen(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)
	if len(ra) == 0 || len(rb) == 0 {
		return 0
	}

	prev := make([]int, len(rb)+1)
	best := 0
	for i := 1; i <= len(ra); i++ {
		curr := make([]int, len(rb)+1)
		for j := 1; j <= len(rb); j++ {
			if ra[i-1] == rb[j-1] {
				curr[j] = prev[j-1] + 1
				if curr[j] > best {
					best = curr[j]
				}
			}
		}
		prev = curr
	}
	return best
}
