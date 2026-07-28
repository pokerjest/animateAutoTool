package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExpectedVideoProxyCancellation(t *testing.T) {
	t.Run("transport reports context cancellation", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/video", nil)
		if !isExpectedVideoProxyCancellation(req, context.Canceled) {
			t.Fatal("expected context cancellation to be ignored")
		}
	})

	t.Run("request context was canceled", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/video", nil)
		ctx, cancel := context.WithCancel(req.Context())
		cancel()
		req = req.WithContext(ctx)
		if !isExpectedVideoProxyCancellation(req, errors.New("write failed")) {
			t.Fatal("expected a canceled client request to be ignored")
		}
	})

	t.Run("reverse proxy abort handler", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/video", nil)
		if !isExpectedVideoProxyCancellation(req, http.ErrAbortHandler) {
			t.Fatal("expected reverse proxy abort handler to be ignored")
		}
	})

	t.Run("real upstream failure", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/video", nil)
		if isExpectedVideoProxyCancellation(req, errors.New("upstream connection refused")) {
			t.Fatal("expected a real upstream failure to remain reportable")
		}
	})
}
