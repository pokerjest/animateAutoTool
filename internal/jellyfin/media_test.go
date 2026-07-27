package jellyfin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMediaCatalogMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/Library/MediaFolders":
			_, _ = w.Write([]byte(`{"Items":[{"Id":"library-1","Name":"Anime","CollectionType":"tvshows","ChildCount":2}],"TotalRecordCount":1}`))
		case "/Users/user-1/Items":
			assert.Equal(t, "library-1", req.URL.Query().Get("ParentId"))
			_, _ = w.Write([]byte(`{"Items":[{"Id":"series-1","Name":"Example","Type":"Series","ProductionYear":2026,"RunTimeTicks":1000,"UserData":{"IsFavorite":true}}],"TotalRecordCount":1}`))
		case "/Users/user-1/Items/series-1":
			_, _ = w.Write([]byte(`{"Id":"series-1","Name":"Example","Type":"Series","Overview":"Summary","RunTimeTicks":1000,"UserData":{"Played":false,"IsFavorite":true}}`))
		case "/Users/user-1/Items/Resume":
			assert.Equal(t, "true", req.URL.Query().Get("Recursive"))
			_, _ = w.Write([]byte(`{"Items":[{"Id":"episode-1","Name":"Episode 1","Type":"Episode","RunTimeTicks":1000,"UserData":{"PlaybackPositionTicks":500}}],"TotalRecordCount":1}`))
		case "/Items/series-1/Images/Primary":
			_, _ = w.Write([]byte{0xff, 0xd8, 0xff, 0xd9})
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "test-key")
	client.UserID = "user-1"
	libraries, err := client.GetMediaFoldersContext(context.Background())
	require.NoError(t, err)
	require.Len(t, libraries, 1)
	assert.Equal(t, "Anime", libraries[0].Name)

	page, err := client.ListItemsContext(context.Background(), MediaQuery{ParentID: "library-1", Recursive: true})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "series-1", page.Items[0].ID)
	assert.True(t, page.Items[0].UserData.IsFavorite)

	item, err := client.GetMediaItemContext(context.Background(), "series-1")
	require.NoError(t, err)
	assert.Equal(t, "Summary", item.Overview)

	resume, err := client.ListResumeContext(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, resume.Items, 1)
	assert.EqualValues(t, 500, resume.Items[0].UserData.PlaybackPositionTicks)

	image, err := client.GetImageContext(context.Background(), "series-1")
	require.NoError(t, err)
	assert.NotEmpty(t, image)
}
