package tmdb

import "testing"

func TestFixImageKeepsAbsoluteURL(t *testing.T) {
	client := &Client{}
	url := "https://image.tmdb.org/t/p/w500/example.jpg"

	if got := client.fixImage(url); got != url {
		t.Fatalf("expected absolute url to stay unchanged, got %q", got)
	}
}
