// Package api implements the HTTP API under /api/ and mounts the frontend
// handler for everything else.
package api

import (
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/bege/photogallery/backend/internal/config"
	"github.com/bege/photogallery/backend/internal/db"
	"github.com/bege/photogallery/backend/internal/storage"
)

// Deps are the collaborators a Server needs. Now and Rand are injectable
// for tests; nil means the real clock and crypto/rand.
type Deps struct {
	DB    *db.DB
	Store *storage.Store
	Cfg   config.Config
	Log   *slog.Logger
	Now   func() time.Time
	Rand  io.Reader
	Web   http.Handler // serves the frontend; nil means 404 for non-API paths
}

// Server is the root http.Handler.
type Server struct {
	db      *db.DB
	store   *storage.Store
	cfg     config.Config
	log     *slog.Logger
	now     func() time.Time
	rand    io.Reader
	handler http.Handler

	deriveSem chan struct{} // bounds concurrent libvips derivative generation
	zipSem    chan struct{} // bounds concurrent zip downloads
}

// jsonTimeout bounds handlers that produce small JSON responses. Uploads
// and byte streams are deliberately not wrapped.
const jsonTimeout = 30 * time.Second

// New builds the server and its routes.
func New(d Deps) *Server {
	s := &Server{
		db: d.DB, store: d.Store, cfg: d.Cfg, log: d.Log, now: d.Now, rand: d.Rand,
		deriveSem: make(chan struct{}, runtime.NumCPU()),
		zipSem:    make(chan struct{}, maxConcurrentZips),
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.rand == nil {
		s.rand = rand.Reader
	}
	web := d.Web
	if web == nil {
		web = http.NotFoundHandler()
	}

	mux := http.NewServeMux()
	s.routes(mux)
	mux.HandleFunc("GET /api/healthz", s.healthz)
	mux.HandleFunc("/api/", s.notFound)
	mux.Handle("/", web)

	s.handler = securityHeaders(s.requestLog(s.recoverer(mux)))
	return s
}

func (s *Server) routes(mux *http.ServeMux) {
	// Lightroom plugin endpoints, API-key protected.
	pub := func(h http.HandlerFunc) http.Handler { return s.requireAPIKey(h) }
	pubJSON := func(h http.HandlerFunc) http.Handler {
		return s.requireAPIKey(http.TimeoutHandler(h, jsonTimeout, timeoutBody))
	}
	mux.Handle("POST /api/publish/albums", pubJSON(s.createAlbum))
	mux.Handle("PUT /api/publish/albums/{id}", pubJSON(s.updateAlbum))
	mux.Handle("DELETE /api/publish/albums/{id}", pubJSON(s.deleteAlbum))
	mux.Handle("GET /api/publish/albums/{id}/photos", pubJSON(s.listPublishedPhotos))
	mux.Handle("POST /api/publish/albums/{id}/photos", pub(s.uploadPhoto))
	mux.Handle("PUT /api/publish/albums/{id}/photos/{photo_id}", pub(s.replacePhoto))
	mux.Handle("DELETE /api/publish/albums/{id}/photos/{photo_id}", pubJSON(s.deletePhoto))
	mux.Handle("PUT /api/publish/albums/{id}/order", pubJSON(s.setPhotoOrder))

	// Visitor endpoints. Image bytes and zips are not wrapped in a timeout.
	visitorJSON := func(h http.HandlerFunc) http.Handler { return http.TimeoutHandler(h, jsonTimeout, timeoutBody) }
	mux.Handle("GET /api/albums", visitorJSON(s.listAlbums))
	mux.Handle("GET /api/albums/{slug}", visitorJSON(s.getAlbum))
	mux.HandleFunc("GET /api/albums/{slug}/cover", s.albumCover)
	mux.HandleFunc("GET /api/albums/{slug}/photos/{photo_id}/{variant}", s.getPhotoVariant)
	mux.HandleFunc("GET /api/albums/{slug}/download", s.downloadAlbum)
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// albumURL is the public page for an album.
func (s *Server) albumURL(slug string) string {
	return s.cfg.PublicBaseURL + "/a/" + slug
}

// photoURL is the public page for an album opened at one photo.
func (s *Server) photoURL(slug, photoID string) string {
	return s.albumURL(slug) + "?photo=" + photoID
}
