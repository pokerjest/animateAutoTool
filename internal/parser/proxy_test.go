package parser

import (
	"net/http"
	"net/url"
	"testing"
)

func TestMikanParserSetProxyNormalizesHostPort(t *testing.T) {
	client := NewMikanParser()
	if err := client.SetProxy("127.0.0.1:7890"); err != nil {
		t.Fatalf("configure proxy: %v", err)
	}
	transport, ok := client.client.GetClient().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected HTTP transport, got %T", client.client.GetClient().Transport)
	}
	proxyURL, err := transport.Proxy(&http.Request{URL: &url.URL{Scheme: "https", Host: "mikanani.me"}})
	if err != nil {
		t.Fatalf("resolve proxy: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:7890" {
		t.Fatalf("unexpected proxy URL: %v", proxyURL)
	}
}
