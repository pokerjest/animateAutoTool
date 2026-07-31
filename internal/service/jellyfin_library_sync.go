package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/jellyfin"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/store"
)

var ErrJellyfinNotConfigured = errors.New("jellyfin is not configured")

type JellyfinLibrarySyncResult struct {
	PendingSeries int
	MatchedSeries int
}

// RequestJellyfinLibraryRefresh asks Jellyfin to scan its configured media
// libraries. Jellyfin performs the scan asynchronously.
func RequestJellyfinLibraryRefresh(ctx context.Context) error {
	client, err := configuredJellyfinClient()
	if err != nil {
		return err
	}
	return client.RequestLibraryRefreshContext(ctx)
}

// SyncJellyfinLibraryMappings reconciles already-scanned Jellyfin series with
// local records. Provider IDs written to tvshow.nfo are preferred; a unique
// exact title is the safe fallback for older Jellyfin entries without IDs.
// This makes playback availability independent from opening an episode first.
func SyncJellyfinLibraryMappings(ctx context.Context) (JellyfinLibrarySyncResult, error) {
	return syncJellyfinLibraryMappings(ctx, nil)
}

// SyncJellyfinLibraryMappingsForAnimeIDs limits reconciliation to the local
// series touched by one completed-download batch.
func SyncJellyfinLibraryMappingsForAnimeIDs(ctx context.Context, animeIDs []uint) (JellyfinLibrarySyncResult, error) {
	if len(animeIDs) == 0 {
		return JellyfinLibrarySyncResult{}, nil
	}
	return syncJellyfinLibraryMappings(ctx, animeIDs)
}

func syncJellyfinLibraryMappings(ctx context.Context, animeIDs []uint) (JellyfinLibrarySyncResult, error) {
	result := JellyfinLibrarySyncResult{}
	if db.DB == nil {
		return result, nil
	}

	var pending []model.LocalAnime
	query := db.DB.Preload("Metadata").
		Where("(jellyfin_series_id = '' OR jellyfin_series_id IS NULL) AND EXISTS (SELECT 1 FROM local_episodes WHERE local_episodes.local_anime_id = local_animes.id AND local_episodes.deleted_at IS NULL)")
	if len(animeIDs) > 0 {
		query = query.Where("local_animes.id IN ?", animeIDs)
	}
	if err := query.Find(&pending).Error; err != nil {
		return result, err
	}
	result.PendingSeries = len(pending)
	if result.PendingSeries == 0 {
		return result, nil
	}

	client, err := configuredJellyfinClient()
	if err != nil {
		return result, err
	}
	items, err := client.ListLibrarySeriesContext(ctx)
	if err != nil {
		return result, fmt.Errorf("读取 Jellyfin 媒体库失败: %w", err)
	}
	providerIndex := buildJellyfinProviderIndex(items)
	titleIndex := buildJellyfinTitleIndex(items)
	localStore := store.NewLocalAnimeStore(db.DB)
	for i := range pending {
		anime := &pending[i]
		seriesID := matchJellyfinSeriesID(anime, providerIndex, titleIndex)
		if seriesID == "" {
			continue
		}
		if err := localStore.UpdateJellyfinSeriesID(anime.ID, seriesID); err != nil {
			return result, fmt.Errorf("保存 Jellyfin 条目关联失败: %w", err)
		}
		result.MatchedSeries++
	}
	return result, nil
}

func configuredJellyfinClient() (*jellyfin.Client, error) {
	baseURL := strings.TrimSpace(configValue(model.ConfigKeyJellyfinUrl))
	apiKey := strings.TrimSpace(configValue(model.ConfigKeyJellyfinApiKey))
	if baseURL == "" || apiKey == "" {
		return nil, ErrJellyfinNotConfigured
	}
	return jellyfin.NewClientWithProxy(baseURL, apiKey, configuredProxyURL(model.ConfigKeyProxyJellyfin)), nil
}

func buildJellyfinProviderIndex(items []jellyfin.LibrarySeries) map[string]string {
	index := make(map[string]string)
	for _, item := range items {
		if item.ID == "" {
			continue
		}
		for provider, id := range item.ProviderIDs {
			key := jellyfinProviderKey(provider, id)
			if key != "" {
				index[key] = item.ID
			}
		}
	}
	return index
}

func matchJellyfinSeriesID(anime *model.LocalAnime, providerIndex, titleIndex map[string]string) string {
	if anime == nil {
		return ""
	}
	metadata := anime.Metadata
	type candidate struct {
		provider string
		id       int
	}
	if metadata != nil {
		candidates := []candidate{{provider: "bangumi", id: metadata.BangumiID}, {provider: "tmdb", id: metadata.TMDBID}}
		if strings.EqualFold(strings.TrimSpace(metadata.DataSource), "tmdb") {
			candidates[0], candidates[1] = candidates[1], candidates[0]
		}
		for _, item := range candidates {
			if item.id == 0 {
				continue
			}
			if seriesID := providerIndex[jellyfinProviderKey(item.provider, strconv.Itoa(item.id))]; seriesID != "" {
				return seriesID
			}
		}
	}
	for _, title := range jellyfinLocalTitleCandidates(anime) {
		if seriesID := titleIndex[normalizeJellyfinTitle(title)]; seriesID != "" {
			return seriesID
		}
	}
	return ""
}

func buildJellyfinTitleIndex(items []jellyfin.LibrarySeries) map[string]string {
	index := make(map[string]string)
	duplicates := make(map[string]struct{})
	for _, item := range items {
		key := normalizeJellyfinTitle(item.Name)
		if key == "" || item.ID == "" {
			continue
		}
		if existing := index[key]; existing != "" && existing != item.ID {
			duplicates[key] = struct{}{}
			continue
		}
		index[key] = item.ID
	}
	for key := range duplicates {
		delete(index, key)
	}
	return index
}

func jellyfinLocalTitleCandidates(anime *model.LocalAnime) []string {
	values := []string{anime.Title}
	if anime.Metadata != nil {
		values = append(values,
			anime.Metadata.Title,
			anime.Metadata.TitleCN,
			anime.Metadata.TitleEN,
			anime.Metadata.TitleJP,
			anime.Metadata.BangumiTitle,
			anime.Metadata.TMDBTitle,
			anime.Metadata.AniListTitle,
		)
	}
	return values
}

func normalizeJellyfinTitle(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func jellyfinProviderKey(provider, id string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	id = strings.TrimSpace(id)
	if provider == "" || id == "" {
		return ""
	}
	return provider + ":" + id
}
