package jellyfin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func configureZeroConfigRetriesForTest(t *testing.T) {
	t.Helper()

	prevReadyAttempts := serverReadyAttempts
	prevReadyDelay := serverReadyPollDelay
	prevAuthAttempts := authRetryAttempts
	prevAuthDelay := authRetryDelay
	prevStartupAttempts := startupUserRetryAttempts
	prevStartupDelay := startupUserRetryDelay

	serverReadyAttempts = 2
	serverReadyPollDelay = 0
	authRetryAttempts = 2
	authRetryDelay = 0
	startupUserRetryAttempts = 2
	startupUserRetryDelay = 0

	t.Cleanup(func() {
		serverReadyAttempts = prevReadyAttempts
		serverReadyPollDelay = prevReadyDelay
		authRetryAttempts = prevAuthAttempts
		authRetryDelay = prevAuthDelay
		startupUserRetryAttempts = prevStartupAttempts
		startupUserRetryDelay = prevStartupDelay
	})
}

func TestStartupWizardCompleted(t *testing.T) {
	t.Parallel()

	completed := true
	notCompleted := false

	assert.True(t, startupWizardCompleted(&PublicSystemInfo{StartupWizardCompleted: &completed}))
	assert.False(t, startupWizardCompleted(&PublicSystemInfo{StartupWizardCompleted: &notCompleted}))
	assert.False(t, startupWizardCompleted(&PublicSystemInfo{}))
	assert.False(t, startupWizardCompleted(nil))
}

func TestShouldRetryStartupUser(t *testing.T) {
	t.Parallel()

	assert.False(t, shouldRetryStartupUser(&APIError{StatusCode: http.StatusUnauthorized}))
	assert.False(t, shouldRetryStartupUser(&APIError{StatusCode: http.StatusForbidden}))
	assert.True(t, shouldRetryStartupUser(&APIError{StatusCode: http.StatusInternalServerError}))
	assert.True(t, shouldRetryStartupUser(assert.AnError))
}

func TestAttemptZeroConfigSkipsStartupWizardWhenServerAlreadyInitialized(t *testing.T) {
	configureZeroConfigRetriesForTest(t)

	var startupUserCalls int32
	completed := true

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/System/Info/Public":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"LocalAddress":           "http://127.0.0.1:8096",
				"ServerName":             "NAVI",
				"Version":                "10.11.5",
				"Id":                     "server-id",
				"StartupWizardCompleted": completed,
			}))
		case "/Users/AuthenticateByName":
			http.Error(w, "", http.StatusUnauthorized)
		case "/Startup/User":
			atomic.AddInt32(&startupUserCalls, 1)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	key, err := AttemptZeroConfig(server.URL, "admin", "wrong-password")

	require.Empty(t, key)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAlreadyConfigured)
	assert.Contains(t, err.Error(), "bootstrap credentials were rejected")
	assert.Zero(t, atomic.LoadInt32(&startupUserCalls))
}

func TestAttemptZeroConfigDoesNotRetryUnauthorizedStartupUser(t *testing.T) {
	configureZeroConfigRetriesForTest(t)

	var startupUserCalls int32
	completed := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/System/Info/Public":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"LocalAddress":           "http://127.0.0.1:8096",
				"ServerName":             "NAVI",
				"Version":                "10.11.5",
				"Id":                     "server-id",
				"StartupWizardCompleted": completed,
			}))
		case "/Users/AuthenticateByName":
			http.Error(w, "", http.StatusUnauthorized)
		case "/Startup/User":
			atomic.AddInt32(&startupUserCalls, 1)
			http.Error(w, "", http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	key, err := AttemptZeroConfig(server.URL, "admin", "wrong-password")

	require.Empty(t, key)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAlreadyConfigured)
	assert.Contains(t, err.Error(), "startup wizard is unavailable")
	assert.Equal(t, int32(1), atomic.LoadInt32(&startupUserCalls))
}
