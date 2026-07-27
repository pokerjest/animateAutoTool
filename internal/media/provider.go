package media

import "context"

type ProviderCapabilities struct {
	Libraries bool `json:"libraries"`
	Search    bool `json:"search"`
	Episodes  bool `json:"episodes"`
	Progress  bool `json:"progress"`
	Favorites bool `json:"favorites"`
	Images    bool `json:"images"`
}

type Library struct {
	ID             string
	Name           string
	CollectionType string
	ItemCount      int
}

type UserState struct {
	Played          bool
	Favorite        bool
	ResumeTicks     int64
	ProgressPercent float64
}

type Item struct {
	ID                string
	Name              string
	Type              string
	Overview          string
	ProductionYear    int
	PremiereDate      string
	CommunityRating   float64
	RuntimeTicks      int64
	IndexNumber       int
	ParentIndexNumber int
	ParentID          string
	SeriesID          string
	SeriesName        string
	ChildCount        int
	Genres            []string
	UserState         UserState
}

type Query struct {
	ParentID         string
	SearchTerm       string
	IncludeItemTypes []string
	Filters          []string
	SortBy           string
	SortOrder        string
	StartIndex       int
	Limit            int
	Recursive        bool
}

type Page struct {
	Items []Item
	Total int
}

type PlaybackInfo struct {
	Provider        string
	ItemID          string
	DirectStreamURL string
	ResumeTicks     int64
	RuntimeTicks    int64
	Played          bool
	Favorite        bool
}

type ProgressInput struct {
	Event         string
	Ticks         int64
	DurationTicks int64
}

type UserStateInput struct {
	Played   *bool
	Favorite *bool
}

type MediaProvider interface {
	ID() string
	Capabilities() ProviderCapabilities
	ListLibraries(ctx context.Context) ([]Library, error)
	ListItems(ctx context.Context, query Query) (Page, error)
	GetItem(ctx context.Context, itemID string) (*Item, error)
	ListChildren(ctx context.Context, itemID string, includeTypes []string) (Page, error)
	GetPlayback(ctx context.Context, itemID string) (*PlaybackInfo, error)
	ListContinueWatching(ctx context.Context, limit int) (Page, error)
	ListFavorites(ctx context.Context, limit int) (Page, error)
	UpdateProgress(ctx context.Context, itemID string, input ProgressInput) error
	UpdateUserState(ctx context.Context, itemID string, input UserStateInput) error
	GetImage(ctx context.Context, itemID string) ([]byte, error)
}
