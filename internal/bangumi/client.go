package bangumi

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

type Client struct {
	AppID       string
	AppSecret   string
	RedirectURI string
	client      *resty.Client
}

func NewClient(appID, appSecret, redirectURI string) *Client {
	return &Client{
		AppID:       appID,
		AppSecret:   appSecret,
		RedirectURI: redirectURI,
		client:      resty.New().SetTimeout(10 * time.Second),
	}
}

func (c *Client) SetProxy(proxyURL string) {
	if proxyURL != "" {
		c.client.SetProxy(proxyURL)
	}
}

func (c *Client) GetAuthorizationURL() string {
	// https://bgm.tv/oauth/authorize?client_id=[client_id]&response_type=code&redirect_uri=[redirect_uri]
	u := fmt.Sprintf("https://bgm.tv/oauth/authorize?client_id=%s&response_type=code&redirect_uri=%s",
		c.AppID, url.QueryEscape(c.RedirectURI))
	return u
}

func (c *Client) ExchangeToken(code string) (*OauthTokenResponse, error) {
	// POST https://bgm.tv/oauth/access_token
	resp, err := c.client.R().
		SetFormData(map[string]string{
			"grant_type":    "authorization_code",
			"client_id":     c.AppID,
			"client_secret": c.AppSecret,
			"code":          code,
			"redirect_uri":  c.RedirectURI,
		}).
		Post("https://bgm.tv/oauth/access_token")

	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("token exchange failed: %s", string(resp.Body()))
	}

	var tokenResp OauthTokenResponse
	if err := json.Unmarshal(resp.Body(), &tokenResp); err != nil {
		return nil, err
	}
	return &tokenResp, nil
}

func (c *Client) RefreshToken(refreshToken string) (*OauthTokenResponse, error) {
	// POST https://bgm.tv/oauth/access_token
	resp, err := c.client.R().
		SetFormData(map[string]string{
			"grant_type":    "refresh_token",
			"client_id":     c.AppID,
			"client_secret": c.AppSecret,
			"refresh_token": refreshToken,
			"redirect_uri":  c.RedirectURI,
		}).
		Post("https://bgm.tv/oauth/access_token")

	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("token refresh failed: %s", string(resp.Body()))
	}

	var tokenResp OauthTokenResponse
	if err := json.Unmarshal(resp.Body(), &tokenResp); err != nil {
		return nil, err
	}
	return &tokenResp, nil
}

func (c *Client) GetCurrentUser(accessToken string) (*UserProfile, error) {
	// GET https://api.bgm.tv/v0/me
	resp, err := c.client.R().
		SetHeader("Authorization", "Bearer "+accessToken).
		SetHeader("User-Agent", "pokerjest/animateAutoTool/1.0 (https://github.com/pokerjest/animateAutoTool)").
		Get("https://api.bgm.tv/v0/me")

	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("get profile failed: %s", string(resp.Body()))
	}

	var user UserProfile
	if err := json.Unmarshal(resp.Body(), &user); err != nil {
		return nil, err
	}

	// Fix avatar url if it starts with //
	if strings.HasPrefix(user.Avatar.Large, "//") {
		user.Avatar.Large = "https:" + user.Avatar.Large
	}
	if strings.HasPrefix(user.Avatar.Medium, "//") {
		user.Avatar.Medium = "https:" + user.Avatar.Medium
	}
	if strings.HasPrefix(user.Avatar.Small, "//") {
		user.Avatar.Small = "https:" + user.Avatar.Small
	}

	return &user, nil
}

// SearchResult represents a search result item
type SearchResult struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	NameCN string `json:"name_cn"`
	Images Images `json:"images"`
}

