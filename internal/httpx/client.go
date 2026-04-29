package httpx

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

const DefaultUserAgent = "AnimateAutoTool/0.5.0 (+https://github.com/pokerjest/animateAutoTool)"

func NewRestyClient(timeout time.Duration, proxyURL string, headers map[string]string) *resty.Client {
	client := resty.New().SetTimeout(timeout)
	client.SetTransport(newHTTPTransport(proxyURL))
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
	return NewHTTPClientWithProxy(timeout, "")
}

func NewHTTPClientWithProxy(timeout time.Duration, proxyURL string) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: newHTTPTransport(proxyURL),
	}
}

func newHTTPTransport(proxyURL string) *http.Transport {
	base, _ := http.DefaultTransport.(*http.Transport)
	if base == nil {
		base = &http.Transport{}
	}

	transport := base.Clone()
	transport.Proxy = nil
	transport.ProxyConnectHeader = nil
	transport.DialContext = (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext

	if trimmed := strings.TrimSpace(proxyURL); trimmed != "" {
		if parsed, err := url.Parse(trimmed); err == nil {
			transport.Proxy = http.ProxyURL(parsed)
		}
	}

	return transport
}
