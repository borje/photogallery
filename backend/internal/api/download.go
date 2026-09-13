package api

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/bege/smugbox/backend/internal/db"
	"github.com/bege/smugbox/backend/internal/storage"
)

// maxConcurrentZips bounds simultaneous album downloads to spare disk I/O.
const maxConcurrentZips = 2

// GET /api/albums/{slug}/download — streams a zip of all originals.
func (s *Server) downloadAlbum(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !validSlug(slug) {
		writeError(w, http.StatusNotFound, "album_not_found", "")
		return
	}
	album, err := s.db.GetAlbumBySlug(r.Context(), slug)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "album_not_found", "")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !s.albumAccess(r, album) {
		passwordRequired(w, nil, nil)
		return
	}
	select {
	case s.zipSem <- struct{}{}:
		defer func() { <-s.zipSem }()
	default:
		w.Header().Set("Retry-After", "30")
		writeError(w, http.StatusServiceUnavailable, "too_many_downloads", "try again in a moment")
		return
	}
	photos, err := s.db.ListReadyPhotos(r.Context(), album.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	name := sanitizeFilename(album.Name)
	if name == "" {
		name = "album"
	}
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", contentDisposition(name+".zip"))
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	zw := zip.NewWriter(w)
	rc := http.NewResponseController(w)
	seen := map[string]int{}
	for _, p := range photos {
		f, err := s.store.Open(album.ID, p.ID, storage.Original)
		if err != nil {
			s.log.Error("zip: original missing", "album", album.ID, "photo", p.ID, "err", err)
			continue
		}
		hdr := &zip.FileHeader{Name: uniqueZipName(seen, p.Filename), Method: zip.Store, Modified: p.UpdatedAt}
		hdr.SetMode(0o644)
		entry, err := zw.CreateHeader(hdr)
		if err == nil {
			_, err = io.Copy(entry, f)
		}
		f.Close()
		if err != nil {
			// Almost always the client went away; nothing more to send.
			s.log.Info("zip stream aborted", "album", album.ID, "err", err)
			return
		}
		_ = rc.Flush()
	}
	if err := zw.Close(); err != nil {
		s.log.Info("zip finalize failed", "album", album.ID, "err", err)
	}
}

// uniqueZipName disambiguates repeated file names (case-insensitively) as
// name-2.jpg, name-3.jpg, ...
func uniqueZipName(seen map[string]int, name string) string {
	if name == "" {
		name = "photo.jpg"
	}
	key := strings.ToLower(name)
	n := seen[key]
	seen[key] = n + 1
	if n == 0 {
		return name
	}
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := n + 1; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", base, i, ext)
		ck := strings.ToLower(candidate)
		if seen[ck] == 0 {
			seen[ck] = 1
			return candidate
		}
	}
}
