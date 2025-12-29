package jellyfin

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// GetItemInfo fetches details for an item (resume position, media sources)
func (c *Client) GetItemInfo(itemId string) (map[string]interface{}, error) {
	endpoint := fmt.Sprintf("/Users/%s/Items/%s", c.UserID, itemId)
	resp, err := c.do("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetStreamURL generates a direct stream URL for the item
func (c *Client) GetStreamURL(itemId string) string {
	// Simple Direct Play URL
	// /Videos/{Id}/stream?static=true&mediaSourceId={Id}&deviceId={DeviceId}&api_key={Token}
	// We simplify for now, standard stream endpoint often redirects to suitable transcoding/direct

	// Better: /Videos/{Id}/stream.{Container}
	// Or: /Videos/{Id}/stream?static=true

	// For ArtPlayer, we usually want a direct file stream if possible, or HLS.
	// Let's use the universal stream endpoint.

	base := fmt.Sprintf("%s/Videos/%s/stream", c.BaseURL, itemId)
	u, _ := url.Parse(base)
	q := u.Query()
	q.Set("static", "true") // Attempt direct play
	q.Set("api_key", c.APIKey)
	u.RawQuery = q.Encode()
	return u.String()
}

type ItemResponse struct {
	Items []struct {
		Id          string
		Name        string
		ProviderIds map[string]string `json:"ProviderIds"`
		UserData    struct {
			PlaybackPositionTicks int64
			Played                bool
		}
	}
}

// GetItemByProviderID finds a Series/Item by its provider ID (e.g. Bangumi, TMDB)
func (c *Client) GetItemByProviderID(provider, id string) (string, error) {
	// /Items?Recursive=true&AnyProviderIdEquals={provider}.{id}
	// Note: Jellyfin provider key might be lowercased or specific. Bangumi plugin might use "Bangumi"?
	// Standard ones: "Tmdb", "Imdb".
	// For generic NFO, it might be just "bangumi" if we used <uniqueid type="bangumi">.

	params := url.Values{}
	params.Set("Recursive", "true")
	params.Set("AnyProviderIdEquals", fmt.Sprintf("%s.%s", provider, id))
	params.Set("IncludeItemTypes", "Series,Movie") // Optimization
	params.Set("Fields", "ProviderIds")            // Essential for verification!

	endpoint := "/Items?" + params.Encode()
	resp, err := c.do("GET", endpoint, nil)
	if err != nil {
		return "", err
	}

	var res ItemResponse
	if err := json.Unmarshal(resp, &res); err != nil {
		return "", err
	}

	if len(res.Items) == 0 {
		return "", fmt.Errorf("item not found for %s:%s", provider, id)
	}

	// Strict Verification
	// Jellyfin might return loose matches, so we must check ProviderIds
	for _, item := range res.Items {
		for k, v := range item.ProviderIds {
			if strings.EqualFold(k, provider) && v == id {
				return item.Id, nil
			}
		}
	}

	return "", fmt.Errorf("strict match not found for %s:%s locally", provider, id)
}

// GetEpisodeFromSeries finds an episode by SeriesId + Season + Index
func (c *Client) GetEpisodeFromSeries(seriesId string, season, index int) (string, int64, error) {
	// /Items?ParentId={SeriesId}&Recursive=true&IncludeItemTypes=Episode&ParentIndexNumber={Season}&IndexNumber={Index}
	// Note: ParentIndexNumber is Season (usually)

	params := url.Values{}
	params.Set("ParentId", seriesId)
	params.Set("Recursive", "true")
	params.Set("IncludeItemTypes", "Episode")
	params.Set("ParentIndexNumber", fmt.Sprintf("%d", season))
	params.Set("IndexNumber", fmt.Sprintf("%d", index))
	// Need UserData for Resume
	params.Set("Fields", "UserData")

	endpoint := fmt.Sprintf("/Users/%s/Items?%s", c.UserID, params.Encode())
	resp, err := c.do("GET", endpoint, nil)
	if err != nil {
		return "", 0, err
	}

	var res ItemResponse
	if err := json.Unmarshal(resp, &res); err != nil {
		return "", 0, err
	}

	if len(res.Items) == 0 {
		return "", 0, fmt.Errorf("episode S%dE%d not found", season, index)
	}

	return res.Items[0].Id, res.Items[0].UserData.PlaybackPositionTicks, nil
}

// MarkPlayed marks an item as played
func (c *Client) MarkPlayed(itemId string) error {
	// POST /Users/{UserId}/PlayedItems/{ItemId}
	endpoint := fmt.Sprintf("/Users/%s/PlayedItems/%s", c.UserID, itemId)
	_, err := c.do("POST", endpoint, nil)
	return err
}

// UnmarkPlayed marks an item as unplayed
func (c *Client) UnmarkPlayed(itemId string) error {
	// DELETE /Users/{UserId}/PlayedItems/{ItemId}
	endpoint := fmt.Sprintf("/Users/%s/PlayedItems/%s", c.UserID, itemId)
	_, err := c.do("DELETE", endpoint, nil)
	return err
}

// UpdateProgress reports playback progress (for resume points)
func (c *Client) UpdateProgress(itemId string, ticks int64) error {
	// POST /Sessions/Playing/Progress
	// Body: { ItemId: "...", PositionTicks: ... , EventName: "TimeUpdate" }
	// Needs generic Session handling?
	// Actually simple Progress report usually works if we provide ItemId.

	req := map[string]interface{}{
		"ItemId":        itemId,
		"PositionTicks": ticks,
		"EventName":     "TimeUpdate", // or "Pause"
	}

	_, err := c.do("POST", "/Sessions/Playing/Progress", req)
	return err
}

// GetSeriesEpisodes returns all episodes for a series with UserData (Played/Resume)
func (c *Client) GetSeriesEpisodes(seriesId string) ([]struct {
	Id       string
	Index    int // Episode Index
	Season   int // Season Index
	Overview string
	Rating   float64
	AirDate  string // PremiereDate
	Duration int64  // RunTimeTicks
	UserData struct {
		Played                bool
		PlaybackPositionTicks int64
	}
}, error) {
	// /Items?ParentId={SeriesId}&Recursive=true&IncludeItemTypes=Episode&Fields=UserData,ParentId,Overview,CommunityRating,PremiereDate,RunTimeTicks

	params := url.Values{}
	params.Set("ParentId", seriesId)
	params.Set("Recursive", "true")
	params.Set("IncludeItemTypes", "Episode")
	params.Set("Fields", "UserData,ParentIndexNumber,IndexNumber,Overview,CommunityRating,PremiereDate,RunTimeTicks")

	endpoint := fmt.Sprintf("/Users/%s/Items?%s", c.UserID, params.Encode())
	resp, err := c.do("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var res struct {
		Items []struct {
			Id                string
			IndexNumber       int
			ParentIndexNumber int
			Overview          string
			CommunityRating   float64
			PremiereDate      string
			RunTimeTicks      int64
			UserData          struct {
				Played                bool
				PlaybackPositionTicks int64
			}
		}
	}
	if err := json.Unmarshal(resp, &res); err != nil {
		return nil, err
	}

	// Convert to simpler struct
	var result []struct {
		Id       string
		Index    int
		Season   int
		Overview string
		Rating   float64
		AirDate  string
		Duration int64
		UserData struct {
			Played                bool
			PlaybackPositionTicks int64
		}
	}

	for _, item := range res.Items {
		result = append(result, struct {
			Id       string
			Index    int
			Season   int
			Overview string
			Rating   float64
			AirDate  string
			Duration int64
			UserData struct {
				Played                bool
				PlaybackPositionTicks int64
			}
		}{
			Id:       item.Id,
			Index:    item.IndexNumber,
			Season:   item.ParentIndexNumber,
			Overview: item.Overview,
			Rating:   item.CommunityRating,
			AirDate:  item.PremiereDate,
			Duration: item.RunTimeTicks,
			UserData: item.UserData,
		})
	}

	return result, nil
}
