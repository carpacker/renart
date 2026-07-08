//go:build webdev

package web

import (
	"io/fs"
	"testing/fstest"
)

// Dev builds (`go build -tags webdev`) skip embedding web/dist so the backend
// can be rebuilt on every save without a frontend build present — the real UI
// is served by the Vite dev server (see scripts/dev.sh). This placeholder is
// only ever seen if you hit the backend port directly instead of Vite's.
func DistFS() (fs.FS, error) {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(devPlaceholderHTML)},
	}, nil
}

const devPlaceholderHTML = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <title>Renart — dev backend</title>
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <style>
      body { font: 15px/1.6 system-ui, sans-serif; margin: 4rem auto; max-width: 34rem; padding: 0 1.5rem; color: #1f2430; }
      code { background: #eef0f4; padding: 0.1rem 0.35rem; border-radius: 4px; }
      a { color: #2f6feb; }
    </style>
  </head>
  <body>
    <h1>Renart dev backend</h1>
    <p>
      This is the API backend built with <code>-tags webdev</code>, so the UI is
      not embedded. Open the Vite dev server instead — it serves the app with
      hot-reload and proxies the API here:
    </p>
    <p><a href="http://127.0.0.1:5173">http://127.0.0.1:5173</a></p>
  </body>
</html>
`
