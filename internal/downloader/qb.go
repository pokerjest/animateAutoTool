package downloader

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

type QBittorrentClient struct {
	client  *resty.Client
	baseURL string
}

func NewQBittorrentClient(baseURL string) *QBittorrentClient {
	// 确保 baseURL 不以 / 结尾
	baseURL = strings.TrimSuffix(baseURL, "/")

	client := resty.New().
		SetTimeout(30 * time.Second).
		SetBaseURL(baseURL)

	// 自动重试
	client.SetRetryCount(3).SetRetryWaitTime(2 * time.Second)

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

	// qBit 登录失败在 body 返回 "Fails."
	if resp.String() == "Fails." {
		return errors.New("login failed: invalid credentials")
	}

	// 虽然 resty 会自动处理 cookie，但确认一下
	// 只要 resp header Set-Cookie 有 SID 即可
	return nil
}

func (q *QBittorrentClient) AddTorrent(torrentURL, savePath, category string, paused bool) error {
	pausedStr := "false"
	if paused {
		pausedStr = "true"
	}

	// 这里的 AddTorrent 使用 urls 参数 (磁力链或 HTTP 链接)
	// 如果需要上传种子文件，需要用 SetFileReader
	resp, err := q.client.R().
		SetFormData(map[string]string{
			"urls":        torrentURL,
			"savepath":    savePath,
			"category":    category,
			"paused":      pausedStr,
			"autoTMM":     "false", // 禁用自动种子管理，以便使用自定义路径
			"root_folder": "true",  // 创建根目录
		}).
		Post("/api/v2/torrents/add")

	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("failed to add torrent, status: %s", resp.Status())
	}

	return nil
}

func (q *QBittorrentClient) Ping() error {
	_, err := q.GetVersion()
	return err
}

func (q *QBittorrentClient) GetVersion() (string, error) {
	resp, err := q.client.R().Get("/api/v2/app/version")
	if err != nil {
		return "", err
	}
	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("ping failed: %s", resp.Status())
	}
	return resp.String(), nil
}
