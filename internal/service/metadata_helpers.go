package service

import (
	"log"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/db"
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
	return providerMatchReferences(m, queryTitle, metadataSourceBangumi)
}

func providerMatchReferences(m *model.AnimeMetadata, queryTitle, excludedProvider string) []string {
	references := []string{strings.TrimSpace(queryTitle)}
	if metadataSourcesConflict(m) {
		// Two already-polluted providers must not validate each other. During
		// repair, only the independent owner query is trusted.
		return nonEmptyTitles(references)
	}
	if m != nil {
		if excludedProvider != metadataSourceBangumi && m.BangumiID != 0 {
			references = append(references, m.BangumiTitle)
		}
		if excludedProvider != metadataSourceTMDB && m.TMDBID != 0 {
			references = append(references, m.TMDBTitle)
		}
		if excludedProvider != metadataSourceAniList && m.AniListID != 0 {
			references = append(references, m.AniListTitle)
		}
	}
	return nonEmptyTitles(references)
}

func providerCandidateScore(references, candidateTitles []string) int {
	best := 0
	for _, reference := range references {
		for _, candidate := range candidateTitles {
			if score := titleMatchScore(reference, candidate); score > best {
				best = score
			}
		}
	}
	return best
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

func metadataCanBridgeTitles(m *model.AnimeMetadata) bool {
	return m != nil && !metadataSourcesConflict(m)
}

func metadataRefreshQuery(m *model.AnimeMetadata) string {
	if m == nil {
		return ""
	}
	if metadataSourcesConflict(m) {
		// Do not arbitrarily let TMDB or AniList win a polluted row. The active
		// display title reflects the source the user last selected; owner-aware
		// local/subscription refreshes use their own independent title instead.
		if strings.TrimSpace(m.Title) != "" {
			return m.Title
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
	if metadataSourcesConflict(m) {
		log.Printf(
			"WARN: blocked metadata propagation metadata_id=%d title=%q reason=conflicting_provider_titles recovery_action=refresh_linked_items_independently",
			m.ID,
			m.Title,
		)
		return
	}

	mStore := metadataStore()
	if mStore == nil {
		return
	}
	if !quarantineMismatchedMetadataLinks(m) {
		log.Printf(
			"WARN: blocked metadata propagation metadata_id=%d title=%q reason=link_quarantine_failed recovery_action=retry_metadata_sync",
			m.ID,
			m.Title,
		)
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

func quarantineMismatchedMetadataLinks(m *model.AnimeMetadata) bool {
	if m == nil || m.ID == 0 || db.DB == nil {
		return false
	}

	var localAnimes []model.LocalAnime
	if err := db.DB.Select("id", "title", "path", "metadata_id").
		Where("metadata_id = ?", m.ID).
		Find(&localAnimes).Error; err != nil {
		log.Printf("WARN: failed to inspect local metadata links metadata_id=%d: %v", m.ID, err)
		return false
	}
	var subscriptions []model.Subscription
	if err := db.DB.Select("id", "title", "metadata_id").
		Where("metadata_id = ?", m.ID).
		Find(&subscriptions).Error; err != nil {
		log.Printf("WARN: failed to inspect subscription metadata links metadata_id=%d: %v", m.ID, err)
		return false
	}
	if len(localAnimes)+len(subscriptions) < 2 {
		return true
	}

	hasMatchingOwner := false
	for i := range localAnimes {
		if localAnimeMatchesMetadataIdentity(&localAnimes[i], m) {
			hasMatchingOwner = true
			break
		}
	}
	if !hasMatchingOwner {
		for i := range subscriptions {
			if subscriptionMatchesMetadataIdentity(&subscriptions[i], m) {
				hasMatchingOwner = true
				break
			}
		}
	}
	if !hasMatchingOwner {
		// Explicit links with fully localized titles can lack textual overlap.
		// Without one independently matching owner, detaching would be a guess.
		return true
	}

	quarantineSucceeded := true
	for i := range localAnimes {
		if localAnimeMatchesMetadataIdentity(&localAnimes[i], m) {
			continue
		}
		if err := db.DB.Model(&model.LocalAnime{}).
			Where("id = ? AND metadata_id = ?", localAnimes[i].ID, m.ID).
			Update("metadata_id", nil).Error; err != nil {
			log.Printf("WARN: failed to quarantine local metadata link local_anime_id=%d metadata_id=%d: %v", localAnimes[i].ID, m.ID, err)
			quarantineSucceeded = false
			continue
		}
		log.Printf(
			"WARN: quarantined unrelated local metadata link local_anime_id=%d title=%q metadata_id=%d metadata_title=%q recovery_action=independent_metadata_refresh",
			localAnimes[i].ID,
			localAnimes[i].Title,
			m.ID,
			m.Title,
		)
	}
	for i := range subscriptions {
		if subscriptionMatchesMetadataIdentity(&subscriptions[i], m) {
			continue
		}
		if err := db.DB.Model(&model.Subscription{}).
			Where("id = ? AND metadata_id = ?", subscriptions[i].ID, m.ID).
			Update("metadata_id", nil).Error; err != nil {
			log.Printf("WARN: failed to quarantine subscription metadata link subscription_id=%d metadata_id=%d: %v", subscriptions[i].ID, m.ID, err)
			quarantineSucceeded = false
			continue
		}
		log.Printf(
			"WARN: quarantined unrelated subscription metadata link subscription_id=%d title=%q metadata_id=%d metadata_title=%q recovery_action=independent_metadata_refresh",
			subscriptions[i].ID,
			subscriptions[i].Title,
			m.ID,
			m.Title,
		)
	}
	return quarantineSucceeded
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
