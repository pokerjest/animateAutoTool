package ai

import (
	"net/http"
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
