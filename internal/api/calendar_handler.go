package api

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// GetCalendarHandler renders the anime calendar
func (s *Server) GetCalendarHandler(c *gin.Context) {
	// Attempt to get user access token if available, though Calendar is public API usually
	// But Bangumi client structure uses access token for personal data.
	// The GetCalendar method we added doesn't require token, it uses public API.

	// Use our Bangumi client instance
	// We need to access the bangumi client from Server struct or create a temporary one?
	// The Server struct has BangumiClient field of type *bangumi.Client?
	// Let's check api/server.go or where Server is defined.
	// Assuming s.BangumiClient exists.

	// Actually, looking at other handlers, how do we access bangumi client?
	// We might need to check internal/api/routes.go or server struct definition.
	// For now, I will assume s.BangumiClient is available.

	calendar, err := s.BangumiClient.GetCalendar()
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

	c.HTML(http.StatusOK, "calendar.html", gin.H{
		"Calendar": calendar,
		"Today":    today,
	})
}
