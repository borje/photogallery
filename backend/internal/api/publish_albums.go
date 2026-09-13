package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/bege/smugbox/backend/internal/auth"
	"github.com/bege/smugbox/backend/internal/db"
)

const (
	maxNameLen        = 200
	maxDescriptionLen = 5000
	maxPasswordLen    = 72 // bcrypt input limit
	slugRetries       = 10

	maxIdempotencyKeyLen = 128
)

type albumInput struct {
	Name         *string `json:"name"`
	Description  *string `json:"description"`
	Password     *string `json:"password"`
	IsListed     *bool   `json:"is_listed"`
	CoverPhotoID *string `json:"cover_photo_id"`
	ParentID     *string `json:"parent_id"` // "" or omitted = root
	// IdempotencyKey lets a client retry a create safely: a second POST with
	// the same key answers with the album the first one made. Ignored on
	// update.
	IdempotencyKey *string `json:"idempotency_key"`
}

type albumOutput struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	URL       string `json:"url"`
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
	IsListed  bool   `json:"is_listed"`
}

func (s *Server) albumOutput(a *db.Album) albumOutput {
	return albumOutput{ID: a.ID, Slug: a.Slug, URL: s.albumURL(a.Slug), Name: a.Name, Protected: a.Protected(), IsListed: a.IsListed}
}

// albumFromPath loads the album named by {id}, answering 404 on failure.
func (s *Server) albumFromPath(w http.ResponseWriter, r *http.Request) (*db.Album, bool) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusNotFound, "album_not_found", "")
		return nil, false
	}
	a, err := s.db.GetAlbum(r.Context(), id)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "album_not_found", "")
		return nil, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return nil, false
	}
	return a, true
}

// validIdempotencyKey returns the trimmed key ("" when absent) or writes a
// 400 and returns false.
func validIdempotencyKey(w http.ResponseWriter, key *string) (string, bool) {
	if key == nil {
		return "", true
	}
	k := strings.TrimSpace(*key)
	if len(k) > maxIdempotencyKeyLen {
		writeError(w, http.StatusBadRequest, "invalid_idempotency_key", "idempotency_key must be at most 128 characters")
		return "", false
	}
	for _, c := range k {
		if c < 0x21 || c > 0x7e {
			writeError(w, http.StatusBadRequest, "invalid_idempotency_key", "idempotency_key must be printable ASCII without spaces")
			return "", false
		}
	}
	return k, true
}

func validName(w http.ResponseWriter, name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxNameLen {
		writeError(w, http.StatusBadRequest, "invalid_name", "name must be 1-200 characters")
		return "", false
	}
	return name, true
}

func (s *Server) createAlbum(w http.ResponseWriter, r *http.Request) {
	var in albumInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Name == nil {
		writeError(w, http.StatusBadRequest, "invalid_name", "name is required")
		return
	}
	name, ok := validName(w, *in.Name)
	if !ok {
		return
	}
	key, ok := validIdempotencyKey(w, in.IdempotencyKey)
	if !ok {
		return
	}
	if key != "" {
		if existing, err := s.db.GetAlbumByIdempotencyKey(r.Context(), key); err == nil {
			writeJSON(w, http.StatusCreated, s.albumOutput(existing))
			return
		} else if !errors.Is(err, db.ErrNotFound) {
			s.internalError(w, r, err)
			return
		}
	}
	now := s.now()
	a := &db.Album{ID: uuid.NewString(), Name: name, IsListed: true, IdempotencyKey: key, CreatedAt: now, UpdatedAt: now}
	if in.Description != nil {
		if len(*in.Description) > maxDescriptionLen {
			writeError(w, http.StatusBadRequest, "invalid_description", "description too long")
			return
		}
		a.Description = strings.TrimSpace(*in.Description)
	}
	if in.IsListed != nil {
		a.IsListed = *in.IsListed
	}
	if in.Password != nil && *in.Password != "" {
		if len(*in.Password) > maxPasswordLen {
			writeError(w, http.StatusBadRequest, "invalid_password", "password too long")
			return
		}
		h, err := auth.HashPassword(*in.Password)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		a.PasswordHash = h
		a.PasswordVersion = 1
	}
	if in.ParentID != nil {
		folderID, ok := s.resolveParentFolder(w, r, *in.ParentID)
		if !ok {
			return
		}
		a.FolderID = folderID
	}

	err := withUniqueSlug(s.rand, name, func(slug string) { a.Slug = slug }, func() error {
		return s.db.CreateAlbum(r.Context(), a)
	})
	if errors.Is(err, db.ErrIdempotencyKeyTaken) {
		// Lost a race with a retry of the same request.
		a, err = s.db.GetAlbumByIdempotencyKey(r.Context(), key)
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.albumOutput(a))
}

