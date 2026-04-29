package httpx

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

const DefaultUserAgent = "AnimateAutoTool/0.5.0 (+https://github.com/pokerjest/animateAutoTool)"

func NewRestyClient(timeout time.Duration, proxyURL string, headers map[string]string) *resty.Client {
	client := resty.New().SetTimeout(timeout)
	if strings.TrimSpace(proxyURL) != "" {
		client.SetProxy(strings.TrimSpace(proxyURL))
	}
	client.SetHeader("User-Agent", DefaultUserAgent)
	for key, value := range headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		client.SetHeader(key, value)
	}
	return client
}

func NewRequest(ctx context.Context, client *resty.Client) *resty.Request {
	req := client.R()
	if ctx != nil {
		req.SetContext(ctx)
	}
	return req
}

func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}
