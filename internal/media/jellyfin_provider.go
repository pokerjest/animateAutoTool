package media

import (
	"context"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/jellyfin"
)

type JellyfinProvider struct {
	client    *jellyfin.Client
	directURL string
	apiKey    string
}

func NewJellyfinProvider(client *jellyfin.Client, directURL, apiKey string) *JellyfinProvider {
	return &JellyfinProvider{client: client, directURL: strings.TrimRight(directURL, "/"), apiKey: apiKey}
}

func (p *JellyfinProvider) ID() string {
	return "jellyfin"
}

func (p *JellyfinProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{Libraries: true, Search: true, Episodes: true, Progress: true, Favorites: true, Images: true}
}

func (p *JellyfinProvider) ListLibraries(ctx context.Context) ([]Library, error) {
	items, err := p.client.GetMediaFoldersContext(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]Library, 0, len(items))
	for _, item := range items {
		switch strings.ToLower(strings.TrimSpace(item.CollectionType)) {
		case "", "movies", "tvshows", "mixed", "homevideos", "musicvideos":
		default:
			continue
		}
		result = append(result, Library{ID: item.ID, Name: item.Name, CollectionType: item.CollectionType, ItemCount: item.ChildCount})
	}
	return result, nil
}

func (p *JellyfinProvider) ListItems(ctx context.Context, query Query) (Page, error) {
	page, err := p.client.ListItemsContext(ctx, jellyfin.MediaQuery{
		ParentID: query.ParentID, SearchTerm: query.SearchTerm,
		IncludeItemTypes: query.IncludeItemTypes, Filters: query.Filters,
		SortBy: query.SortBy, SortOrder: query.SortOrder,
		StartIndex: query.StartIndex, Limit: query.Limit, Recursive: query.Recursive,
	})
	if err != nil {
		return Page{}, err
	}
	return convertPage(page), nil
}

func (p *JellyfinProvider) GetItem(ctx context.Context, itemID string) (*Item, error) {
	item, err := p.client.GetMediaItemContext(ctx, itemID)
	if err != nil {
		return nil, err
	}
	result := convertItem(*item)
	return &result, nil
}

func (p *JellyfinProvider) ListChildren(ctx context.Context, itemID string, includeTypes []string) (Page, error) {
	page, err := p.client.ListChildrenContext(ctx, itemID, includeTypes)
	if err != nil {
		return Page{}, err
	}
	return convertPage(page), nil
}

func (p *JellyfinProvider) GetPlayback(ctx context.Context, itemID string) (*PlaybackInfo, error) {
	item, err := p.client.GetMediaItemContext(ctx, itemID)
	if err != nil {
		return nil, err
	}
	directURL := ""
	if p.directURL != "" {
		directURL = jellyfin.NewClient(p.directURL, p.apiKey).GetStreamURL(itemID)
	}
	return &PlaybackInfo{
		Provider: p.ID(), ItemID: itemID, DirectStreamURL: directURL,
		ResumeTicks: item.UserData.PlaybackPositionTicks, RuntimeTicks: item.RunTimeTicks,
		Played: item.UserData.Played, Favorite: item.UserData.IsFavorite,
	}, nil
}

func (p *JellyfinProvider) ListContinueWatching(ctx context.Context, limit int) (Page, error) {
	page, err := p.client.ListResumeContext(ctx, limit)
	if err != nil {
		return Page{}, err
	}
	return convertPage(page), nil
}

func (p *JellyfinProvider) ListFavorites(ctx context.Context, limit int) (Page, error) {
	page, err := p.client.ListFavoritesContext(ctx, limit)
	if err != nil {
		return Page{}, err
	}
	return convertPage(page), nil
}

func (p *JellyfinProvider) UpdateProgress(_ context.Context, itemID string, input ProgressInput) error {
	if input.Event == "ended" {
		return p.client.MarkPlayed(itemID)
	}
	return p.client.UpdateProgress(itemID, input.Ticks)
}

func (p *JellyfinProvider) UpdateUserState(_ context.Context, itemID string, input UserStateInput) error {
	if input.Played != nil {
		var err error
		if *input.Played {
			err = p.client.MarkPlayed(itemID)
		} else {
			err = p.client.UnmarkPlayed(itemID)
		}
		if err != nil {
			return err
		}
	}
	if input.Favorite != nil {
		return p.client.SetFavorite(itemID, *input.Favorite)
	}
	return nil
}

func (p *JellyfinProvider) GetImage(ctx context.Context, itemID string) ([]byte, error) {
	return p.client.GetImageContext(ctx, itemID)
}

func convertPage(page jellyfin.MediaPage) Page {
	items := make([]Item, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, convertItem(item))
	}
	return Page{Items: items, Total: page.Total}
}

func convertItem(item jellyfin.MediaItem) Item {
	progress := 0.0
	if item.RunTimeTicks > 0 {
		progress = min(100, float64(item.UserData.PlaybackPositionTicks)/float64(item.RunTimeTicks)*100)
	}
	return Item{
		ID: item.ID, Name: item.Name, Type: item.Type, Overview: item.Overview,
		ProductionYear: item.ProductionYear, PremiereDate: item.PremiereDate,
		CommunityRating: item.CommunityRating, RuntimeTicks: item.RunTimeTicks,
		IndexNumber: item.IndexNumber, ParentIndexNumber: item.ParentIndexNumber,
		ParentID: item.ParentID, SeriesID: item.SeriesID, SeriesName: item.SeriesName,
		ChildCount: item.ChildCount, Genres: item.Genres,
		UserState: UserState{
			Played: item.UserData.Played, Favorite: item.UserData.IsFavorite,
			ResumeTicks: item.UserData.PlaybackPositionTicks, ProgressPercent: progress,
		},
	}
}
