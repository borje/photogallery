package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o644)
	os.WriteFile(filepath.Join(dir, "assets", "app-abc123.js"), []byte("console.log(1)"), 0o644)
	os.WriteFile(filepath.Join(dir, "favicon.svg"), []byte("<svg/>"), 0o644)
	return dir
}

func get(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestServesFilesAndSPAFallback(t *testing.T) {
	h := Handler(setup(t))
	for _, p := range []string{"/", "/a/some-album", "/a/x/y", "/nested/route"} {
		rec := get(h, http.MethodGet, p)
		if rec.Code != 200 || rec.Body.String() != "<html>app</html>" {
			t.Errorf("%s: code %d body %q", p, rec.Code, rec.Body.String())
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("%s: Cache-Control %q", p, cc)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s: Content-Type %q", p, ct)
		}
	}
	rec := get(h, http.MethodGet, "/assets/app-abc123.js")
	if rec.Code != 200 || rec.Body.String() != "console.log(1)" {
		t.Fatalf("asset: %d %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("asset content type %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("asset cache control %q", cc)
	}
	if rec := get(h, http.MethodGet, "/favicon.svg"); rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("favicon: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	if rec := get(h, http.MethodGet, "/missing.png"); rec.Code != 404 {
		t.Fatalf("missing file with extension should 404, got %d", rec.Code)
	}
	if rec := get(h, http.MethodGet, "/assets/"); rec.Code != 200 || rec.Body.String() != "<html>app</html>" {
		t.Fatalf("directory path should fall back to index, got %d", rec.Code)
	}
	if rec := get(h, http.MethodPost, "/"); rec.Code != 405 {
		t.Fatalf("POST should be 405, got %d", rec.Code)
	}
}

func TestTraversalIsNeutralised(t *testing.T) {
	dir := setup(t)
	os.WriteFile(filepath.Join(filepath.Dir(dir), "secret.txt"), []byte("secret"), 0o644)
	h := Handler(dir)
	for _, p := range []string{"/../secret.txt", "/assets/../../secret.txt", "/%2e%2e/secret.txt"} {
		req := httptest.NewRequest(http.MethodGet, "http://x"+p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), "secret") {
			t.Fatalf("%s leaked file outside dir", p)
		}
	}
}

func TestNoFrontendDir(t *testing.T) {
	h := Handler("")
	for _, p := range []string{"/", "/a/x", "/index.html"} {
		if rec := get(h, http.MethodGet, p); rec.Code != 404 {
			t.Fatalf("%s: %d, want 404", p, rec.Code)
		}
	}
	// A configured dir without index.html also yields 404 for routes.
	h = Handler(t.TempDir())
	if rec := get(h, http.MethodGet, "/a/x"); rec.Code != 404 {
		t.Fatalf("missing index.html: %d", rec.Code)
	}
}