// SearchSubject searches for a subject by keyword and returns the first match with details
func (c *Client) SearchSubject(keyword string) (*SearchResult, error) {
	// GET https://api.bgm.tv/search/subject/{keywords}?type=2&responseGroup=small
	encodedKeyword := url.QueryEscape(keyword)
	u := fmt.Sprintf("https://api.bgm.tv/search/subject/%s?type=2&responseGroup=small&max_results=1", encodedKeyword)

	resp, err := c.client.R().
		SetHeader("User-Agent", "pokerjest/animateAutoTool/1.0 (https://github.com/pokerjest/animateAutoTool)").
		Get(u)

	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("search failed: %s", string(resp.Body()))
	}

	var result struct {
		List []SearchResult `json:"list"`
	}

	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, err
	}

	if len(result.List) > 0 {
		// Fix image urls
		item := &result.List[0]
		fixImage := func(url string) string {
			if strings.HasPrefix(url, "//") {
				return "https:" + url
			}
			return url
		}
		item.Images.Large = fixImage(item.Images.Large)
		item.Images.Common = fixImage(item.Images.Common)
		item.Images.Medium = fixImage(item.Images.Medium)
		item.Images.Small = fixImage(item.Images.Small)
		item.Images.Grid = fixImage(item.Images.Grid)
		return item, nil
	}

	return nil, nil // Not found
}

func (c *Client) GetSubject(id int) (*Subject, error) {
	// GET https://api.bgm.tv/v0/subjects/{subject_id}
	// Try V0 API first
	u := fmt.Sprintf("https://api.bgm.tv/v0/subjects/%d", id)

	resp, err := c.client.R().
		SetHeader("User-Agent", "pokerjest/animateAutoTool/1.0 (https://github.com/pokerjest/animateAutoTool)").
		Get(u)

	// Helper for fixing images
	fixImage := func(url string) string {
		if strings.HasPrefix(url, "//") {
			return "https:" + url
		}
		return url
	}

	var subject Subject
	var success bool

	// Check if V0 succeeded
	if err == nil && !resp.IsError() {
		if err := json.Unmarshal(resp.Body(), &subject); err == nil {
			success = true
		}
	}

	// Fallback to Legacy API if V0 failed
	if !success {
		// GET https://api.bgm.tv/subject/{subject_id}?responseGroup=medium
		uLegacy := fmt.Sprintf("https://api.bgm.tv/subject/%d?responseGroup=medium", id)
		resp, err = c.client.R().
			SetHeader("User-Agent", "pokerjest/animateAutoTool/1.0 (https://github.com/pokerjest/animateAutoTool)").
			Get(uLegacy)

		legacyFailed := false
		if err != nil || resp.IsError() {
			legacyFailed = true
		} else {
			if err := json.Unmarshal(resp.Body(), &subject); err != nil {
				legacyFailed = true
			} else if subject.ID == 0 {
				legacyFailed = true
			}
		}

		// Final Fallback: HTML Scraping
		if legacyFailed {
			uWeb := fmt.Sprintf("https://bgm.tv/subject/%d", id)
			// Mimic a real browser slightly more just in case
			resp, err = c.client.R().
				SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36").
				Get(uWeb)

			if err == nil && !resp.IsError() {
				scraped := scrapeSubjectHTML(string(resp.Body()), id)
				if scraped != nil {
					subject = *scraped
					success = true
				}
			}

			if !success {
				// If all methods failed, return error
				return nil, fmt.Errorf("all fetch methods failed for subject %d", id)
			}
		}
	}

	// Fix http images
	subject.Images.Large = fixImage(subject.Images.Large)
	subject.Images.Common = fixImage(subject.Images.Common)
	subject.Images.Medium = fixImage(subject.Images.Medium)
	subject.Images.Small = fixImage(subject.Images.Small)
	subject.Images.Grid = fixImage(subject.Images.Grid)

	return &subject, nil
}

