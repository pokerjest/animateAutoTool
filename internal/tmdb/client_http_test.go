package tmdb

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func rewriteTMDBTransport(target string) http.RoundTripper {
	base := http.DefaultTransport
	serverURL, _ := http.NewRequest(http.MethodGet, target, nil)
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.themoviedb.org" {
			r.URL.Scheme = serverURL.URL.Scheme
			r.URL.Host = serverURL.URL.Host
		}
		return base.RoundTrip(r)
	})
}

func TestSearchTVNormalizesRelativeImages(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/3/search/tv" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"id":7,"name":"Test Show","poster_path":"/poster.jpg","backdrop_path":"https://example.com/backdrop.jpg"}]}`))
	}))
	defer server.Close()

	client := NewClient("token", "")
	client.client.SetTransport(rewriteTMDBTransport(server.URL))

	results, err := client.SearchTV("test")
	if err != nil {
		t.Fatalf("search tv failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if got := results[0].PosterPath; got != ImageBaseURL+"/poster.jpg" {
		t.Fatalf("unexpected poster path: %q", got)
	}
	if got := results[0].BackdropPath; got != "https://example.com/backdrop.jpg" {
		t.Fatalf("unexpected backdrop path: %q", got)
	}
}

func TestGetSeasonDetailsNormalizesStillPaths(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/3/tv/42/season/2" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":2,"season_number":2,"episodes":[{"episode_number":1,"still_path":"/still.jpg"}]}`))
	}))
	defer server.Close()

	client := NewClient("token", "")
	client.client.SetTransport(rewriteTMDBTransport(server.URL))

	season, err := client.GetSeasonDetails(42, 2)
	if err != nil {
		t.Fatalf("get season details failed: %v", err)
	}
	if len(season.Episodes) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(season.Episodes))
	}
	if got := season.Episodes[0].StillPath; got != ImageBaseURL+"/still.jpg" {
		t.Fatalf("unexpected still path: %q", got)
	}
}
