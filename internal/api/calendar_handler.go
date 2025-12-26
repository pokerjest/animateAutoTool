package api

import (
	"log"
	"net/http"
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
	// Map: BangumiID -> SubscriptionID (if subscribed)
	subMap := make(map[int]uint)
	var subs []model.Subscription
	// Preload Metadata to get BangumiID
	if err := db.DB.Preload("Metadata").Where("is_active = ?", true).Find(&subs).Error; err == nil {
		for _, sub := range subs {
			if sub.Metadata != nil && sub.Metadata.BangumiID != 0 {
				subMap[sub.Metadata.BangumiID] = sub.ID
			}
		}
	}

	c.HTML(http.StatusOK, "calendar.html", gin.H{
		"Calendar":   calendar,
		"Today":      today,
		"SkipLayout": isHTMX,
		"SubMap":     subMap,
	})
}
