package api

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/pokerjest/animateAutoTool/internal/anilist"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/tmdb"
)

// MetadataSourceCandidate is a real item returned by one of the configured
// metadata providers. AI may only select IDs present in this snapshot.
type MetadataSourceCandidate struct {
	ID      int    `json:"id"`
	Source  string `json:"source"`
	Name    string `json:"name"`
	NameCN  string `json:"name_cn"`
	Image   string `json:"image"`
	Summary string `json:"summary"`
	AirDate string `json:"air_date"`
}

// MetadataMatchCandidate groups corresponding real records from Bangumi,
// TMDB and AniList. A source can be nil when it is not configured, unavailable
// or no safe match was found.
type MetadataMatchCandidate struct {
	Title    string                   `json:"title"`
	Summary  string                   `json:"summary"`
	AirDate  string                   `json:"air_date"`
	Image    string                   `json:"image"`
	Bangumi  *MetadataSourceCandidate `json:"bangumi,omitempty"`
	TMDB     *MetadataSourceCandidate `json:"tmdb,omitempty"`
	AniList  *MetadataSourceCandidate `json:"anilist,omitempty"`
	Score    float64                  `json:"score"`
	Evidence []string                 `json:"evidence"`
}

type MetadataSourceStatus struct {
	Configured bool   `json:"configured"`
	Searched   bool   `json:"searched"`
	Error      string `json:"error,omitempty"`
	Count      int    `json:"count"`
}

type MetadataMatchSearchResult struct {
	Query        string                          `json:"query"`
	Source       string                          `json:"source"`
	SourceID     int                             `json:"source_id,omitempty"`
	SourceStatus map[string]MetadataSourceStatus `json:"source_status"`
	Candidates   []MetadataMatchCandidate        `json:"candidates"`
}

type metadataSearchOptions struct {
	Query    string
	Source   string
	SourceID int
}

type metadataSourceResults struct {
	items  []MetadataSourceCandidate
	status MetadataSourceStatus
}