func scrapeSubjectHTML(htmlContent string, id int) *Subject {
	s := &Subject{ID: id, Type: 2} // Default to Anime

	// 1. Extract Summary
	// <div id="subject_summary" class="subject_summary" property="v:summary">...</div>
	reSummary := regexp.MustCompile(`<div id="subject_summary" class="subject_summary" property="v:summary">([\s\S]*?)</div>`)
	if match := reSummary.FindStringSubmatch(htmlContent); len(match) > 1 {
		s.Summary = strings.TrimSpace(match[1])
		s.Summary = strings.ReplaceAll(s.Summary, "<br />", "\n")
		s.Summary = strings.ReplaceAll(s.Summary, "<br>", "\n")
	}

	// 2. Extract Rating
	// <span class="number" property="v:average">7.4</span>
	reRating := regexp.MustCompile(`<span class="number" property="v:average">([\d.]+)</span>`)
	if match := reRating.FindStringSubmatch(htmlContent); len(match) > 1 {
		if val, err := strconv.ParseFloat(match[1], 64); err == nil {
			s.Rating.Score = val
		}
	}

	// 3. Extract Cover Image
	// <a href="..." title="..." class="thickbox cover">
	//   <img src="//lain.bgm.tv/pic/cover/c/..." ... />
	// </a>
	// Or sometimes just in infobox
	reImg := regexp.MustCompile(`<img src="(//lain\.bgm\.tv/pic/cover/[a-z]/[^"]+)"`)
	if match := reImg.FindStringSubmatch(htmlContent); len(match) > 1 {
		base := match[1]
		if strings.Contains(base, "/c/") {
			s.Images.Common = "https:" + base
			s.Images.Large = "https:" + strings.Replace(base, "/c/", "/l/", 1)
			s.Images.Medium = "https:" + strings.Replace(base, "/c/", "/m/", 1)
			s.Images.Small = "https:" + strings.Replace(base, "/c/", "/s/", 1)
			s.Images.Grid = "https:" + strings.Replace(base, "/c/", "/g/", 1)
		} else {
			s.Images.Large = "https:" + base // Fallback
		}
	}

	// 4. Extract Title (CN name usually in infobox or header)
	// Matches <title>Title | Bangumi ...</title>
	reTitle := regexp.MustCompile(`<title>(.*?) \| Bangumi`)
	if match := reTitle.FindStringSubmatch(htmlContent); len(match) > 1 {
		s.Name = strings.TrimSpace(match[1])
		s.NameCN = s.Name
	}

	return s
}

// CollectionUpdateOptions represents fields that can be updated
type CollectionUpdateOptions struct {
	Status  int      `json:"type"`    // 1: Wish, 2: Collect, 3: Do, 4: On_Hold, 5: Dropped
	Comment string   `json:"comment"` // Optional comment
	Tags    []string `json:"tags"`    // Optional tags
	Rating  int      `json:"rating"`  // Optional rating (1-10), 0 to ignore
	Private int      `json:"private"` // 0: Public, 1: Private
}

// UpdateCollection updates the user's collection status for a subject
func (c *Client) UpdateCollection(accessToken string, subjectID int, opts CollectionUpdateOptions) error {
	// POST https://api.bgm.tv/v0/users/-/collections/{subject_id}
	u := fmt.Sprintf("https://api.bgm.tv/v0/users/-/collections/%d", subjectID)

	body := map[string]interface{}{
		"type": opts.Status,
	}

	if opts.Comment != "" {
		body["comment"] = opts.Comment
	}
	if len(opts.Tags) > 0 {
		body["tags"] = opts.Tags
	}
	if opts.Rating > 0 {
		body["rate"] = opts.Rating
	}
	if opts.Private == 1 {
		body["private"] = true
	} else {
		// Bangumi API might expect boolean or int, v0 usually boolean for private
		body["private"] = false
	}

	resp, err := c.client.R().
		SetHeader("Authorization", "Bearer "+accessToken).
		SetHeader("User-Agent", "pokerjest/animateAutoTool/1.0 (https://github.com/pokerjest/animateAutoTool)").
		SetBody(body).
		Post(u)

	if err != nil {
		return err
	}
	if resp.IsError() {
		return fmt.Errorf("update collection failed: %s", string(resp.Body()))
	}
	return nil
}

