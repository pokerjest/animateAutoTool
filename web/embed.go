package webassets

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static dist
var Assets embed.FS

func StaticFS() (http.FileSystem, error) {
	sub, err := fs.Sub(Assets, "static")
	if err != nil {
		return nil, err
	}
	return http.FS(sub), nil
}

func DistAssetsFS() (http.FileSystem, error) {
	sub, err := fs.Sub(Assets, "dist/assets")
	if err != nil {
		return nil, err
	}
	return http.FS(sub), nil
}

func SPAIndex() ([]byte, error) {
	return Assets.ReadFile("dist/index.html")
}