// searchMetadataMatchCandidates searches all approved metadata sources and
// groups records using deterministic title/year similarity. It never accepts a
// URL or an ID supplied by the model without looking it up through a provider.
func searchMetadataMatchCandidates(ctx context.Context, opts metadataSearchOptions) (*MetadataMatchSearchResult, error) {
	query := strings.TrimSpace(opts.Query)
	source := strings.ToLower(strings.TrimSpace(opts.Source))
	if query == "" && opts.SourceID <= 0 {
		return nil, fmt.Errorf("搜索关键词不能为空")
	}
	if source == "" {
		source = SourceBangumi
	}
	if source != SourceBangumi && source != SourceTMDB && source != SourceAniList {
		return nil, fmt.Errorf("不支持的元数据来源 %q", source)
	}

	// A selected source ID is treated as a seed: its details provide title
	// variants for the other two providers.
	var seed *MetadataSourceCandidate
	if opts.SourceID > 0 {
		seed, _ = fetchMetadataSourceCandidate(ctx, source, opts.SourceID)
		if seed != nil {
			query = firstNonEmpty(seed.NameCN, seed.Name, query)
		}
	}
	if query == "" {
		return nil, fmt.Errorf("无法从所选来源读取标题")
	}

	variants := titleVariants(query)
	if seed != nil {
		variants = appendUniqueStrings(variants, seed.Name, seed.NameCN)
	}

	results := map[string]metadataSourceResults{
		SourceBangumi: {status: MetadataSourceStatus{Configured: true}},
		SourceTMDB:    {status: MetadataSourceStatus{Configured: configValue(model.ConfigKeyTMDBToken) != ""}},
		SourceAniList: {status: MetadataSourceStatus{Configured: configValue(model.ConfigKeyAniListToken) != ""}},
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, sourceName := range []string{SourceBangumi, SourceTMDB, SourceAniList} {
		sourceName := sourceName
		if !results[sourceName].status.Configured {
			sourceResult := results[sourceName]
			sourceResult.status.Error = "该来源尚未配置"
			results[sourceName] = sourceResult
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			found, err := searchMetadataSource(ctx, sourceName, variants)
			mu.Lock()
			defer mu.Unlock()
			sourceResult := metadataSourceResults{
				items: found,
				status: MetadataSourceStatus{
					Configured: true,
					Searched:   err == nil,
					Count:      len(found),
				},
			}
			if err != nil {
				sourceResult.status.Error = sanitizeMetadataError(err)
			}
			results[sourceName] = sourceResult
		}()
	}
	wg.Wait()
	if seed != nil {
		sourceResult := results[source]
		items := sourceResult.items
		found := false
		for _, item := range items {
			if item.ID == seed.ID {
				found = true
				break
			}
		}
		if !found {
			sourceResult.items = append([]MetadataSourceCandidate{*seed}, sourceResult.items...)
			sourceResult.status.Searched = true
			sourceResult.status.Count++
			results[source] = sourceResult
		}
	}

	grouped := groupMetadataCandidates(query, results, seed)
	statuses := make(map[string]MetadataSourceStatus, len(results))
	for name, result := range results {
		statuses[name] = result.status
	}
	return &MetadataMatchSearchResult{
		Query: query, Source: source, SourceID: opts.SourceID,
		SourceStatus: statuses, Candidates: grouped,
	}, nil
}

func searchMetadataSource(ctx context.Context, source string, variants []string) ([]MetadataSourceCandidate, error) {
	seen := map[int]bool{}
	items := make([]MetadataSourceCandidate, 0, 12)
	var firstErr error
	for _, variant := range variants {
		if strings.TrimSpace(variant) == "" {
			continue
		}
		var found []MetadataSourceCandidate
		var err error
		switch source {
		case SourceBangumi:
			client := bangumi.NewClient("", "", "")
			applyProxyToBangumiClient(client)
			rows, searchErr := client.SearchSubjectsContext(ctx, variant)
			err = searchErr
			if err == nil {
				for _, row := range rows {
					found = append(found, MetadataSourceCandidate{
						ID: row.ID, Source: SourceBangumi, Name: row.Name, NameCN: row.NameCN,
						Image: row.Images.Large, Summary: "", AirDate: "",
					})
				}
			}
		case SourceTMDB:
			token := configValue(model.ConfigKeyTMDBToken)
			if token == "" {
				return items, fmt.Errorf("还没有配置 TMDB Token")
			}
			rows, searchErr := tmdb.NewClient(token, configuredProxyURL(model.ConfigKeyProxyTMDB)).SearchTVContext(ctx, variant)
			err = searchErr
			if err == nil {
				for _, row := range rows {
					found = append(found, MetadataSourceCandidate{
						ID: row.ID, Source: SourceTMDB, Name: row.OriginalName, NameCN: row.Name,
						Image: row.PosterPath, Summary: row.Overview, AirDate: row.FirstAirDate,
					})
				}
			}
		case SourceAniList:
			token := configValue(model.ConfigKeyAniListToken)
			if token == "" {
				return items, fmt.Errorf("还没有配置 AniList Token")
			}
			rows, searchErr := anilist.NewClient(token, configuredProxyURL(model.ConfigKeyProxyAniList)).SearchAnimeResultsContext(ctx, variant, 10)
			err = searchErr
			if err == nil {
				for _, row := range rows {
					name := firstNonEmpty(row.Title.Romaji, row.Title.Native, row.Title.English)
					image := firstNonEmpty(row.CoverImage.ExtraLarge, row.CoverImage.Large, row.CoverImage.Medium)
					found = append(found, MetadataSourceCandidate{
						ID: row.ID, Source: SourceAniList, Name: name, NameCN: firstNonEmpty(row.Title.English, row.Title.Native, row.Title.Romaji),
						Image: image, Summary: row.Description,
					})
				}
			}
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
		for _, item := range found {
			if item.ID > 0 && !seen[item.ID] {
				seen[item.ID] = true
				items = append(items, item)
			}
		}
		if len(items) >= 12 {
			break
		}
	}
	if len(items) > 12 {
		items = items[:12]
	}
	if len(items) > 0 {
		// A transient failure for one title variant should not hide successful
		// candidates obtained from another variant.
		firstErr = nil
	}
	return items, firstErr
}

func fetchMetadataSourceCandidate(ctx context.Context, source string, id int) (*MetadataSourceCandidate, error) {
	switch source {
	case SourceBangumi:
		client := bangumi.NewClient("", "", "")
		applyProxyToBangumiClient(client)
		row, err := client.GetSubjectContext(ctx, id)
		if err != nil || row == nil {
			return nil, err
		}
		return &MetadataSourceCandidate{ID: row.ID, Source: source, Name: row.Name, NameCN: row.NameCN, Image: row.Images.Large, Summary: row.Summary, AirDate: row.Date}, nil
	case SourceTMDB:
		token := configValue(model.ConfigKeyTMDBToken)
		if token == "" {
			return nil, fmt.Errorf("还没有配置 TMDB Token")
		}
		row, err := tmdb.NewClient(token, configuredProxyURL(model.ConfigKeyProxyTMDB)).GetTVDetailsContext(ctx, id)
		if err != nil || row == nil {
			return nil, err
		}
		return &MetadataSourceCandidate{ID: row.ID, Source: source, Name: row.OriginalName, NameCN: row.Name, Image: row.PosterPath, Summary: row.Overview, AirDate: row.FirstAirDate}, nil
	case SourceAniList:
		token := configValue(model.ConfigKeyAniListToken)
		if token == "" {
			return nil, fmt.Errorf("还没有配置 AniList Token")
		}
		row, err := anilist.NewClient(token, configuredProxyURL(model.ConfigKeyProxyAniList)).GetAnimeDetailsContext(ctx, id)
		if err != nil || row == nil {
			return nil, err
		}
		return &MetadataSourceCandidate{
			ID: row.ID, Source: source, Name: firstNonEmpty(row.Title.Romaji, row.Title.Native, row.Title.English),
			NameCN: firstNonEmpty(row.Title.English, row.Title.Native, row.Title.Romaji),
			Image:  firstNonEmpty(row.CoverImage.ExtraLarge, row.CoverImage.Large, row.CoverImage.Medium), Summary: row.Description,
		}, nil
	default:
		return nil, fmt.Errorf("不支持的元数据来源 %q", source)
	}
}

func groupMetadataCandidates(query string, results map[string]metadataSourceResults, seed *MetadataSourceCandidate) []MetadataMatchCandidate {
	base := make([]MetadataSourceCandidate, 0, 12)
	if seed != nil {
		base = append(base, *seed)
	}
	for _, source := range []string{SourceBangumi, SourceTMDB, SourceAniList} {
		base = append(base, results[source].items...)
	}
	seen := map[string]bool{}
	groups := make([]MetadataMatchCandidate, 0, len(base))
	for _, anchor := range base {
		key := anchor.Source + ":" + strconv.Itoa(anchor.ID)
		if seen[key] {
			continue
		}
		seen[key] = true
		group := MetadataMatchCandidate{Title: firstNonEmpty(anchor.NameCN, anchor.Name), Summary: anchor.Summary, AirDate: anchor.AirDate, Image: anchor.Image, Score: titleScore(query, candidateTitles(anchor)), Evidence: []string{}}
		setSourceCandidate(&group, anchor)
		for _, source := range []string{SourceBangumi, SourceTMDB, SourceAniList} {
			if source == anchor.Source {
				continue
			}
			best, score := bestSourceCandidate(anchor, results[source].items)
			if best != nil && (score >= 0.45 || source == SourceBangumi && anchor.Source == SourceTMDB || source == SourceTMDB && anchor.Source == SourceBangumi) {
				setSourceCandidate(&group, *best)
				if score >= 0.8 {
					group.Evidence = append(group.Evidence, source+" 标题高度相似")
				}
			}
		}
		if group.Bangumi != nil && group.TMDB != nil && group.AniList != nil {
			group.Evidence = append(group.Evidence, "三个来源均有真实候选")
		}
		if len(group.Evidence) == 0 {
			group.Evidence = append(group.Evidence, "候选由真实来源搜索返回，需人工核对")
		}
		groups = append(groups, group)
		if len(groups) >= 12 {
			break
		}
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Score > groups[j].Score })
	return groups
}

func bestSourceCandidate(anchor MetadataSourceCandidate, items []MetadataSourceCandidate) (*MetadataSourceCandidate, float64) {
	var best *MetadataSourceCandidate
	bestScore := 0.0
	for i := range items {
		score := titleScore(anchor.Name+" "+anchor.NameCN, candidateTitles(items[i]))
		if y1, y2 := yearOf(anchor.AirDate), yearOf(items[i].AirDate); y1 != "" && y1 == y2 {
			score += 0.15
		}
		if score > bestScore {
			bestScore = score
			copy := items[i]
			best = &copy
		}
	}
	return best, minFloat(bestScore, 1)
}

func setSourceCandidate(group *MetadataMatchCandidate, item MetadataSourceCandidate) {
	copy := item
	switch item.Source {
	case SourceBangumi:
		group.Bangumi = &copy
	case SourceTMDB:
		group.TMDB = &copy
	case SourceAniList:
		group.AniList = &copy
	}
	if group.Title == "" {
		group.Title = firstNonEmpty(item.NameCN, item.Name)
	}
	if group.Summary == "" {
		group.Summary = item.Summary
	}
	if group.AirDate == "" {
		group.AirDate = item.AirDate
	}
	if group.Image == "" {
		group.Image = item.Image
	}
}

func candidateTitles(item MetadataSourceCandidate) []string {
	return []string{item.Name, item.NameCN}
}

func titleVariants(query string) []string {
	parts := appendUniqueStrings(nil, query)
	clean := strings.TrimSpace(strings.Join(strings.FieldsFunc(query, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSpace(r)
	}), " "))
	parts = appendUniqueStrings(parts, clean)
	return parts
}

