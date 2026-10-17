// Package webui serves the web interface embedded into the binary.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// dist/app is produced by "npm run build" in web/. dist/.keep keeps the
// embed pattern valid when the web interface has not been built.
//
//go:embed all:dist
var dist embed.FS

// Embedded returns the built web interface, or nil when it was not built.
func Embedded() fs.FS {
	sub, err := fs.Sub(dist, "dist/app")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}

const notBuilt = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>metagit</title></head>
<body><p>The web interface is not built. Run <code>make build</code>.</p></body></html>`

// Handler serves files from site and falls back to index.html for client-side
// routes. A nil site serves a page that says the interface was not built.
func Handler(site fs.FS) http.Handler {
	if site == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(notBuilt))
		})
	}
	files := http.FileServerFS(site)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if info, err := fs.Stat(site, name); err == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					// Vite puts a content hash into asset names.
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(name, "assets/") || path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
		}
		serveIndex(w, r, site)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, site fs.FS) {
	data, err := fs.ReadFile(site, "index.html")
	if err != nil {
		http.Error(w, "index.html not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}
