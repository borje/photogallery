// Package web serves the built frontend from a directory with SPA fallback.
package web

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// Handler serves files from dir. Paths without a file extension that do not
// match a file fall back to index.html so client-side routes work. With an
// empty dir every request is answered 404.
func Handler(dir string) http.Handler {
	if dir == "" {
		return http.NotFoundHandler()
	}
	fsys := os.DirFS(dir)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if rel == "" {
			rel = "index.html"
		}
		if !fs.ValidPath(rel) {
			http.NotFound(w, r)
			return
		}
		if info, err := fs.Stat(fsys, rel); err == nil && !info.IsDir() {
			if strings.HasPrefix(rel, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			http.ServeFileFS(w, r, fsys, rel)
			return
		}
		if path.Ext(rel) != "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, fsys, "index.html")
	})
}
