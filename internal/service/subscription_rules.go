package service

import (
	"log"
	"regexp"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
)

type subscriptionRuleSet struct {
	subscriptionTitle string
	subtitleGroup     string
	filter            patternMatcher
	exclude           patternMatcher
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

	rules.subscriptionTitle = strings.TrimSpace(sub.Title)
	rules.subtitleGroup = strings.TrimSpace(sub.SubtitleGroup)
	rules.filter = newPatternMatcher(sub.FilterRule, "filter", sub.Title)
	rules.exclude = newPatternMatcher(sub.ExcludeRule, "exclude", sub.Title)
	return rules
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
	if r.exclude.matches(ep, r.subtitleGroup) {
		return false
	}
	if r.filter.disabled {
		return true
	}
	return r.filter.matches(ep, r.subtitleGroup)
}

func (m patternMatcher) matches(ep parser.Episode, subtitleGroup string) bool {
	if m.disabled {
		return false
	}

	for _, candidate := range subscriptionRuleCandidates(ep, subtitleGroup) {
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

func subscriptionRuleCandidates(ep parser.Episode, subtitleGroup string) []string {
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
	add(subtitleGroup)
	return candidates
}
