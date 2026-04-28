package service

import "github.com/pokerjest/animateAutoTool/internal/model"

type metadataSourceChoice struct {
	name     string
	id       int
	title    string
	image    string
	summary  string
	priority int
}

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
	return choices
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
	switch source {
	case metadataSourceBangumi:
		if m.TMDBSummary != "" {
			return m.TMDBSummary
		}
		return m.AniListSummary
	case metadataSourceTMDB:
		if m.AniListSummary != "" {
			return m.AniListSummary
		}
		return m.BangumiSummary
	case metadataSourceAniList:
		if m.TMDBSummary != "" {
			return m.TMDBSummary
		}
		return m.BangumiSummary
	default:
		return ""
	}
}
