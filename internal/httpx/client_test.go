package httpx

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestNewRestyClientDisablesImplicitEnvironmentProxy(t *testing.T) {
	client := NewRestyClient(5*time.Second, "", nil)
	transport, ok := client.GetClient().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected custom http transport, got %T", client.GetClient().Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("expected resty client without explicit proxy to ignore environment proxy settings")
	}
}

func TestNewHTTPClientDisablesImplicitEnvironmentProxy(t *testing.T) {
	client := NewHTTPClient(5 * time.Second)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected custom http transport, got %T", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("expected default http client without explicit proxy to ignore environment proxy settings")
	}
}

func TestNewHTTPClientAllowsZeroTimeoutForStreaming(t *testing.T) {
	client := NewHTTPClient(0)
	if client.Timeout != 0 {
		t.Fatalf("expected zero timeout for streaming client, got %s", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected custom http transport, got %T", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("expected streaming http client to ignore environment proxy settings")
	}
}

func TestNewHTTPClientWithProxyUsesExplicitProxy(t *testing.T) {
	client := NewHTTPClientWithProxy(5*time.Second, "http://127.0.0.1:8080")
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected custom http transport, got %T", client.Transport)
	}
	if transport.Proxy == nil {
		t.Fatal("expected explicit proxy client to set transport proxy")
	}
	reqURL, err := url.Parse("https://api.example.com")
	if err != nil {
		t.Fatalf("failed to parse request url: %v", err)
	}
	proxyURL, err := transport.Proxy(&http.Request{URL: reqURL})
	if err != nil {
		t.Fatalf("failed to resolve proxy: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:8080" {
		t.Fatalf("expected explicit proxy to be preserved, got %v", proxyURL)
	}
}

func TestNormalizeProxyURLAcceptsDesktopProxyShorthand(t *testing.T) {
	got, err := NormalizeProxyURL("127.0.0.1:7890")
	if err != nil {
		t.Fatalf("expected shorthand proxy to normalize: %v", err)
	}
	if got != "http://127.0.0.1:7890" {
		t.Fatalf("unexpected normalized proxy: %q", got)
	}

	client := NewHTTPClientWithProxy(5*time.Second, "127.0.0.1:7890")
	transport := client.Transport.(*http.Transport)
	proxyURL, err := transport.Proxy(&http.Request{URL: &url.URL{Scheme: "https", Host: "api.example.com"}})
	if err != nil {
		t.Fatalf("resolve normalized proxy: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != got {
		t.Fatalf("expected shorthand proxy to be installed, got %v", proxyURL)
	}
}

func TestNormalizeProxyURLRejectsUnsupportedOrPathURLs(t *testing.T) {
	for _, raw := range []string{"ftp://127.0.0.1:21", "http://127.0.0.1:7890/path", "http://"} {
		if _, err := NormalizeProxyURL(raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestNewRestyClientUsesExplicitProxy(t *testing.T) {
	client := NewRestyClient(5*time.Second, "socks5://127.0.0.1:7891", nil)
	transport, ok := client.GetClient().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected custom http transport, got %T", client.GetClient().Transport)
	}
	proxyURL, err := transport.Proxy(&http.Request{URL: &url.URL{Scheme: "https", Host: "api.example.com"}})
	if err != nil {
		t.Fatalf("resolve Resty proxy: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "socks5://127.0.0.1:7891" {
		t.Fatalf("expected explicit Resty proxy, got %v", proxyURL)
	}
}
