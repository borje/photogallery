package api

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/bege/photogallery/backend/internal/db"
	"github.com/bege/photogallery/backend/internal/storage"
)

// GET /api/albums/{slug}/photos/{photo_id}/{variant}[?download=1]
func (s *Server) getPhotoVariant(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !validSlug(slug) {
		writeError(w, http.StatusNotFound, "album_not_found", "")
		return
	}
	variant, ok := storage.ParseVariant(r.PathValue("variant"))
	if !ok {
		writeError(w, http.StatusNotFound, "variant_not_found", "")
		return
	}
	photoID := r.PathValue("photo_id")
	if _, err := uuid.Parse(photoID); err != nil {
		writeError(w, http.StatusNotFound, "photo_not_found", "")
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
	photo, err := s.db.GetPhoto(r.Context(), album.ID, photoID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "photo_not_found", "")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	cache := "public, max-age=86400"
	if album.Protected() {
		cache = "private, max-age=3600"
	}
	s.serveVariant(w, r, album, photo, variant, r.URL.Query().Get("download") == "1", cache)
}

// serveVariant streams one stored file with ETag and caching headers. The
// large variant falls back to the original when it was not generated.
func (s *Server) serveVariant(w http.ResponseWriter, r *http.Request, album *db.Album, photo *db.Photo, v storage.Variant, download bool, cacheControl string) {
	if v == storage.Large {
		if _, err := s.store.Stat(album.ID, photo.ID, v); errors.Is(err, fs.ErrNotExist) {
			v = storage.Original
		}
	}
	f, err := s.store.Open(album.ID, photo.ID, v)
	if errors.Is(err, fs.ErrNotExist) {
		s.log.Error("photo file missing on disk", "album", album.ID, "photo", photo.ID, "variant", v)
		writeError(w, http.StatusNotFound, "file_missing", "")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("ETag", fmt.Sprintf(`"%s-%s"`, photo.ContentHash, v))
	h.Set("Cache-Control", cacheControl)
	if download {
		h.Set("Content-Disposition", contentDisposition(photo.Filename))
	}
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// contentDisposition builds an attachment header with an ASCII fallback and
// an RFC 5987 encoded UTF-8 filename.
func contentDisposition(filename string) string {
	if filename == "" {
		filename = "download"
	}
	var ascii strings.Builder
	for _, r := range filename {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			ascii.WriteByte('_')
		} else {
			ascii.WriteRune(r)
		}
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii.String(), rfc5987(filename))
}

func rfc5987(s string) string {
	const attrChars = "!#$&+-.^_`|~"
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', strings.IndexByte(attrChars, c) >= 0:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
