package tmdb

import (
	"encoding/json"
	"fmt"
	"strings"
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
	c.SetTimeout(30 * time.Second)
	if proxyURL != "" {
		c.SetProxy(proxyURL)
	}
	if token != "" {
		c.SetHeader("Authorization", "Bearer "+token)
	}
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
	ID           int            `json:"id"`
	Name         string         `json:"name"`
	OriginalName string         `json:"original_name"`
	Overview     string         `json:"overview"`
	PosterPath   string         `json:"poster_path"`
	BackdropPath string         `json:"backdrop_path"`
	FirstAirDate string         `json:"first_air_date"`
	VoteAverage  float64        `json:"vote_average"`
	Seasons      []SimpleSeason `json:"seasons"`
}

type SimpleSeason struct {
	AirDate      string `json:"air_date"`
	EpisodeCount int    `json:"episode_count"`
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"poster_path"`
	SeasonNumber int    `json:"season_number"`
}

// SearchTV searches for a TV show by query and returns a list of results
func (c *Client) SearchTV(query string) ([]TVShow, error) {
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

	for i := range result.Results {
		result.Results[i].PosterPath = c.fixImage(result.Results[i].PosterPath)
		result.Results[i].BackdropPath = c.fixImage(result.Results[i].BackdropPath)
	}

	return result.Results, nil
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

type SeasonDetails struct {
	ID           int       `json:"id"`
	AirDate      string    `json:"air_date"`
	Name         string    `json:"name"`
	Overview     string    `json:"overview"`
	SeasonNumber int       `json:"season_number"`
	Episodes     []Episode `json:"episodes"`
}

type Episode struct {
	ID             int     `json:"id"`
	AirDate        string  `json:"air_date"`
	EpisodeNumber  int     `json:"episode_number"`
	Name           string  `json:"name"`
	Overview       string  `json:"overview"`
	ProductionCode string  `json:"production_code"`
	SeasonNumber   int     `json:"season_number"`
	StillPath      string  `json:"still_path"`
	VoteAverage    float64 `json:"vote_average"`
}

// GetSeasonDetails fetches all episodes for a specific season
func (c *Client) GetSeasonDetails(tvID int, seasonNumber int) (*SeasonDetails, error) {
	resp, err := c.client.R().
		SetQueryParam("language", "zh-CN").
		Get(fmt.Sprintf("%s/tv/%d/season/%d", BaseURL, tvID, seasonNumber))

	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("TMDB Error: %s", resp.Status())
	}

	var season SeasonDetails
	if err := json.Unmarshal(resp.Body(), &season); err != nil {
		return nil, err
	}

	for i := range season.Episodes {
		season.Episodes[i].StillPath = c.fixImage(season.Episodes[i].StillPath)
	}

	return &season, nil
}

func (c *Client) fixImage(path string) string {
	if path == "" {
		return ""
	}
	return ImageBaseURL + path
}

// ProxyImage fetches an image from TMDB and returns the response
func (c *Client) ProxyImage(path string) (*resty.Response, error) {
	// If path contains the base URL, strip it or just use the suffix
	cleanPath := strings.TrimPrefix(path, ImageBaseURL)
	cleanPath = strings.TrimPrefix(cleanPath, "/")

	return c.client.R().
		Get("https://image.tmdb.org/t/p/original/" + cleanPath)
}
