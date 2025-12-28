package downloader

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

type QBittorrentClient struct {
	client  *resty.Client
	baseURL string
	cookies []*http.Cookie // Manually store cookies
}

func NewQBittorrentClient(baseURL string) *QBittorrentClient {
	// 确保 baseURL 不以 / 结尾
	baseURL = strings.TrimSuffix(baseURL, "/")

	client := resty.New().
		SetTimeout(5*time.Second).
		SetBaseURL(baseURL).
		SetHeader("Referer", baseURL).
		SetHeader("Origin", baseURL).
		SetHeader("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36").
		SetCookieJar(nil) // Keep Jar just in case, but we will manual override

	client.SetRetryCount(3).SetRetryWaitTime(2 * time.Second)

	// Middleware to log requests
	client.OnBeforeRequest(func(c *resty.Client, req *resty.Request) error {
		log.Printf("DEBUG: Outgoing Request: %s %s", req.Method, req.URL)
		log.Printf("DEBUG: Outgoing Headers: %v", req.Header)
		if c.GetClient().Jar != nil {
			u, _ := url.Parse(baseURL)
			cookies := c.GetClient().Jar.Cookies(u)
			log.Printf("DEBUG: Jar Cookies for %s: %v", baseURL, cookies)
		}
		return nil
	})

	return &QBittorrentClient{
		client:  client,
		baseURL: baseURL,
	}
}

func (q *QBittorrentClient) Login(username, password string) error {
	resp, err := q.client.R().
		SetFormData(map[string]string{
			"username": username,
			"password": password,
		}).
		Post("/api/v2/auth/login")

	if err != nil {
		return err
	}

	// Log everything
	log.Printf("DEBUG: Login Status: %s", resp.Status())
	log.Printf("DEBUG: Login Body: %s", resp.String())
	log.Printf("DEBUG: Login Headers: %v", resp.Header())
	log.Printf("DEBUG: Login Cookies: %v", resp.Cookies())

	// Store cookies manually
	q.cookies = resp.Cookies()

	// qBit 登录失败在 body 返回 "Fails."
	if resp.String() == "Fails." {
		return errors.New("login failed: invalid credentials")
	}

	return nil
}

func (q *QBittorrentClient) AddTorrent(torrentURL, savePath, category string, paused bool) error {
	pausedStr := "false"
	if paused {
		pausedStr = "true"
	}

	req := q.client.R().
		SetFormData(map[string]string{
			"urls":        torrentURL,
			"savepath":    savePath,
			"category":    category,
			"paused":      pausedStr,
			"autoTMM":     "false", // 禁用自动种子管理，以便使用自定义路径
			"root_folder": "true",  // 创建根目录
		}).
		SetHeader("Referer", q.baseURL+"/"). // Add trailing slash to match browser
		SetHeader("Origin", q.baseURL)

	// Manually attach cookies
	if len(q.cookies) > 0 {
		req.SetCookies(q.cookies)
		log.Printf("DEBUG: Manually attaching %d cookies", len(q.cookies))
	} else {
		log.Printf("WARNING: No cookies to attach! Login might have failed silently or didn't set cookies.")
	}

	resp, err := req.Post("/api/v2/torrents/add")

	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("failed to add torrent, status: %s, body: %s", resp.Status(), resp.String())
	}

	return nil
}

func (q *QBittorrentClient) Ping() error {
	_, err := q.GetVersion()
	return err
}

func (q *QBittorrentClient) GetVersion() (string, error) {
	req := q.client.R()
	// Manually attach cookies provided by Login
	if len(q.cookies) > 0 {
		req.SetCookies(q.cookies)
	}

	resp, err := req.Get("/api/v2/app/version")
	if err != nil {
		return "", err
	}
	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("ping failed: %s, body: %s", resp.Status(), resp.String())
	}
	return resp.String(), nil
}
