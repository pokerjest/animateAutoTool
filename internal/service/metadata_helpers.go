package service

import (
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

func getCandidateTitles(m *model.AnimeMetadata, query string) []string {
	seen := make(map[string]bool)
	var candidates []string
	add := func(t string) {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			seen[t] = true
			candidates = append(candidates, t)
		}
	}
	add(m.TitleCN)
	add(m.TitleJP)
	add(m.TitleEN)
	add(query)
	if strings.Contains(query, "-") {
		for _, part := range strings.Split(query, "-") {
			add(part)
		}
	}
	add(m.Title)
	return candidates
}

func bangumiMatchReferences(m *model.AnimeMetadata, queryTitle string) []string {
	seen := make(map[string]bool)
	references := make([]string, 0, 4)
	add := func(title string) {
		title = strings.TrimSpace(title)
		if title == "" || seen[title] {
			return
		}
		seen[title] = true
		references = append(references, title)
	}

	// Once another provider has identified the work, its provider-specific
	// title is safer than the shared display fields, which may contain a stale
	// Bangumi match from an older scan.
	if m != nil {
		if m.TMDBID != 0 {
			add(m.TMDBTitle)
		}
		if m.AniListID != 0 {
			add(m.AniListTitle)
		}
	}
	if len(references) > 0 {
		return references
	}

	add(queryTitle)
	if m != nil {
		add(m.TitleCN)
		add(m.TitleJP)
		add(m.TitleEN)
		add(m.Title)
	}
	return references
}

func bestBangumiSearchResult(results []bangumi.SearchResult, references []string) (*bangumi.SearchResult, int) {
	bestIndex := -1
	bestScore := 0
	for i := range results {
		for _, candidateTitle := range []string{results[i].NameCN, results[i].Name} {
			for _, reference := range references {
				score := titleMatchScore(candidateTitle, reference)
				if score > bestScore {
					bestIndex = i
					bestScore = score
				}
			}
		}
	}
	if bestIndex < 0 || bestScore < 45 {
		return nil, bestScore
	}
	result := results[bestIndex]
	return &result, bestScore
}

func shouldApplyBangumiSubject(m *model.AnimeMetadata, subject *bangumi.Subject, queryTitle string) bool {
	if subject == nil {
		return false
	}
	bangumiTitles := []string{subject.NameCN, subject.Name}
	referenceTitles := bangumiMatchReferences(m, queryTitle)
	for _, bgmTitle := range bangumiTitles {
		for _, ref := range referenceTitles {
			if titlesLookRelated(bgmTitle, ref) {
				return true
			}
		}
	}
	return false
}

func metadataSourcesConflict(m *model.AnimeMetadata) bool {
	if m == nil || m.BangumiID == 0 || strings.TrimSpace(m.BangumiTitle) == "" {
		return false
	}
	independentTitles := make([]string, 0, 2)
	if m.TMDBID != 0 && strings.TrimSpace(m.TMDBTitle) != "" {
		independentTitles = append(independentTitles, m.TMDBTitle)
	}
	if m.AniListID != 0 && strings.TrimSpace(m.AniListTitle) != "" {
		independentTitles = append(independentTitles, m.AniListTitle)
	}
	if len(independentTitles) == 0 {
		return false
	}
	for _, title := range independentTitles {
		if titlesLookRelated(m.BangumiTitle, title) {
			return false
		}
	}
	return true
}

func metadataRefreshQuery(m *model.AnimeMetadata) string {
	if m == nil {
		return ""
	}
	if metadataSourcesConflict(m) {
		if strings.TrimSpace(m.TMDBTitle) != "" {
			return m.TMDBTitle
		}
		if strings.TrimSpace(m.AniListTitle) != "" {
			return m.AniListTitle
		}
	}
	if strings.TrimSpace(m.TitleCN) != "" {
		return m.TitleCN
	}
	return m.Title
}

func sourceMatchScore(rawQueryTitle string, m *model.AnimeMetadata, candidateTitle string) int {
	references := []string{rawQueryTitle}
	if m != nil {
		references = append(references, m.TitleCN, m.TitleJP, m.TitleEN, m.Title)
	}
	best := 0
	for _, ref := range references {
		score := titleMatchScore(candidateTitle, ref)
		if score > best {
			best = score
		}
	}
	return best
}

// SyncMetadataToModels propagates metadata fields to all linked Subscription and LocalAnime records
func (s *MetadataService) SyncMetadataToModels(m *model.AnimeMetadata) {
	if m == nil || m.ID == 0 {
		return
	}

	mStore := metadataStore()
	if mStore == nil {
		return
	}

	updates := map[string]interface{}{
		"image":   m.Image,
		"summary": m.Summary,
	}
	_ = mStore.PropagateToSubscriptions(m.ID, updates)

	localUpdates := map[string]interface{}{
		"image":    m.Image,
		"summary":  m.Summary,
		"air_date": m.AirDate,
	}
	_ = mStore.PropagateToLocalAnimes(m.ID, localUpdates)
}

func performWithRetry[T any](op func() (T, error)) (T, error) {
	var result T
	var err error
	for i := 0; i < 3; i++ {
		if i > 0 {
			time.Sleep(1 * time.Second)
		}
		result, err = op()
		if err == nil {
			return result, nil
		}
	}
	return result, err
}
