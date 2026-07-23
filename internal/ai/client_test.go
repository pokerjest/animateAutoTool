package ai

import (
	"net/http"
	"net/url"
	"testing"
)

func TestNewClientDisablesImplicitEnvironmentProxy(t *testing.T) {
	client := NewClient("https://api.openai.com/v1", "test-key", "gpt-4o-mini")
	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected custom http transport, got %T", client.httpClient.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("expected AI client to ignore environment proxy settings by default")
	}
}

func TestNewClientWithProxyUsesExplicitProxy(t *testing.T) {
	client := NewClientWithProxy("https://api.openai.com/v1", "test-key", "gpt-4o-mini", "127.0.0.1:7890")
	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected custom http transport, got %T", client.httpClient.Transport)
	}
	proxyURL, err := transport.Proxy(&http.Request{URL: &url.URL{Scheme: "https", Host: "api.openai.com"}})
	if err != nil {
		t.Fatalf("resolve proxy: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:7890" {
		t.Fatalf("unexpected proxy URL: %v", proxyURL)
	}
}