func appendUniqueStrings(dst []string, values ...string) []string {
	seen := make(map[string]bool, len(dst))
	for _, item := range dst {
		if key := strings.ToLower(strings.TrimSpace(item)); key != "" {
			seen[key] = true
		}
	}
	for _, item := range values {
		if key := strings.ToLower(strings.TrimSpace(item)); key != "" && !seen[key] {
			dst = append(dst, strings.TrimSpace(item))
			seen[key] = true
		}
	}
	return dst
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeMatchTitle(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func titleScore(query string, candidates []string) float64 {
	base := normalizeMatchTitle(query)
	if base == "" {
		return 0
	}
	best := 0.0
	for _, candidate := range candidates {
		normalized := normalizeMatchTitle(candidate)
		if normalized == "" {
			continue
		}
		score := 0.0
		switch {
		case normalized == base:
			score = 1
		case strings.Contains(normalized, base) || strings.Contains(base, normalized):
			score = 0.82
		default:
			score = tokenOverlap(base, normalized)
		}
		if score > best {
			best = score
		}
	}
	return best
}

func tokenOverlap(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	shorter, longer := a, b
	if len(shorter) > len(longer) {
		shorter, longer = longer, shorter
	}
	common := 0
	for _, r := range shorter {
		if strings.ContainsRune(longer, r) {
			common++
		}
	}
	return float64(common) / float64(len([]rune(shorter)))
}

func yearOf(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 4 {
		for i := 0; i <= len(value)-4; i++ {
			if _, err := strconv.Atoi(value[i : i+4]); err == nil {
				return value[i : i+4]
			}
		}
	}
	return ""
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func sanitizeMetadataError(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}
