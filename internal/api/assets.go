package api

import (
	"encoding/json"
	"fmt"
	"html/template"
	"time"

	appversion "github.com/pokerjest/animateAutoTool/internal/version"
)

func templateFuncMap() template.FuncMap {
	return template.FuncMap{
		"div": func(a, b float64) float64 {
			return a / b
		},
		"toGB": func(size int64) string {
			gb := float64(size) / 1024 / 1024 / 1024
			return fmt.Sprintf("%.2f GB", gb)
		},
		"humanizeBytes": func(size int64) string {
			switch {
			case size <= 0:
				return "0 B"
			case size < 1024:
				return fmt.Sprintf("%d B", size)
			case size < 1024*1024:
				return fmt.Sprintf("%.1f KB", float64(size)/1024)
			case size < 1024*1024*1024:
				return fmt.Sprintf("%.1f MB", float64(size)/1024/1024)
			default:
				return fmt.Sprintf("%.2f GB", float64(size)/1024/1024/1024)
			}
		},
		"json": func(v interface{}) template.JS {
			a, _ := json.Marshal(v)
			return template.JS(a) //nolint:gosec // json.Marshal escapes HTML
		},
		"toJson": func(v interface{}) string {
			a, _ := json.Marshal(v)
			return string(a)
		},
		"formatTime": func(v interface{}) string {
			t, ok := asTemplateTime(v)
			if !ok {
				return "从未"
			}
			return t.Local().Format("2006-01-02 15:04")
		},
		"timeAgo": func(v interface{}) string {
			t, ok := asTemplateTime(v)
			if !ok {
				return "从未"
			}
			return humanizeTimeAgo(time.Since(t))
		},
		"humanizeOperationError": humanizeOperationError,
		"appVersion": func() string {
			return appversion.AppVersion
		},
	}
}

func asTemplateTime(v interface{}) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		if t.IsZero() {
			return time.Time{}, false
		}
		return t, true
	case *time.Time:
		if t == nil || t.IsZero() {
			return time.Time{}, false
		}
		return *t, true
	default:
		return time.Time{}, false
	}
}

func humanizeTimeAgo(d time.Duration) string {
	if d < 0 {
		d = -d
	}

	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d 天前", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%d 天前", int(d.Hours()/24))
	}
}
