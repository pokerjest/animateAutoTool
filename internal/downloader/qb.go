package downloader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/pokerjest/animateAutoTool/internal/httpx"
)

type QBittorrentClient struct {
	client  *resty.Client
	baseURL string
	cookies []*http.Cookie // Manually store cookies
}

var ErrTorrentRejected = errors.New("qBittorrent rejected the torrent (Fails.)")

type TorrentInfo struct {
	Hash          string  `json:"hash"`
	Name          string  `json:"name"`
	State         string  `json:"state"`
	ContentPath   string  `json:"content_path"`
	SavePath      string  `json:"save_path"`
	Progress      float64 `json:"progress"`
	Size          int64   `json:"size"`
	Completed     int64   `json:"completed"`
	DownloadSpeed int64   `json:"dlspeed"`
}

func NewQBittorrentClient(baseURL string) *QBittorrentClient {
	// 确保 baseURL 不以 / 结尾
	baseURL = strings.TrimSuffix(baseURL, "/")

	jar, _ := cookiejar.New(nil)
	client := httpx.NewRestyClient(5*time.Second, "", nil).
		SetBaseURL(baseURL).
		SetHeader("Referer", baseURL).
		SetHeader("Origin", baseURL).
		SetCookieJar(jar)

	client.SetRetryCount(3).SetRetryWaitTime(2 * time.Second)

	// Middleware to log requests
	client.OnBeforeRequest(func(_ *resty.Client, req *resty.Request) error {
		log.Printf("DEBUG: Outgoing Request: %s %s", req.Method, req.URL)
		return nil
	})

	return &QBittorrentClient{
		client:  client,
		baseURL: baseURL,
	}
}

func (q *QBittorrentClient) Login(username, password string) error {
	return q.LoginContext(context.Background(), username, password)
}

func (q *QBittorrentClient) LoginContext(ctx context.Context, username, password string) error {
	// qBittorrent may bypass authentication for localhost or configured IP
	// subnets. In that mode the login endpoint can return "Ok." without a SID
	// cookie, so probe an authenticated endpoint before attempting credentials.
	if _, err := q.GetVersionContext(ctx); err == nil {
		log.Printf("DEBUG: qBittorrent API is accessible without a session cookie; using IP/localhost auth bypass")
		return nil
	}

	resp, err := httpx.NewRequest(ctx, q.client).
		SetFormData(map[string]string{
			"username": username,
			"password": password,
		}).
		Post("/api/v2/auth/login")

	if err != nil {
		return err
	}

	// Keep debug output minimal; avoid leaking credentials/cookies.
	log.Printf("DEBUG: Login Status: %s", resp.Status())
	log.Printf("DEBUG: Login Cookie Count: %d", len(resp.Cookies()))

	// Store cookies manually
	q.cookies = resp.Cookies()

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("login failed: %s", resp.Status())
	}

	// qBit 登录失败在 body 返回 "Fails."；不同版本可能带换行。
	if strings.EqualFold(strings.TrimSpace(resp.String()), "Fails.") {
		return errors.New("login failed: invalid credentials")
	}
	if !strings.EqualFold(strings.TrimSpace(resp.String()), "Ok.") {
		return fmt.Errorf("login failed: unexpected response %q", strings.TrimSpace(resp.String()))
	}

	// A standard login returns SID. If a proxy or an auth-bypass rule omits it,
	// accept the connection only after a protected endpoint succeeds.
	if _, verifyErr := q.GetVersionContext(ctx); verifyErr != nil {
		if len(q.cookies) == 0 {
			return fmt.Errorf("login returned Ok but no session cookie was issued and API verification failed: %w", verifyErr)
		}
		return fmt.Errorf("login session verification failed: %w", verifyErr)
	}
	if len(q.cookies) == 0 {
		log.Printf("DEBUG: qBittorrent login returned no cookie, but API verification succeeded; using auth bypass")
	}

	return nil
}

func (q *QBittorrentClient) AddTorrent(torrentURL, savePath, category string, paused bool) error {
	return q.AddTorrentContext(context.Background(), torrentURL, savePath, category, paused)
}

func (q *QBittorrentClient) AddTorrentContext(ctx context.Context, torrentURL, savePath, category string, paused bool) error {
	form := q.torrentOptions(savePath, category, paused)
	form["urls"] = torrentURL
	req := httpx.NewRequest(ctx, q.client).
		SetFormData(form).
		SetHeader("Referer", q.baseURL+"/"). // Add trailing slash to match browser
		SetHeader("Origin", q.baseURL)

	// Manually attach cookies
	if len(q.cookies) > 0 {
		req.SetCookies(q.cookies)
		log.Printf("DEBUG: Manually attaching %d cookies", len(q.cookies))
	}

	resp, err := req.Post("/api/v2/torrents/add")

	if err != nil {
		return err
	}

	return validateAddTorrentResponse(resp)
}

