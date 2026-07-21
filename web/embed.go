//go:build !webdev

package web

import (
	"embed"
	"io/fs"
)

//go:embed dist/index.html dist/assets/** dist/icons/** dist/monaco/**
var embeddedDist embed.FS

func DistFS() (fs.FS, error) {
	return fs.Sub(embeddedDist, "dist")
}
