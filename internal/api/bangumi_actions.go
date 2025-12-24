package api

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

// UpdateBangumiCollectionHandler 更新 Bangumi 收藏状态 (订阅)
func UpdateBangumiCollectionHandler(c *gin.Context) {
	// Check Login
	var accessToken string
	if err := db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyBangumiAccessToken).Select("value").Scan(&accessToken).Error; err != nil || accessToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登录 Bangumi"})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	var req struct {
		Status  int      `json:"status"` // 1: Wish, 2: Collect, 3: Do, 4: On_Hold, 5: Dropped
		Rating  int      `json:"rating"`
		Comment string   `json:"comment"`
		Tags    []string `json:"tags"`
		Private bool     `json:"private"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// Default to 3 (Watching) if not provided or bad format
		req.Status = 3
	}

	// Init Client (credentials not needed for this call if token is valid, but struct requires them)
	// Fetch app ID/Secret from DB just in case, though token is what matters
	client := bangumi.NewClient("", "", "")

	opts := bangumi.CollectionUpdateOptions{
		Status:  req.Status,
		Rating:  req.Rating,
		Comment: req.Comment,
		Tags:    req.Tags,
	}
	if req.Private {
		opts.Private = 1
	}

	if err := client.UpdateCollection(accessToken, id, opts); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "已更新收藏状态"})
}

// UpdateBangumiProgressHandler 同步观看进度
func UpdateBangumiProgressHandler(c *gin.Context) {
	// Check Login
	var accessToken string
	if err := db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyBangumiAccessToken).Select("value").Scan(&accessToken).Error; err != nil || accessToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登录 Bangumi"})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	var req struct {
		EpisodeCount int `json:"episode_count"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid body"})
		return
	}

	client := bangumi.NewClient("", "", "")

	if err := client.UpdateWatchedEpisodes(accessToken, id, req.EpisodeCount); err != nil {
		log.Printf("ERROR: UpdateWatchedEpisodes failed for ID %d: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": fmt.Sprintf("已同步至第 %d 集", req.EpisodeCount)})
}