func (q *QBittorrentClient) AddTorrentFileContext(ctx context.Context, filename string, data []byte, savePath, category string, paused bool) error {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return errors.New("failed to add torrent file: filename is empty")
	}
	if len(data) == 0 {
		return errors.New("failed to add torrent file: file is empty")
	}

	req := httpx.NewRequest(ctx, q.client).
		SetFormData(q.torrentOptions(savePath, category, paused)).
		SetFileReader("torrents", filename, bytes.NewReader(data)).
		SetHeader("Referer", q.baseURL+"/").
		SetHeader("Origin", q.baseURL)
	if len(q.cookies) > 0 {
		req.SetCookies(q.cookies)
	}

	resp, err := req.Post("/api/v2/torrents/add")
	if err != nil {
		return err
	}
	return validateAddTorrentResponse(resp)
}

func (q *QBittorrentClient) torrentOptions(savePath, category string, paused bool) map[string]string {
	pausedStr := "false"
	if paused {
		pausedStr = "true"
	}
	return map[string]string{
		"savepath":    savePath,
		"category":    category,
		"paused":      pausedStr,
		"autoTMM":     "false", // 禁用自动种子管理，以便使用自定义路径
		"root_folder": "true",  // 创建根目录
	}
}

func validateAddTorrentResponse(resp *resty.Response) error {
	if resp == nil {
		return errors.New("failed to add torrent: empty qBittorrent response")
	}
	body := strings.TrimSpace(resp.String())
	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("failed to add torrent, status: %s, body: %s", resp.Status(), body)
	}
	if body == "" || strings.EqualFold(body, "Ok.") {
		return nil
	}
	if strings.EqualFold(body, "Fails.") {
		return ErrTorrentRejected
	}
	return fmt.Errorf("qBittorrent returned an unexpected add response: %q", body)
}

func (q *QBittorrentClient) Ping() error {
	return q.PingContext(context.Background())
}

func (q *QBittorrentClient) PingContext(ctx context.Context) error {
	_, err := q.GetVersionContext(ctx)
	return err
}

func (q *QBittorrentClient) GetVersion() (string, error) {
	return q.GetVersionContext(context.Background())
}

func (q *QBittorrentClient) GetVersionContext(ctx context.Context) (string, error) {
	req := httpx.NewRequest(ctx, q.client)
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

func (q *QBittorrentClient) ListTorrents() ([]TorrentInfo, error) {
	return q.ListTorrentsContext(context.Background())
}

func (q *QBittorrentClient) ListTorrentsContext(ctx context.Context) ([]TorrentInfo, error) {
	req := httpx.NewRequest(ctx, q.client).
		SetQueryParam("filter", "all").
		SetResult(&[]TorrentInfo{})
	if len(q.cookies) > 0 {
		req.SetCookies(q.cookies)
	}

	resp, err := req.Get("/api/v2/torrents/info")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("list torrents failed: %s, body: %s", resp.Status(), resp.String())
	}

	result, ok := resp.Result().(*[]TorrentInfo)
	if !ok || result == nil {
		return nil, fmt.Errorf("list torrents failed: unexpected response payload")
	}
	return *result, nil
}

func (q *QBittorrentClient) RenameFile(hash, oldPath, newPath string) error {
	return q.RenameFileContext(context.Background(), hash, oldPath, newPath)
}

// SetLocation moves a torrent through qBittorrent so its resume data and
// seeding state remain valid after automatic media organization.
func (q *QBittorrentClient) SetLocation(hash, location string) error {
	if strings.TrimSpace(hash) == "" {
		return errors.New("set location failed: missing torrent hash")
	}
	if strings.TrimSpace(location) == "" {
		return errors.New("set location failed: missing destination")
	}
	req := httpx.NewRequest(context.Background(), q.client).
		SetFormData(map[string]string{
			"hashes":   strings.TrimSpace(hash),
			"location": strings.TrimSpace(location),
		})
	if len(q.cookies) > 0 {
		req.SetCookies(q.cookies)
	}
	resp, err := req.Post("/api/v2/torrents/setLocation")
	if err != nil {
		return err
	}
	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("set location failed: %s, body: %s", resp.Status(), resp.String())
	}
	return nil
}

func (q *QBittorrentClient) RenameFileContext(ctx context.Context, hash, oldPath, newPath string) error {
	if strings.TrimSpace(hash) == "" {
		return errors.New("rename file failed: missing torrent hash")
	}
	if strings.TrimSpace(oldPath) == "" || strings.TrimSpace(newPath) == "" {
		return errors.New("rename file failed: missing old or new path")
	}

	req := httpx.NewRequest(ctx, q.client).
		SetFormData(map[string]string{
			"hash":    strings.TrimSpace(hash),
			"oldPath": filepath.ToSlash(strings.TrimSpace(oldPath)),
			"newPath": filepath.ToSlash(strings.TrimSpace(newPath)),
		})
	if len(q.cookies) > 0 {
		req.SetCookies(q.cookies)
	}

	resp, err := req.Post("/api/v2/torrents/renameFile")
	if err != nil {
		return err
	}
	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("rename file failed: %s, body: %s", resp.Status(), resp.String())
	}
	return nil
}
