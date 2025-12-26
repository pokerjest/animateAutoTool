package api

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

// GetCalendarHandler renders the anime calendar
func GetCalendarHandler(c *gin.Context) {
	// Use Bangumi Client (Public API, no auth needed)
	client := bangumi.NewClient("", "", "")

	// Check for HTMX request
	log.Printf("DEBUG: Calendar Handler: Fetching data...")
	calendar, err := client.GetCalendar()
	if err != nil {
		log.Printf("Calendar: Failed to fetch calendar: %v", err)
		c.HTML(http.StatusOK, "calendar.html", gin.H{
			"Error": "无法获取番剧日历: " + err.Error(),
		})
		return
	}
	log.Printf("DEBUG: Got %d days of calendar data", len(calendar))
	for i, d := range calendar {
		log.Printf("DEBUG: Day %d: ID=%d, Name=%s, Items=%d", i, d.Weekday.ID, d.Weekday.CN, len(d.Items))
	}

	// Determine today's weekday for highlighting (1=Mon, 7=Sun)
	// Bangumi returns 1=Mon...7=Sun.
	// Go's time.Weekday() returns 0=Sun, 1=Mon...
	// We need to map Go's 0 to 7.
	today := int(time.Now().Weekday())
	if today == 0 {
		today = 7
	}

	// Check for HTMX request
	isHTMX := c.GetHeader("HX-Request") == "true"

	// Fetch active subscriptions to check status
	type SubInfo struct {
		ID            uint
		Total         int
		Downloaded    int64
		BangumiStatus int // 1:Wish, 2:Collect, 3:Doing, 4:OnHold, 5:Dropped
	}
	subMap := make(map[int]SubInfo)
	var mu sync.Mutex

	// 1. Load Local Subscriptions
	var subs []model.Subscription
	if err := db.DB.Preload("Metadata").Where("is_active = ?", true).Find(&subs).Error; err == nil {
		for _, sub := range subs {
			if sub.Metadata != nil && sub.Metadata.BangumiID != 0 {
				var count int64
				db.DB.Model(&model.DownloadLog{}).Where("subscription_id = ? AND status = ?", sub.ID, "completed").Count(&count)
				subMap[sub.Metadata.BangumiID] = SubInfo{
					ID:         sub.ID,
					Total:      sub.LastEp,
					Downloaded: count,
				}
			}
		}
	}

	// 2. Fetch Bangumi Collection Status (If authenticated)
	var tokenCfg model.GlobalConfig
	if err := db.DB.Where("key = ?", model.ConfigKeyBangumiAccessToken).First(&tokenCfg).Error; err == nil && tokenCfg.Value != "" {
		token := tokenCfg.Value
		var wg sync.WaitGroup

		// Fetch types 1 (Wish), 2 (Collect), 3 (Doing), 4 (OnHold), 5 (Dropped)
		// We limit to 50 items per type to avoid slow load, assuming calendar items are recent
		types := []int{1, 2, 3, 4, 5}

		for _, t := range types {
			wg.Add(1)
			go func(status int) {
				defer wg.Done()
				// Fetch "me" collection
				// Use a reasonable limit, e.g., 100
				items, err := client.GetUserCollection(token, "me", status, 100, 0)
				if err == nil {
					mu.Lock()
					defer mu.Unlock()
					for _, item := range items {
						bid := item.SubjectID
						info, exists := subMap[bid]
						if !exists {
							info = SubInfo{}
						}
						info.BangumiStatus = status
						subMap[bid] = info
					}
				} else {
					log.Printf("Failed to fetch collection type %d: %v", status, err)
				}
			}(t)
		}
		wg.Wait()
	}

	c.HTML(http.StatusOK, "calendar.html", gin.H{
		"Calendar":   calendar,
		"Today":      today,
		"SkipLayout": isHTMX,
		"SubMap":     subMap,
	})
}
