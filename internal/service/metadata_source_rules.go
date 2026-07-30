package service

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/model"
)

type metadataSourceChoice struct {
	name     string
	id       int
	title    string
	image    string
	summary  string
	priority int
}

const (
	metadataFieldTitle     = "title"
	metadataFieldImage     = "image"
	metadataFieldSummary   = "summary"
	metadataSourceLocalNFO = "local-nfo"
)

func buildMetadataSourceChoices(m *model.AnimeMetadata) []metadataSourceChoice {
	var choices []metadataSourceChoice
	if m == nil {
		return choices
	}
	if m.TMDBID != 0 {
		choices = append(choices, metadataSourceChoice{
			name:     "tmdb",
			id:       m.TMDBID,
			title:    m.TMDBTitle,
			image:    m.TMDBImage,
			summary:  m.TMDBSummary,
			priority: 3,
		})
	}
	if m.AniListID != 0 {
		choices = append(choices, metadataSourceChoice{
			name:     "anilist",
			id:       m.AniListID,
			title:    m.AniListTitle,
			image:    m.AniListImage,
			summary:  m.AniListSummary,
			priority: 2,
		})
	}
	if m.BangumiID != 0 {
		choices = append(choices, metadataSourceChoice{
			name:     "bangumi",
			id:       m.BangumiID,
			title:    m.BangumiTitle,
			image:    m.BangumiImage,
			summary:  m.BangumiSummary,
			priority: 1,
		})
	}
	order := configuredMetadataSourceOrder()
	for i := range choices {
		choices[i].priority = len(order) + 1
		for index, source := range order {
			if choices[i].name == source {
				choices[i].priority = len(order) - index
				break
			}
		}
	}
	sort.SliceStable(choices, func(i, j int) bool { return choices[i].priority > choices[j].priority })
	return choices
}

func configuredMetadataSourceOrder() []string {
	raw := strings.TrimSpace(configValue(model.ConfigKeyMetadataSourceOrder))
	if raw == "" {
		raw = "bangumi,tmdb,anilist"
	}
	seen := map[string]bool{}
	order := make([]string, 0, 3)
	for _, item := range strings.Split(raw, ",") {
		source := strings.ToLower(strings.TrimSpace(item))
		switch source {
		case metadataSourceBangumi, metadataSourceTMDB, metadataSourceAniList:
			if !seen[source] {
				seen[source] = true
				order = append(order, source)
			}
		}
	}
	for _, source := range []string{metadataSourceBangumi, metadataSourceTMDB, metadataSourceAniList} {
		if !seen[source] {
			order = append(order, source)
		}
	}
	return order
}

func selectMetadataSource(rawQueryTitle string, m *model.AnimeMetadata) *metadataSourceChoice {
	choices := buildMetadataSourceChoices(m)
	var selected *metadataSourceChoice
	bestScore := -1
	for i := range choices {
		score := sourceMatchScore(rawQueryTitle, m, choices[i].title)
		if selected == nil || score > bestScore || (score == bestScore && choices[i].priority > selected.priority) {
			bestScore = score
			selected = &choices[i]
		}
	}
	return selected
}

func fallbackSummaryForSource(source string, m *model.AnimeMetadata) string {
	if m == nil {
		return ""
	}
	for _, candidate := range orderedMetadataSources(source) {
		if candidate == source {
			continue
		}
		if value := metadataSourceField(m, candidate, metadataFieldSummary); value != "" {
			return value
		}
	}
	return ""
}

func orderedMetadataSources(preferred string) []string {
	order := configuredMetadataSourceOrder()
	preferred = strings.ToLower(strings.TrimSpace(preferred))
	if preferred == "" {
		return order
	}
	result := make([]string, 0, len(order))
	result = append(result, preferred)
	for _, source := range order {
		if source != preferred {
			result = append(result, source)
		}
	}
	return result
}

func metadataSourceField(m *model.AnimeMetadata, source, field string) string {
	if m == nil {
		return ""
	}
	switch source {
	case metadataSourceBangumi:
		switch field {
		case metadataFieldTitle:
			return strings.TrimSpace(m.BangumiTitle)
		case metadataFieldImage:
			return strings.TrimSpace(m.BangumiImage)
		case metadataFieldSummary:
			return strings.TrimSpace(m.BangumiSummary)
		}
	case metadataSourceTMDB:
		switch field {
		case metadataFieldTitle:
			return strings.TrimSpace(m.TMDBTitle)
		case metadataFieldImage:
			return strings.TrimSpace(m.TMDBImage)
		case metadataFieldSummary:
			return strings.TrimSpace(m.TMDBSummary)
		}
	case metadataSourceAniList:
		switch field {
		case metadataFieldTitle:
			return strings.TrimSpace(m.AniListTitle)
		case metadataFieldImage:
			return strings.TrimSpace(m.AniListImage)
		case metadataFieldSummary:
			return strings.TrimSpace(m.AniListSummary)
		}
	}
	return ""
}

func firstConfiguredMetadataField(m *model.AnimeMetadata, field string) (string, string) {
	for _, source := range configuredMetadataSourceOrder() {
		if value := metadataSourceField(m, source, field); value != "" {
			return value, source
		}
	}
	return "", ""
}

func metadataFieldSources(raw string) map[string]string {
	result := map[string]string{}
	_ = json.Unmarshal([]byte(raw), &result)
	return result
}

func metadataFieldLocked(sources map[string]string, field string) bool {
	source := strings.ToLower(strings.TrimSpace(sources[field]))
	return source == metadataSourceLocalNFO || source == "user" || source == "manual"
}
