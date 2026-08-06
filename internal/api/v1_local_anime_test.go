package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestV1LocalAnimeHandlerPaginatesAndSearchesWholeLibrary(t *testing.T) {
	originalDB := db.DB
	tx := originalDB.Begin()
	require.NoError(t, tx.Error)
	db.DB = tx
	t.Cleanup(func() {
		_ = tx.Rollback().Error
		db.DB = originalDB
	})
	require.NoError(t, db.DB.Unscoped().Where("1 = 1").Delete(&model.LocalEpisode{}).Error)
	require.NoError(t, db.DB.Unscoped().Where("1 = 1").Delete(&model.LocalAnime{}).Error)
	require.NoError(t, db.DB.Unscoped().Where("1 = 1").Delete(&model.LocalAnimeDirectory{}).Error)

	directory := model.LocalAnimeDirectory{Path: "/library"}
	require.NoError(t, db.DB.Create(&directory).Error)
	metadata := model.AnimeMetadata{Title: "Provider title", TitleCN: "元数据搜索命中", BangumiID: 730001}
	require.NoError(t, db.DB.Create(&metadata).Error)

	items := make([]model.LocalAnime, 105)
	for i := range items {
		items[i] = model.LocalAnime{
			DirectoryID: directory.ID,
			Title:       fmt.Sprintf("Library Show %03d", i+1),
			Path:        fmt.Sprintf("/library/show-%03d", i+1),
			Season:      1,
			FileCount:   1,
		}
	}
	items[0].MetadataID = &metadata.ID
	items[1].Title = "100% Hero"
	require.NoError(t, db.DB.CreateInBatches(&items, 50).Error)

	request := func(rawQuery string) struct {
		Data struct {
			Items []model.LocalAnime `json:"items"`
		} `json:"data"`
		Meta struct {
			Page     int   `json:"page"`
			PageSize int   `json:"page_size"`
			Total    int64 `json:"total"`
		} `json:"meta"`
	} {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/local-anime?"+rawQuery, nil)
		V1LocalAnimeHandler(c)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var payload struct {
			Data struct {
				Items []model.LocalAnime `json:"items"`
			} `json:"data"`
			Meta struct {
				Page     int   `json:"page"`
				PageSize int   `json:"page_size"`
				Total    int64 `json:"total"`
			} `json:"meta"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
		return payload
	}

	secondPage := request("page=2&page_size=100")
	assert.Equal(t, 2, secondPage.Meta.Page)
	assert.Equal(t, 100, secondPage.Meta.PageSize)
	assert.EqualValues(t, 105, secondPage.Meta.Total)
	assert.Len(t, secondPage.Data.Items, 5)

	metadataMatch := request("page=1&page_size=20&q=" + url.QueryEscape("元数据搜索命中"))
	assert.EqualValues(t, 1, metadataMatch.Meta.Total)
	require.Len(t, metadataMatch.Data.Items, 1)
	assert.Equal(t, items[0].ID, metadataMatch.Data.Items[0].ID)
	assert.Equal(t, "元数据搜索命中", metadataMatch.Data.Items[0].Metadata.TitleCN)

	literalWildcard := request("page=1&page_size=20&q=" + url.QueryEscape("%"))
	assert.EqualValues(t, 1, literalWildcard.Meta.Total)
	require.Len(t, literalWildcard.Data.Items, 1)
	assert.Equal(t, "100% Hero", literalWildcard.Data.Items[0].Title)
}

func TestV1LocalAnimeHandlerReturnsErrorWhenDatabaseUnavailable(t *testing.T) {
	originalDB := db.DB
	db.DB = nil
	t.Cleanup(func() { db.DB = originalDB })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/local-anime", nil)
	V1LocalAnimeHandler(c)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "local_anime_unavailable")
}
