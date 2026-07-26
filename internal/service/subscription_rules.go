package service

import (
	"log"
	"regexp"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
)

const (
	resolution2160p = "2160p"
	resolution1080p = "1080p"
	resolution720p  = "720p"
	languageCHS     = "chs"
	languageCHT     = "cht"
	languageCHSCHT  = "chs_cht"
)

type subscriptionRuleSet struct {
	resolutionFilter string
	subtitleLanguage string
	filter           patternMatcher
	exclude          patternMatcher
}

type patternMatcher struct {
	raw      string
	regex    *regexp.Regexp
	literal  string
	isRegex  bool
	disabled bool
}

func buildSubscriptionRuleSet(sub *model.Subscription) subscriptionRuleSet {
	rules := subscriptionRuleSet{}
	if sub == nil {
		return rules
	}

	rules.resolutionFilter, _ = NormalizeResolutionFilter(sub.ResolutionFilter)
	rules.subtitleLanguage, _ = NormalizeSubtitleLanguage(sub.SubtitleLanguage)
	rules.filter = newPatternMatcher(sub.FilterRule, "filter", sub.Title)
	rules.exclude = newPatternMatcher(sub.ExcludeRule, "exclude", sub.Title)
	return rules
}

func BuildSubscriptionRuleSetForValidation(sub *model.Subscription) subscriptionRuleSet {
	return buildSubscriptionRuleSet(sub)
}

func (r subscriptionRuleSet) Allows(ep parser.Episode) bool {
	return r.allows(ep)
}

func newPatternMatcher(raw, kind, subTitle string) patternMatcher {
	expr := strings.TrimSpace(raw)
	if expr == "" {
		return patternMatcher{disabled: true}
	}
	re, err := regexp.Compile(expr)
	if err == nil {
		return patternMatcher{raw: expr, regex: re, isRegex: true}
	}

	log.Printf("SubscriptionManager: %s rule for %q is not a valid regex, falling back to literal match: %q (%v)", kind, subTitle, expr, err)
	return patternMatcher{
		raw:     expr,
		literal: strings.ToLower(expr),
	}
}

func (r subscriptionRuleSet) allows(ep parser.Episode) bool {
	if r.exclude.matches(ep) {
		return false
	}
	if !r.filter.disabled && !r.filter.matches(ep) {
		return false
	}
	if r.resolutionFilter != "" && episodeResolution(ep) != r.resolutionFilter {
		return false
	}
	return matchesSubtitleLanguage(ep.Title, r.subtitleLanguage)
}

// NormalizeResolutionFilter canonicalizes values accepted by the API and
// stored in subscriptions. The boolean is false for unsupported non-empty
// values so callers can reject malformed requests instead of silently
// disabling a requested filter.
func NormalizeResolutionFilter(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", true
	case resolution2160p, "4k", "uhd", "3840x2160":
		return resolution2160p, true
	case resolution1080p, "fhd", "1920x1080":
		return resolution1080p, true
	case resolution720p, "hd", "1280x720":
		return resolution720p, true
	default:
		return "", false
	}
}

// NormalizeSubtitleLanguage canonicalizes the subtitle language selector.
func NormalizeSubtitleLanguage(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", true
	case languageCHS, "simplified":
		return languageCHS, true
	case languageCHT, "traditional":
		return languageCHT, true
	case languageCHSCHT, "chs+cht", "bilingual":
		return languageCHSCHT, true
	default:
		return "", false
	}
}

func episodeResolution(ep parser.Episode) string {
	if value, ok := NormalizeResolutionFilter(ep.Resolution); ok && value != "" {
		return value
	}
	value, _ := NormalizeResolutionFilter(parser.ParseTitle(ep.Title).Resolution)
	return value
}

var (
	simplifiedSubtitleToken  = regexp.MustCompile(`(?i)(^|[^a-z0-9])(chs|sc|gbk|gb2312)([^a-z0-9]|$)`)
	traditionalSubtitleToken = regexp.MustCompile(`(?i)(^|[^a-z0-9])(cht|tc|big5)([^a-z0-9]|$)`)
)

func matchesSubtitleLanguage(title, wanted string) bool {
	if wanted == "" {
		return true
	}

	combined := strings.Contains(title, "简繁") ||
		strings.Contains(title, "簡繁") ||
		strings.Contains(title, "繁简") ||
		strings.Contains(title, "繁簡")
	simplified := combined || strings.Contains(title, "简中") ||
		strings.Contains(title, "简体") ||
		strings.Contains(title, "簡中") ||
		strings.Contains(title, "簡體") ||
		strings.Contains(strings.ToUpper(title), "[GB]") ||
		simplifiedSubtitleToken.MatchString(title)
	traditional := combined || strings.Contains(title, "繁中") ||
		strings.Contains(title, "繁体") ||
		strings.Contains(title, "繁體") ||
		strings.Contains(title, "正體") ||
		traditionalSubtitleToken.MatchString(title)

	switch wanted {
	case languageCHS:
		return simplified
	case languageCHT:
		return traditional
	case languageCHSCHT:
		return simplified && traditional
	default:
		return true
	}
}

func (m patternMatcher) matches(ep parser.Episode) bool {
	if m.disabled {
		return false
	}

	for _, candidate := range subscriptionRuleCandidates(ep) {
		if candidate == "" {
			continue
		}
		if m.isRegex {
			if m.regex.MatchString(candidate) {
				return true
			}
			continue
		}
		if strings.Contains(strings.ToLower(candidate), m.literal) {
			return true
		}
	}

	return false
}

func subscriptionRuleCandidates(ep parser.Episode) []string {
	seen := make(map[string]bool)
	var candidates []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		candidates = append(candidates, value)
	}

	add(ep.Title)
	add(ep.SubGroup)
	add(ep.AnimeIdentify)
	return candidates
}