// UpdateWatchedEpisodes updates the number of watched episodes for a subject
func (c *Client) UpdateWatchedEpisodes(accessToken string, subjectID int, episodeCount int) error {
	// POST https://api.bgm.tv/subject/{subject_id}/update/watched_eps
	u := fmt.Sprintf("https://api.bgm.tv/subject/%d/update/watched_eps", subjectID)

	resp, err := c.client.R().
		SetHeader("Authorization", "Bearer "+accessToken).
		SetHeader("User-Agent", "pokerjest/animateAutoTool/1.0 (https://github.com/pokerjest/animateAutoTool)").
		SetFormData(map[string]string{
			"watched_eps": fmt.Sprintf("%d", episodeCount),
		}).
		Post(u)

	if err != nil {
		return err
	}
	// The legacy API might return a redirect or simple JSON.
	// We check for error status codes.
	if resp.IsError() {
		return fmt.Errorf("update progress failed [%d]: %s", resp.StatusCode(), string(resp.Body()))
	}

	return nil
}

// UserCollectionItem represents a subject in user's collection
type UserCollectionItem struct {
	SubjectID   int      `json:"subject_id"`
	SubjectType int      `json:"subject_type"`
	Rate        int      `json:"rate"`
	Type        int      `json:"type"`
	Comment     string   `json:"comment"`
	Tags        []string `json:"tags"`
	EpStatus    int      `json:"ep_status"`
	VolStatus   int      `json:"vol_status"`
	UpdatedAt   string   `json:"updated_at"`
	Private     bool     `json:"private"`
	Subject     Subject  `json:"subject"`
}

// GetUserCollection fetches user's collection.
// username: user ID or 'me' (if authenticated with token, but 'me' might not work for public API, usually needs 'me' or ID)
// Actually for v0, 'me' might be supported. Let's try or use ID.
// collectionType: 3 = Watching
func (c *Client) GetUserCollection(accessToken string, username string, collectionType int, limit int, offset int) ([]UserCollectionItem, error) {
	// GET https://api.bgm.tv/v0/users/{username}/collections
	u := fmt.Sprintf("https://api.bgm.tv/v0/users/%s/collections", username)

	resp, err := c.client.R().
		SetHeader("Authorization", "Bearer "+accessToken).
		SetHeader("User-Agent", "pokerjest/animateAutoTool/1.0 (https://github.com/pokerjest/animateAutoTool)").
		SetQueryParams(map[string]string{
			"subject_type": "2", // Anime
			"type":         fmt.Sprintf("%d", collectionType),
			"limit":        fmt.Sprintf("%d", limit),
			"offset":       fmt.Sprintf("%d", offset),
		}).
		Get(u)

	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("fetch collection failed: %s", string(resp.Body()))
	}

	var result struct {
		Data   []UserCollectionItem `json:"data"`
		Total  int                  `json:"total"`
		Limit  int                  `json:"limit"`
		Offset int                  `json:"offset"`
	}

	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, err
	}

	// Fix images in subjects
	for i := range result.Data {
		s := &result.Data[i].Subject
		if strings.HasPrefix(s.Images.Large, "//") {
			s.Images.Large = "https:" + s.Images.Large
		}
		if strings.HasPrefix(s.Images.Common, "//") {
			s.Images.Common = "https:" + s.Images.Common
		}
		if strings.HasPrefix(s.Images.Medium, "//") {
			s.Images.Medium = "https:" + s.Images.Medium
		}
		if strings.HasPrefix(s.Images.Small, "//") {
			s.Images.Small = "https:" + s.Images.Small
		}
		if strings.HasPrefix(s.Images.Grid, "//") {
			s.Images.Grid = "https:" + s.Images.Grid
		}
	}

	return result.Data, nil
}
