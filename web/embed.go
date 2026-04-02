package webassets

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed templates static
var Assets embed.FS

func ParseTemplates(funcMap template.FuncMap) (*template.Template, error) {
	return template.New("").Funcs(funcMap).ParseFS(Assets, "templates/*.html")
}

func StaticFS() (http.FileSystem, error) {
	sub, err := fs.Sub(Assets, "static")
	if err != nil {
		return nil, err
	}
	return http.FS(sub), nil
}
