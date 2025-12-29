package jellyfin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	APIKey  string
	Token   string
	UserID  string // Active UserID for context
	Client  *http.Client
}

func NewClient(url, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(url, "/"),
		APIKey:  apiKey,
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type User struct {
	Id   string `json:"Id"`
	Name string `json:"Name"`
}

func (c *Client) GetUsers() ([]User, error) {
	data, err := c.do("GET", "/Users", nil)
	if err != nil {
		return nil, err
	}
	var users []User
	if err := json.Unmarshal(data, &users); err != nil {
		return nil, err
	}
	return users, nil
}

func (c *Client) SetToken(token string) {
	c.Token = token
}

func (c *Client) do(method, path string, body interface{}) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewBuffer(jsonData)
	}

	url := c.BaseURL + path
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	// Auth Headers
	authHeader := fmt.Sprintf(`MediaBrowser Client="AnimateAutoTool", Device="Server", DeviceId="animate-auto-tool", Version="1.0.0"`)
	if c.Token != "" {
		authHeader += fmt.Sprintf(`, Token="%s"`, c.Token)
	}
	req.Header.Set("X-Emby-Authorization", authHeader)

	if c.APIKey != "" {
		req.Header.Set("X-Emby-Token", c.APIKey)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return data, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}

	return data, nil
}

type PublicSystemInfo struct {
	LocalAddress string `json:"LocalAddress"`
	ServerName   string `json:"ServerName"`
	Version      string `json:"Version"`
	Id           string `json:"Id"`
}

func (c *Client) GetPublicInfo() (*PublicSystemInfo, error) {
	data, err := c.do("GET", "/System/Info/Public", nil)
	if err != nil {
		return nil, err
	}
	var info PublicSystemInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

type AuthenticateResponse struct {
	User struct {
		Id   string `json:"Id"`
		Name string `json:"Name"`
	} `json:"User"`
	AccessToken string `json:"AccessToken"`
}

func (c *Client) Authenticate(username, password string) (*AuthenticateResponse, error) {
	req := map[string]string{
		"Username": username,
		"Pw":       password,
	}
	data, err := c.do("POST", "/Users/AuthenticateByName", req)
	if err != nil {
		return nil, err
	}

	var resp AuthenticateResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	c.SetToken(resp.AccessToken)
	return &resp, nil
}

type LibraryOptions struct {
	EnableRealtimeMonitor                 bool   `json:"EnableRealtimeMonitor"`
	PreferredMetadataLanguage             string `json:"PreferredMetadataLanguage,omitempty"`
	MetadataCountryCode                   string `json:"MetadataCountryCode,omitempty"`
	EnablePhotos                          bool   `json:"EnablePhotos"`
	EnableChapterImageExtraction          bool   `json:"EnableChapterImageExtraction"`
	ExtractChapterImagesDuringLibraryScan bool   `json:"ExtractChapterImagesDuringLibraryScan"`
	DownloadImagesInAdvance               bool   `json:"DownloadImagesInAdvance"`
	SaveLocalMetadata                     bool   `json:"SaveLocalMetadata"`
	EnableInternetProviders               bool   `json:"EnableInternetProviders"`
	EnableAutomaticSeriesGrouping         bool   `json:"EnableAutomaticSeriesGrouping"`
	// "Prefer embedded titles over filenames"
	PreferEmbeddedTitles         bool `json:"PreferEmbeddedTitles"`
	EnableEmbeddedTitles         bool `json:"EnableEmbeddedTitles"`
	EnableEmbeddedEpisodeInfos   bool `json:"EnableEmbeddedEpisodeInfos"`
	AutomaticRefreshIntervalDays int  `json:"AutomaticRefreshIntervalDays"`
	// "Trickplay" / scrubbing previews
	EnableTrickplay bool `json:"EnableTrickplay"`
}

func (c *Client) CreateLibrary(name, path, collectionType string) error {
	// POST /Library/VirtualFolders
	// We use URL query parameters for specifying the folder details to avoid issues with body structure mismatch.
	// IMPORTANT: Parameters must be URL encoded.

	params := url.Values{}
	params.Add("name", name)
	params.Add("collectionType", collectionType)
	params.Add("paths", path) // Use "paths" key (repeated) for .NET binding
	params.Add("refreshLibrary", "true")

	// The endpoint includes the encoded query parameters
	endpoint := "/Library/VirtualFolders?" + params.Encode()

	// Body contains the LibraryOptions to ensure all settings are applied.
	// We send the options object directly as expected by [FromBody] in many controllers when mixed.
	body := LibraryOptions{
		EnableRealtimeMonitor:                 true,
		PreferredMetadataLanguage:             "zh",
		MetadataCountryCode:                   "US",
		EnablePhotos:                          true,
		EnableChapterImageExtraction:          true,
		ExtractChapterImagesDuringLibraryScan: true,
		DownloadImagesInAdvance:               true,
		SaveLocalMetadata:                     true,
		EnableInternetProviders:               true,
		EnableAutomaticSeriesGrouping:         true,
		PreferEmbeddedTitles:                  true,
		EnableEmbeddedTitles:                  true,
		EnableEmbeddedEpisodeInfos:            true,
		AutomaticRefreshIntervalDays:          30,
		EnableTrickplay:                       true,
	}

	_, err := c.do("POST", endpoint, body)
	return err
}
