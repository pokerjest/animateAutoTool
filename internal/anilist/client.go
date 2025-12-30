package anilist

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	GraphQLEndpoint = "https://graphql.anilist.co"
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
	if token != "" {
		c.SetHeader("Authorization", "Bearer "+token)
	}
	c.SetHeader("Content-Type", "application/json")
	c.SetHeader("Accept", "application/json")

	return &Client{
		client: c,
		Token:  token,
	}
}

type MediaTitle struct {
	Romaji  string `json:"romaji"`
	English string `json:"english"`
	Native  string `json:"native"`
}

type CoverImage struct {
	ExtraLarge string `json:"extraLarge"`
	Large      string `json:"large"`
	Medium     string `json:"medium"`
}

type Media struct {
	ID             int             `json:"id"`
	Title          MediaTitle      `json:"title"`
	CoverImage     CoverImage      `json:"coverImage"`
	Description    string          `json:"description"`
	AverageScore   int             `json:"averageScore"`
	MediaListEntry *MediaListEntry `json:"mediaListEntry"`
}

type MediaListEntry struct {
	Progress int    `json:"progress"`
	Status   string `json:"status"`
}

type PageData struct {
	Media []Media `json:"media"`
}

type SearchResponseData struct {
	Page PageData `json:"Page"`
}

type SearchResponse struct {
	Data   SearchResponseData `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type MediaResponseData struct {
	Media Media `json:"Media"`
}

type MediaResponse struct {
	Data   MediaResponseData `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *Client) SearchAnime(query string) (*Media, error) {
	graphqlQuery := `
	query ($search: String) {
	  Page(page: 1, perPage: 1) {
	    media(search: $search, type: ANIME, sort: SEARCH_MATCH) {
	      id
	      title {
	        romaji
	        english
	        native
	      }
	      coverImage {
	        extraLarge
	      }
	      description(asHtml: false)
	      averageScore
	    }
	  }
	}
	`
	payload := map[string]interface{}{
		"query": graphqlQuery,
		"variables": map[string]interface{}{
			"search": query,
		},
	}

	resp, err := c.client.R().
		SetBody(payload).
		Post(GraphQLEndpoint)

	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("AniList API Error: %s", resp.Status())
	}

	var result SearchResponse
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, err
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("AniList GraphQL Error: %s", result.Errors[0].Message)
	}

	if len(result.Data.Page.Media) > 0 {
		return &result.Data.Page.Media[0], nil
	}

	return nil, nil
}

func (c *Client) GetAnimeDetails(id int) (*Media, error) {
	graphqlQuery := `
	query ($id: Int) {
	  Media(id: $id, type: ANIME) {
	    id
	    title {
	      romaji
	      english
	      native
	    }
	    coverImage {
	      extraLarge
	    }
	    description(asHtml: false)
	    averageScore
	  }
	}
	`
	payload := map[string]interface{}{
		"query": graphqlQuery,
		"variables": map[string]interface{}{
			"id": id,
		},
	}

	resp, err := c.client.R().
		SetBody(payload).
		Post(GraphQLEndpoint)

	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("AniList API Error: %s", resp.Status())
	}

	var result MediaResponse
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, err
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("AniList GraphQL Error: %s", result.Errors[0].Message)
	}

	return &result.Data.Media, nil
}

// GetMediaListEntry fetches the user's progress for a specific media
func (c *Client) GetMediaListEntry(mediaID int) (*MediaListEntry, error) {
	graphqlQuery := `
	query ($id: Int) {
	  Media(id: $id) {
	    mediaListEntry {
	      progress
	      status
	    }
	  }
	}
	`
	payload := map[string]interface{}{
		"query": graphqlQuery,
		"variables": map[string]interface{}{
			"id": mediaID,
		},
	}

	resp, err := c.client.R().
		SetBody(payload).
		Post(GraphQLEndpoint)

	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("AniList API Error: %s", resp.Status())
	}

	var result MediaResponse
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, err
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("AniList GraphQL Error: %s", result.Errors[0].Message)
	}

	return result.Data.Media.MediaListEntry, nil
}
