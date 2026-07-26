package jellyfin

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestItemDetailsAndFavoriteState(t *testing.T) {
	var mu sync.Mutex
	requests := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"episode-1","RunTimeTicks":14400000000,"UserData":{"PlaybackPositionTicks":7200000000,"Played":false,"IsFavorite":true},"MediaSources":[{"Container":"mkv","Size":1073741824,"Bitrate":8000000,"MediaStreams":[{"Type":"Video","Codec":"hevc","Width":1920,"Height":1080},{"Type":"Audio","Codec":"aac","Channels":2},{"Type":"Subtitle","Codec":"ass"}]}]}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "test-key")
	client.UserID = "user-1"
	details, err := client.GetItemDetails("episode-1")
	require.NoError(t, err)
	assert.Equal(t, int64(7200000000), details.UserData.PlaybackPositionTicks)
	assert.True(t, details.UserData.IsFavorite)
	require.Len(t, details.MediaSources, 1)
	assert.Equal(t, "hevc", details.MediaSources[0].MediaStreams[0].Codec)
	require.NoError(t, client.SetFavorite("episode-1", true))
	require.NoError(t, client.SetFavorite("episode-1", false))

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, requests, "GET /Users/user-1/Items/episode-1")
	assert.Contains(t, requests, "POST /Users/user-1/FavoriteItems/episode-1")
	assert.Contains(t, requests, "DELETE /Users/user-1/FavoriteItems/episode-1")
}
