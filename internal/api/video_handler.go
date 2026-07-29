package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/parser"
)

// GetMikanEpisodesHandler fetches episodes from Mikan RSS for a specific anime/subgroup.
func GetMikanEpisodesHandler(c *gin.Context) {
	bangumiID := c.Query("bangumiId")
	subgroupID := c.Query("subgroupId")

	if bangumiID == "" {
		jsonBadRequest(c, "缺少 bangumiId")
		return
	}

	activeMikanID := bangumiID
	rssURL := fmt.Sprintf("https://mikanani.me/RSS/Bangumi?bangumiId=%s", bangumiID)
	if subgroupID != "" {
		rssURL += fmt.Sprintf("&subgroupid=%s", subgroupID)
	}

	p := newConfiguredMikanParser()
	episodes, err := p.ParseContext(c.Request.Context(), rssURL)
	if err != nil {
		jsonServerError(c, "获取 RSS 剧集列表", err)
		return
	}

	if len(episodes) > 20 {
		episodes = episodes[:20]
	}

	if len(episodes) == 0 {
		title := c.Query("title")
		if title != "" {
			fmt.Printf("GetMikanEpisodesHandler: Bangumi ID %s yielded no results. Searching for title: %s\n", bangumiID, title)
			searchResults, err := p.Search(title)
			if err == nil && len(searchResults) > 0 {
				newID := searchResults[0].MikanID
				activeMikanID = newID
				fmt.Printf("GetMikanEpisodesHandler: Found Mikan ID %s for title %s. Retrying...\n", newID, title)

				newRSSURL := fmt.Sprintf("https://mikanani.me/RSS/Bangumi?bangumiId=%s", newID)
				if subgroupID != "" {
					newRSSURL += fmt.Sprintf("&subgroupid=%s", subgroupID)
				}
				episodes, _ = p.ParseContext(c.Request.Context(), newRSSURL)
			}
		}
	}

	if episodes == nil {
		episodes = []parser.Episode{}
	}

	c.JSON(http.StatusOK, gin.H{
		"episodes": episodes,
		"mikanId":  activeMikanID,
	})
}
