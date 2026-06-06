package web

import (
	"embed"
	"io/fs"
)

//go:embed dist/index.html dist/assets/** dist/icons/** dist/monaco/** dist/*.svg
var embeddedDist embed.FS

func DistFS() (fs.FS, error) {
	return fs.Sub(embeddedDist, "dist")
}
