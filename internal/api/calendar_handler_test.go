package api

import (
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/bangumi"
)

func TestCalendarTodayTabPrefersSundayIDWhenAvailable(t *testing.T) {
	calendar := []bangumi.CalendarItem{
		calendarDay(1),
		calendarDay(7),
	}

	got := calendarTodayTab(calendar, time.Date(2026, 4, 5, 9, 0, 0, 0, time.Local))
	if got != 7 {
		t.Fatalf("expected sunday tab 7, got %d", got)
	}
}

func TestCalendarTodayTabFallsBackToZeroSundayID(t *testing.T) {
	calendar := []bangumi.CalendarItem{
		calendarDay(1),
		calendarDay(0),
	}

	got := calendarTodayTab(calendar, time.Date(2026, 4, 5, 9, 0, 0, 0, time.Local))
	if got != 0 {
		t.Fatalf("expected sunday tab 0, got %d", got)
	}
}

func TestCalendarTodayTabFallsBackToFirstAvailableDay(t *testing.T) {
	calendar := []bangumi.CalendarItem{
		calendarDay(3),
		calendarDay(5),
	}

	got := calendarTodayTab(calendar, time.Date(2026, 4, 7, 9, 0, 0, 0, time.Local))
	if got != 3 {
		t.Fatalf("expected fallback day 3, got %d", got)
	}
}

func calendarDay(id int) bangumi.CalendarItem {
	var item bangumi.CalendarItem
	item.Weekday.ID = id
	return item
}
