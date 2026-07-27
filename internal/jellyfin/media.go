package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// MediaLibrary is the provider-neutral subset of a Jellyfin virtual folder
// needed by the media workspace.
type MediaLibrary struct {
	ID              string `json:"Id"`
	Name            string `json:"Name"`
	CollectionType  string `json:"CollectionType"`
	PrimaryImageTag string `json:"PrimaryImageTag"`
	ChildCount      int    `json:"ChildCount"`
}

// MediaItem is intentionally close to Jellyfin's item shape, while keeping
// only fields used by the application. The API layer maps it to its own DTO.
type MediaItem struct {
	ID                string            `json:"Id"`
	Name              string            `json:"Name"`
	Type              string            `json:"Type"`
	ParentID          string            `json:"ParentId"`
	SeriesID          string            `json:"SeriesId"`
	SeriesName        string            `json:"SeriesName"`
	Overview          string            `json:"Overview"`
	ProductionYear    int               `json:"ProductionYear"`
	PremiereDate      string            `json:"PremiereDate"`
	CommunityRating   float64           `json:"CommunityRating"`
	RunTimeTicks      int64             `json:"RunTimeTicks"`
	IndexNumber       int               `json:"IndexNumber"`
	ParentIndexNumber int               `json:"ParentIndexNumber"`
	ChildCount        int               `json:"ChildCount"`
	IsFolder          bool              `json:"IsFolder"`
	PrimaryImageTag   string            `json:"ImageTags.Primary"`
	ImageTags         map[string]string `json:"ImageTags"`
	Genres            []string          `json:"Genres"`
	UserData          ItemUserData      `json:"UserData"`
	ProviderIDs       map[string]string `json:"ProviderIds"`
}

type MediaQuery struct {
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

type MediaPage struct {
	Items []MediaItem
	Total int
}

func (c *Client) GetMediaFoldersContext(ctx context.Context) ([]MediaLibrary, error) {
	data, err := c.doContext(ctx, "GET", "/Library/MediaFolders", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Items []MediaLibrary `json:"Items"`
	}
	if err := json.Unmarshal(data, &result); err == nil && result.Items != nil {
		return result.Items, nil
	}
	var legacy []MediaLibrary
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, err
	}
	return legacy, nil
}

func (c *Client) ListItemsContext(ctx context.Context, query MediaQuery) (MediaPage, error) {
	if query.Limit <= 0 {
		query.Limit = 48
	}
	if query.Limit > 200 {
		query.Limit = 200
	}
	if query.StartIndex < 0 {
		query.StartIndex = 0
	}
	if len(query.IncludeItemTypes) == 0 {
		query.IncludeItemTypes = []string{"Series", "Movie"}
	}
	params := url.Values{}
	if c.UserID != "" {
		params.Set("UserId", c.UserID)
	}
	if query.ParentID != "" {
		params.Set("ParentId", query.ParentID)
	}
	if query.SearchTerm != "" {
		params.Set("SearchTerm", query.SearchTerm)
	}
	params.Set("IncludeItemTypes", strings.Join(query.IncludeItemTypes, ","))
	if len(query.Filters) > 0 {
		params.Set("Filters", strings.Join(query.Filters, ","))
	}
	params.Set("SortBy", valueOr(query.SortBy, "SortName"))
	params.Set("SortOrder", valueOr(query.SortOrder, "Ascending"))
	params.Set("StartIndex", strconv.Itoa(query.StartIndex))
	params.Set("Limit", strconv.Itoa(query.Limit))
	params.Set("Recursive", strconv.FormatBool(query.Recursive))
	params.Set("Fields", "Overview,Genres,ProviderIds,UserData,ChildCount,ProductionYear,PremiereDate,ParentId,ParentIndexNumber,IndexNumber,SeriesId,SeriesName,PrimaryImageAspectRatio")
	data, err := c.doContext(ctx, "GET", "/Users/"+url.PathEscape(c.UserID)+"/Items?"+params.Encode(), nil)
	if err != nil {
		return MediaPage{}, err
	}
	var result struct {
		Items            []MediaItem `json:"Items"`
		TotalRecordCount int         `json:"TotalRecordCount"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return MediaPage{}, err
	}
	return MediaPage{Items: result.Items, Total: result.TotalRecordCount}, nil
}

func (c *Client) GetMediaItemContext(ctx context.Context, itemID string) (*MediaItem, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return nil, fmt.Errorf("media item id is required")
	}
	params := url.Values{}
	params.Set("Fields", "Overview,Genres,ProviderIds,UserData,MediaSources,MediaStreams,ChildCount,ProductionYear,PremiereDate,ParentId,ParentIndexNumber,IndexNumber,SeriesId,SeriesName,PrimaryImageAspectRatio")
	data, err := c.doContext(ctx, "GET", "/Users/"+url.PathEscape(c.UserID)+"/Items/"+url.PathEscape(itemID)+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var result MediaItem
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) ListChildrenContext(ctx context.Context, parentID string, includeTypes []string) (MediaPage, error) {
	return c.ListItemsContext(ctx, MediaQuery{
		ParentID:         parentID,
		IncludeItemTypes: includeTypes,
		SortBy:           "ParentIndexNumber,IndexNumber,SortName",
		Recursive:        false,
		Limit:            200,
	})
}

func (c *Client) ListResumeContext(ctx context.Context, limit int) (MediaPage, error) {
	if limit <= 0 {
		limit = 24
	}
	params := url.Values{}
	params.Set("Fields", "Overview,Genres,UserData,ChildCount,ProductionYear,PremiereDate,ParentId,ParentIndexNumber,IndexNumber,SeriesId,SeriesName,PrimaryImageAspectRatio")
	params.Set("MediaTypes", "Video")
	params.Set("Limit", strconv.Itoa(limit))
	params.Set("StartIndex", "0")
	params.Set("Recursive", "true")
	data, err := c.doContext(ctx, "GET", "/Users/"+url.PathEscape(c.UserID)+"/Items/Resume?"+params.Encode(), nil)
	if err != nil {
		return MediaPage{}, err
	}
	var result struct {
		Items            []MediaItem `json:"Items"`
		TotalRecordCount int         `json:"TotalRecordCount"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return MediaPage{}, err
	}
	return MediaPage{Items: result.Items, Total: result.TotalRecordCount}, nil
}

func (c *Client) ListFavoritesContext(ctx context.Context, limit int) (MediaPage, error) {
	return c.ListItemsContext(ctx, MediaQuery{
		Filters:          []string{"IsFavorite"},
		IncludeItemTypes: []string{"Series", "Movie"},
		Recursive:        true,
		Limit:            limit,
	})
}

func (c *Client) GetImageContext(ctx context.Context, itemID string) ([]byte, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return nil, fmt.Errorf("media item id is required")
	}
	return c.doContext(ctx, "GET", "/Items/"+url.PathEscape(itemID)+"/Images/Primary", nil)
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
