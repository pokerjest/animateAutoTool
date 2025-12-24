package bangumi

import (
	"encoding/json"
	"fmt"
	"net/url"
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
