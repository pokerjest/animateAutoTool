package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"time"

	webassets "github.com/pokerjest/animateAutoTool/web"
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

func renderTemplateToString(name string, data interface{}) (string, error) {
	tmpl, err := webassets.ParseTemplates(templateFuncMap())
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}
