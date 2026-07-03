package sqllsp

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

func FileURI(path string) URI {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	slash := filepath.ToSlash(abs)
	if runtime.GOOS == "windows" && !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	return URI((&url.URL{Scheme: "file", Path: slash}).String())
}

func URIToPath(uri URI) (string, bool) {
	parsed, err := url.Parse(string(uri))
	if err != nil || parsed.Scheme != "file" {
		return "", false
	}
	path := parsed.Path
	if runtime.GOOS == "windows" {
		path = strings.TrimPrefix(path, "/")
	}
	return filepath.FromSlash(path), true
}
