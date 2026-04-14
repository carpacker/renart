package static

import (
	"bytes"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type assetSource struct {
	fs fs.FS
}

type Handler struct {
	sources []assetSource
}

func NewHandler(embedded fs.FS, overrideDir string) (*Handler, error) {
	sources := make([]assetSource, 0, 2)

	if strings.TrimSpace(overrideDir) != "" {
		override := os.DirFS(overrideDir)
		if _, err := fs.Stat(override, "."); err == nil {
			sources = append(sources, newAssetSource(override))
		}
	}

	if embedded != nil {
		sources = append(sources, newAssetSource(embedded))
	}

	if len(sources) == 0 {
		return nil, fmt.Errorf("no static asset source configured")
	}

	return &Handler{sources: sources}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	assetPath := normalizeAssetPath(r.URL.Path)
	if assetPath != "" && h.serveAsset(w, r, assetPath) {
		return
	}

	if h.serveIndex(w, r) {
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("Bruin Web UI assets are unavailable."))
}

func newAssetSource(fsys fs.FS) assetSource {
	return assetSource{fs: fsys}
}

func (h *Handler) serveAsset(w http.ResponseWriter, r *http.Request, assetPath string) bool {
	for _, source := range h.sources {
		entry, err := fs.Stat(source.fs, assetPath)
		if err != nil || entry.IsDir() {
			continue
		}

		return serveFile(w, r, source.fs, assetPath)
	}

	return false
}

func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request) bool {
	for _, source := range h.sources {
		if _, err := fs.Stat(source.fs, "index.html"); err != nil {
			continue
		}

		return serveFile(w, r, source.fs, "index.html")
	}

	return false
}

func normalizeAssetPath(requestPath string) string {
	clean := path.Clean("/" + requestPath)
	clean = strings.TrimPrefix(clean, "/")
	if clean == "." {
		return ""
	}
	return clean
}

func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) bool {
	content, err := fs.ReadFile(fsys, name)
	if err != nil {
		return false
	}

	if contentType := mime.TypeByExtension(filepath.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}

	http.ServeContent(w, r, path.Base(name), time.Time{}, bytes.NewReader(content))
	return true
}
