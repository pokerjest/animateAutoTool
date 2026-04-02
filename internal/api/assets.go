package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"

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
