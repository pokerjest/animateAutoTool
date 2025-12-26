package api

import (
	"log"
	"net/http"
	"time"

	"github.com/bangumi-pro/bangumi-api-go"
	"github.com/gin-gonic/gin"
)

// GetCalendarHandler renders the anime calendar
func GetCalendarHandler(c *gin.Context) {
	// Use Bangumi Client (Public API, no auth needed)
	client := bangumi.NewClient("", "", "")

	calendar, err := client.GetCalendar()
	if err != nil {
		log.Printf("Calendar: Failed to fetch calendar: %v", err)
		c.HTML(http.StatusOK, "calendar.html", gin.H{
			"Error": "无法获取番剧日历，请稍后重试。",
		})
		return
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

	c.HTML(http.StatusOK, "calendar.html", gin.H{
		"Calendar":   calendar,
		"Today":      today,
		"SkipLayout": isHTMX,
	})
}
