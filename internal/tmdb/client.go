package tmdb

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	BaseURL      = "https://api.themoviedb.org/3"
	ImageBaseURL = "https://image.tmdb.org/t/p/w500" // Use w500 for posters
)

type Client struct {
	client *resty.Client
	Token  string
}

func NewClient(token string, proxyURL string) *Client {
	c := resty.New()
	c.SetTimeout(10 * time.Second)
	if proxyURL != "" {
		c.SetProxy(proxyURL)
	}
	c.SetHeader("Authorization", "Bearer "+token)
	c.SetHeader("Content-Type", "application/json")

	return &Client{
		client: c,
		Token:  token,
	}
}

type SearchResponse struct {
	Results []TVShow `json:"results"`
}

type TVShow struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	OriginalName string  `json:"original_name"`
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"poster_path"`
	BackdropPath string  `json:"backdrop_path"`
	FirstAirDate string  `json:"first_air_date"`
	VoteAverage  float64 `json:"vote_average"`
}

// SearchTV searches for a TV show by query
func (c *Client) SearchTV(query string) (*TVShow, error) {
	// Search in Chinese first (zh-CN)
	resp, err := c.client.R().
		SetQueryParam("query", query).
		SetQueryParam("language", "zh-CN").
		Get(BaseURL + "/search/tv")

	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("TMDB Error: %s", resp.Status())
	}

	var result SearchResponse
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, err
	}

	if len(result.Results) > 0 {
		show := result.Results[0]
		show.PosterPath = c.fixImage(show.PosterPath)
		show.BackdropPath = c.fixImage(show.BackdropPath)
		return &show, nil
	}

	return nil, nil // Not found
}

// GetTVDetails fetches details including overview for a specific ID
func (c *Client) GetTVDetails(id int) (*TVShow, error) {
	resp, err := c.client.R().
		SetQueryParam("language", "zh-CN").
		Get(fmt.Sprintf("%s/tv/%d", BaseURL, id))

	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("TMDB Error: %s", resp.Status())
	}

	var show TVShow
	if err := json.Unmarshal(resp.Body(), &show); err != nil {
		return nil, err
	}

	show.PosterPath = c.fixImage(show.PosterPath)
	show.BackdropPath = c.fixImage(show.BackdropPath)
	return &show, nil
}

func (c *Client) fixImage(path string) string {
	if path == "" {
		return ""
	}
	return ImageBaseURL + path
}
