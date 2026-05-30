package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist
var dist embed.FS

func Files() http.FileSystem {
	files, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return http.FS(files)
}
