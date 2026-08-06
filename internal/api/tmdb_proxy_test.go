package api

import (
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/tmdb"
)

func TestIsAllowedTMDBProxyImage(t *testing.T) {
	tests := []struct {
		contentType string
		bodySize    int
		allowed     bool
	}{
		{contentType: "image/jpeg", bodySize: 1, allowed: true},
		{contentType: " Image/PNG; charset=binary ", bodySize: tmdb.MaxProxyImageBytes, allowed: true},
		{contentType: "application/json", bodySize: 1, allowed: false},
		{contentType: "image/jpeg", bodySize: tmdb.MaxProxyImageBytes + 1, allowed: false},
		{contentType: "image/jpeg", bodySize: -1, allowed: false},
	}
	for _, test := range tests {
		if got := isAllowedTMDBProxyImage(test.contentType, test.bodySize); got != test.allowed {
			t.Fatalf("isAllowedTMDBProxyImage(%q, %d) = %v, want %v", test.contentType, test.bodySize, got, test.allowed)
		}
	}
}
