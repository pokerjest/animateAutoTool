package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBoundedPaginationHandlesExtremeAndInvalidValues(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		defaultSize  int
		maxSize      int
		expectedPage int
		expectedSize int
	}{
		{name: "max int page", query: "page=9223372036854775807&page_size=9223372036854775807", defaultSize: 25, maxSize: 200, expectedPage: maxPaginationPage, expectedSize: 200},
		{name: "negative page", query: "page=-1&page_size=10", defaultSize: 25, maxSize: 200, expectedPage: 1, expectedSize: 10},
		{name: "zero page size", query: "page=2&page_size=0", defaultSize: 25, maxSize: 200, expectedPage: 2, expectedSize: 25},
		{name: "invalid values", query: "page=not-a-number&page_size=bad", defaultSize: 50, maxSize: 100, expectedPage: 1, expectedSize: 50},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest(http.MethodGet, "/?"+test.query, nil)
			page, pageSize := boundedPagination(context, test.defaultSize, test.maxSize)
			if page != test.expectedPage || pageSize != test.expectedSize {
				t.Fatalf("boundedPagination() = (%d, %d), want (%d, %d)", page, pageSize, test.expectedPage, test.expectedSize)
			}
		})
	}
}

func TestPaginationOffsetDoesNotOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if got := paginationOffset(maxInt, 2); got != maxInt {
		t.Fatalf("overflowing offset = %d, want saturation at %d", got, maxInt)
	}
	if got := paginationOffset(-1, 20); got != 0 {
		t.Fatalf("invalid page offset = %d, want 0", got)
	}
	if got := paginationOffset(2, 20); got != 20 {
		t.Fatalf("normal offset = %d, want 20", got)
	}
}
