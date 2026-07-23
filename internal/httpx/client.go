package httpx

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

const DefaultUserAgent = "AnimateAutoTool/0.5.0 (+https://github.com/pokerjest/animateAutoTool)"

// NormalizeProxyURL accepts the common host:port shorthand used by desktop
// proxy applications and returns a URL that net/http can use consistently.
// Only forward proxy schemes supported by Go's HTTP transport are accepted.
func NormalizeProxyURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "http://" + trimmed
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("代理地址格式无效: %w", err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return "", fmt.Errorf("不支持的代理协议 %q，请使用 http、https 或 socks5", parsed.Scheme)
	}
	if parsed.Hostname() == "" {
		return "", fmt.Errorf("代理地址缺少主机名")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("代理地址不能包含路径、查询参数或片段")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Path = ""
	return parsed.String(), nil
}

func NewRestyClient(timeout time.Duration, proxyURL string, headers map[string]string) *resty.Client {
	client := resty.New().SetTimeout(timeout)
	client.SetTransport(newHTTPTransport(proxyURL))
	if normalized, err := NormalizeProxyURL(proxyURL); err == nil && normalized != "" {
		client.SetProxy(normalized)
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

	if normalized, err := NormalizeProxyURL(proxyURL); err == nil && normalized != "" {
		if parsed, err := url.Parse(normalized); err == nil {
			transport.Proxy = http.ProxyURL(parsed)
		}
	}

	return transport
}
