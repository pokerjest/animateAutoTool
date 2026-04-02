package launcher

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagedJellyfinConflictReasonDetectsExistingJellyfin(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/System/Info/Public", r.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"ServerName": "NAVI",
			"Version":    "10.11.5",
		}))
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	reason := managedJellyfinConflictReason(listener.Addr().String(), server.URL)

	assert.Contains(t, reason, "NAVI 10.11.5")
	assert.Contains(t, reason, listener.Addr().String())
}

func TestManagedJellyfinConflictReasonDetectsNonJellyfinPortOccupant(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	reason := managedJellyfinConflictReason(listener.Addr().String(), server.URL)

	assert.Equal(t, "address "+listener.Addr().String()+" is already in use by another process", reason)
}

func TestManagedAListConflictReasonDetectsExistingAList(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/public/settings", r.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"code":    200,
			"message": "success",
			"data": map[string]any{
				"site_title": "AList",
				"version":    "v3.55.0",
			},
		}))
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	reason := managedAListConflictReason(listener.Addr().String(), server.URL)

	assert.Contains(t, reason, "AList v3.55.0")
	assert.Contains(t, reason, listener.Addr().String())
}

func TestManagedAListConflictReasonDetectsNonAListPortOccupant(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	reason := managedAListConflictReason(listener.Addr().String(), server.URL)

	assert.Equal(t, "address "+listener.Addr().String()+" is already in use by another process", reason)
}
