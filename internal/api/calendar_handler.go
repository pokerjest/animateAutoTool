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

const calendarFetchTimeout = 4 * time.Second

// GetCalendarHandler renders the anime calendar.
func GetCalendarHandler(c *gin.Context) {
	client := bangumi.NewClient("", "", "")
	applyProxyToBangumiClient(client)
	client.SetTimeout(calendarFetchTimeout)

	log.Printf("DEBUG: Calendar Handler: Fetching data...")
	calendar, err := client.GetCalendar()
	if err != nil {
		log.Printf("Calendar: Failed to fetch calendar: %v", err)
		c.HTML(http.StatusOK, "calendar.html", gin.H{
			"Error": "无法获取番剧日历：" + humanizeOperationError(err.Error()),
		})
		return
	}
	today := calendarTodayTab(calendar, time.Now())

	isHTMX := c.GetHeader("HX-Request") == ValueTrue
	log.Printf("DEBUG: Calendar: Today=%d, isHTMX=%v", today, isHTMX)

	if len(calendar) == 0 {
		log.Printf("WARNING: Calendar data is empty!")
	} else {
		for i, d := range calendar {
			log.Printf("DEBUG: Day %d: ID=%d, Name=%s, Items=%d", i, d.Weekday.ID, d.Weekday.CN, len(d.Items))
		}
	}

	type SubInfo struct {
		ID            uint
		Total         int
		Downloaded    int64
		BangumiStatus int
	}
	subMap := make(map[int]SubInfo)
	var mu sync.Mutex

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

	if token := configValue(model.ConfigKeyBangumiAccessToken); token != "" {
		var wg sync.WaitGroup
		types := []int{1, 2, 3, 4, 5}

		for _, t := range types {
			wg.Add(1)
			go func(status int) {
				defer wg.Done()
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

func calendarTodayTab(calendar []bangumi.CalendarItem, now time.Time) int {
	target := int(now.Weekday())
	candidates := []int{target}
	if target == 0 {
		candidates = []int{7, 0}
	}

	available := make(map[int]struct{}, len(calendar))
	for _, day := range calendar {
		available[day.Weekday.ID] = struct{}{}
	}

	for _, candidate := range candidates {
		if _, ok := available[candidate]; ok {
			return candidate
		}
	}

	if len(calendar) > 0 {
		return calendar[0].Weekday.ID
	}

	if len(candidates) > 0 {
		return candidates[0]
	}

	return target
}
