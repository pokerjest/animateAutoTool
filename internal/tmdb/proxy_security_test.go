package tmdb

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func rewriteTMDBImageTransport(target string) http.RoundTripper {
	base := http.DefaultTransport
	serverURL, _ := http.NewRequest(http.MethodGet, target, nil)
	return roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "image.tmdb.org" {
			request.URL.Scheme = serverURL.URL.Scheme
			request.URL.Host = serverURL.URL.Host
		}
		return base.RoundTrip(request)
	})
}

func TestCleanProxyImagePathRejectsTraversalAndHostInjection(t *testing.T) {
	for _, raw := range []string{
		"",
		"../poster.jpg",
		"nested/../poster.jpg",
		"nested\\poster.jpg",
		"https://evil.example/poster.jpg",
		"//evil.example/poster.jpg",
		"https://image.tmdb.org.evil.example/t/p/w500/poster.jpg",
	} {
		if _, err := cleanProxyImagePath(raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}

	for raw, expected := range map[string]string{
		"/poster.jpg":       "poster.jpg",
		"nested/poster.jpg": "nested/poster.jpg",
		"https://image.tmdb.org/t/p/w500/nested/poster.jpg": "nested/poster.jpg",
	} {
		got, err := cleanProxyImagePath(raw)
		if err != nil {
			t.Fatalf("cleanProxyImagePath(%q): %v", raw, err)
		}
		if got != expected {
			t.Fatalf("cleanProxyImagePath(%q) = %q, want %q", raw, got, expected)
		}
	}
}

func TestProxyImageContextEnforcesResponseLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(bytes.Repeat([]byte("x"), MaxProxyImageBytes+1))
	}))
	defer server.Close()

	client := NewClient("", "")
	client.client.SetTransport(rewriteTMDBImageTransport(server.URL))
	if _, err := client.ProxyImageContext(context.Background(), "poster.jpg"); err == nil {
		t.Fatal("expected oversized image response to be rejected")
	}
}

func TestProxyImageContextHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()

	client := NewClient("", "")
	client.client.SetTransport(rewriteTMDBImageTransport(server.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := client.ProxyImageContext(ctx, "poster.jpg"); err == nil {
		t.Fatal("expected canceled image request to fail")
	}
	if ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("context error = %v, want deadline exceeded", ctx.Err())
	}
}
