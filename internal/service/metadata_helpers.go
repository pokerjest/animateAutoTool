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

func shouldApplyBangumiSubject(m *model.AnimeMetadata, subject *bangumi.Subject, queryTitle string) bool {
	if subject == nil {
		return false
	}
	if m == nil {
		return true
	}
	if m.TMDBID == 0 && m.AniListID == 0 {
		return true
	}

	bangumiTitles := []string{subject.NameCN, subject.Name}
	referenceTitles := []string{queryTitle, m.TMDBTitle, m.AniListTitle, m.TitleCN, m.TitleJP, m.TitleEN, m.Title}
	for _, bgmTitle := range bangumiTitles {
		for _, ref := range referenceTitles {
			if titlesLookRelated(bgmTitle, ref) {
				return true
			}
		}
	}
	return false
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
		"title":   m.Title,
		"summary": m.Summary,
	}
	_ = mStore.PropagateToSubscriptions(m.ID, updates)

	localUpdates := map[string]interface{}{
		"image":    m.Image,
		"title":    m.Title,
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