func (s *Server) updateAlbum(w http.ResponseWriter, r *http.Request) {
	a, ok := s.albumFromPath(w, r)
	if !ok {
		return
	}
	var in albumInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Name != nil {
		name, ok := validName(w, *in.Name)
		if !ok {
			return
		}
		a.Name = name
	}
	if in.Description != nil {
		if len(*in.Description) > maxDescriptionLen {
			writeError(w, http.StatusBadRequest, "invalid_description", "description too long")
			return
		}
		a.Description = strings.TrimSpace(*in.Description)
	}
	if in.IsListed != nil {
		a.IsListed = *in.IsListed
	}
	if in.ParentID != nil {
		folderID, ok := s.resolveParentFolder(w, r, *in.ParentID)
		if !ok {
			return
		}
		a.FolderID = folderID
	}
	if in.CoverPhotoID != nil {
		if *in.CoverPhotoID == "" {
			a.CoverPhotoID = ""
		} else {
			if _, err := s.db.GetPhoto(r.Context(), a.ID, *in.CoverPhotoID); err != nil {
				if errors.Is(err, db.ErrNotFound) {
					writeError(w, http.StatusUnprocessableEntity, "cover_photo_not_found", "")
					return
				}
				s.internalError(w, r, err)
				return
			}
			a.CoverPhotoID = *in.CoverPhotoID
		}
	}
	if in.Password != nil {
		switch pw := *in.Password; {
		case pw == "" && a.Protected():
			// Remove protection; invalidate existing sessions.
			a.PasswordHash = nil
			a.PasswordVersion++
		case pw == "":
			// Already public: nothing to do.
		case len(pw) > maxPasswordLen:
			writeError(w, http.StatusBadRequest, "invalid_password", "password too long")
			return
		case a.Protected() && auth.CheckPassword(a.PasswordHash, pw):
			// Unchanged password: keep hash and version so visitors stay logged in.
		default:
			h, err := auth.HashPassword(pw)
			if err != nil {
				s.internalError(w, r, err)
				return
			}
			a.PasswordHash = h
			a.PasswordVersion++
		}
	}
	a.UpdatedAt = s.now()
	if err := s.db.UpdateAlbum(r.Context(), a); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.albumOutput(a))
}

func (s *Server) deleteAlbum(w http.ResponseWriter, r *http.Request) {
	a, ok := s.albumFromPath(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteAlbum(r.Context(), a.ID); err != nil && !errors.Is(err, db.ErrNotFound) {
		s.internalError(w, r, err)
		return
	}
	if err := s.store.RemoveAlbum(a.ID); err != nil {
		// The database row is gone; gc will collect the directory.
		s.log.Warn("remove album files", "album", a.ID, "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

type publishedPhoto struct {
	ID          string `json:"id"`
	LrPhotoUUID string `json:"lr_photo_uuid"`
	ContentHash string `json:"content_hash"`
	Filename    string `json:"filename"`
}

func (s *Server) listPublishedPhotos(w http.ResponseWriter, r *http.Request) {
	a, ok := s.albumFromPath(w, r)
	if !ok {
		return
	}
	photos, err := s.db.ListPhotos(r.Context(), a.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]publishedPhoto, 0, len(photos))
	for _, p := range photos {
		out = append(out, publishedPhoto{ID: p.ID, LrPhotoUUID: p.LrPhotoUUID, ContentHash: p.ContentHash, Filename: p.Filename})
	}
	writeJSON(w, http.StatusOK, map[string]any{"photos": out})
}

func (s *Server) setPhotoOrder(w http.ResponseWriter, r *http.Request) {
	a, ok := s.albumFromPath(w, r)
	if !ok {
		return
	}
	var in struct {
		PhotoIDs []string `json:"photo_ids"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	for _, id := range in.PhotoIDs {
		if _, err := uuid.Parse(id); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_photo_id", id)
			return
		}
	}
	if err := s.db.SetPhotoOrder(r.Context(), a.ID, in.PhotoIDs); err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deletePhoto(w http.ResponseWriter, r *http.Request) {
	a, ok := s.albumFromPath(w, r)
	if !ok {
		return
	}
	photoID := r.PathValue("photo_id")
	if _, err := uuid.Parse(photoID); err != nil {
		writeError(w, http.StatusNotFound, "photo_not_found", "")
		return
	}
	err := s.db.DeletePhoto(r.Context(), a.ID, photoID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "photo_not_found", "")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.store.RemovePhoto(a.ID, photoID); err != nil {
		s.log.Warn("remove photo files", "album", a.ID, "photo", photoID, "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
}
