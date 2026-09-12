package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/bege/smugbox/backend/internal/db"
	"github.com/bege/smugbox/backend/internal/image"
	"github.com/bege/smugbox/backend/internal/storage"
)

const (
	maxFieldLen    = 64 << 10 // per text form field
	maxFilenameLen = 255
	maxTakenAtLen  = 64
)

// uploadPhoto handles POST .../photos. If the album already has a photo with
// the same lr_photo_uuid the call becomes an update (idempotent retries).
func (s *Server) uploadPhoto(w http.ResponseWriter, r *http.Request) {
	a, ok := s.albumFromPath(w, r)
	if !ok {
		return
	}
	s.ingest(w, r, a, nil)
}

// replacePhoto handles PUT .../photos/{photo_id}: new file and/or metadata.
func (s *Server) replacePhoto(w http.ResponseWriter, r *http.Request) {
	a, ok := s.albumFromPath(w, r)
	if !ok {
		return
	}
	photoID := r.PathValue("photo_id")
	if _, err := uuid.Parse(photoID); err != nil {
		writeError(w, http.StatusNotFound, "photo_not_found", "")
		return
	}
	existing, err := s.db.GetPhoto(r.Context(), a.ID, photoID)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "photo_not_found", "")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.ingest(w, r, a, existing)
}

// upload is the parsed multipart request.
type upload struct {
	fields   map[string]string
	staged   *storage.Staged
	size     int64
	hash     string
	head     [3]byte
	headLen  int
	filename string // from the file part's Content-Disposition
}

// headCapture remembers the first bytes written through it.
type headCapture struct {
	up *upload
}

func (h headCapture) Write(p []byte) (int, error) {
	// headLen counts bytes across calls; i indexes this call's chunk.
	for i := 0; h.up.headLen < len(h.up.head) && i < len(p); i++ {
		h.up.head[h.up.headLen] = p[i]
		h.up.headLen++
	}
	return len(p), nil
}

func (s *Server) readUpload(w http.ResponseWriter, r *http.Request) (*upload, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadBytes+(1<<20))
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_multipart", err.Error())
		return nil, false
	}
	up := &upload{fields: map[string]string{}}
	hasher := sha256.New()
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			up.abort()
			if tooLarge(err) {
				writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "")
			} else {
				writeError(w, http.StatusBadRequest, "invalid_multipart", err.Error())
			}
			return nil, false
		}
		if part.FormName() == "file" {
			if up.staged != nil {
				up.abort()
				writeError(w, http.StatusBadRequest, "invalid_multipart", "more than one file part")
				return nil, false
			}
			st, err := s.store.NewStaged()
			if err != nil {
				s.internalError(w, r, err)
				return nil, false
			}
			up.staged = st
			up.filename = part.FileName()
			n, err := io.Copy(io.MultiWriter(st, hasher, headCapture{up}), part)
			if err != nil {
				up.abort()
				if tooLarge(err) {
					writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "")
				} else {
					s.internalError(w, r, fmt.Errorf("store upload: %w", err))
				}
				return nil, false
			}
			up.size = n
			continue
		}
		value, err := readField(part)
		if err != nil {
			up.abort()
			writeError(w, http.StatusBadRequest, "invalid_field", fmt.Sprintf("%s: %v", part.FormName(), err))
			return nil, false
		}
		up.fields[part.FormName()] = value
	}
	if up.staged != nil {
		up.hash = hex.EncodeToString(hasher.Sum(nil))
	}
	return up, true
}

func (up *upload) abort() {
	if up.staged != nil {
		up.staged.Abort()
	}
}

func tooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

func readField(part *multipart.Part) (string, error) {
	b, err := io.ReadAll(io.LimitReader(part, maxFieldLen+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxFieldLen {
		return "", errors.New("field too long")
	}
	return string(b), nil
}

// ingest stores a new or replacement photo. existing is nil for POST.
func (s *Server) ingest(w http.ResponseWriter, r *http.Request, a *db.Album, existing *db.Photo) {
	up, ok := s.readUpload(w, r)
	if !ok {
		return
	}
	defer up.abort()

	// Resolve the target row: POST with a known lr_photo_uuid is an update.
	lrUUID := strings.TrimSpace(up.fields["lr_photo_uuid"])
	if existing == nil {
		if lrUUID == "" || len(lrUUID) > maxNameLen {
			writeError(w, http.StatusBadRequest, "missing_lr_photo_uuid", "")
			return
		}
		p, err := s.db.GetPhotoByLrUUID(r.Context(), a.ID, lrUUID)
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			s.internalError(w, r, err)
			return
		}
		existing = p
	}
	isNew := existing == nil
	if isNew && up.staged == nil {
		writeError(w, http.StatusBadRequest, "missing_file", "")
		return
	}

	now := s.now()
	photo := existing
	if isNew {
		photo = &db.Photo{ID: uuid.NewString(), AlbumID: a.ID, LrPhotoUUID: lrUUID, MimeType: "image/jpeg", CreatedAt: now}
	}
	photo.UpdatedAt = now
	if !applyMetadata(w, photo, up.fields) {
		return
	}

	if up.staged != nil {
		if up.size == 0 || !image.IsJPEG(up.head[:up.headLen]) {
			writeError(w, http.StatusUnsupportedMediaType, "not_jpeg", "file must be a JPEG image")
			return
		}
		if err := up.staged.Sync(); err != nil {
			s.internalError(w, r, err)
			return
		}
		info, err := image.Probe(up.staged.Path())
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid_image", err.Error())
			return
		}
		photo.Width, photo.Height = info.Width, info.Height
		photo.SizeBytes = up.size
		photo.ContentHash = up.hash
		photo.MimeType = "image/jpeg"
		if photo.Filename == "" || up.fields["filename"] == "" {
			if fn := sanitizeFilename(up.filename); fn != "" {
				photo.Filename = fn
			}
		}

		// Render a thumb and throw it away: this decodes the whole file, so
		// a corrupt JPEG is rejected here rather than by the worker after
		// the client was told 201. The worker renders the copy that is kept,
		// which leaves the original as the only file this request commits.
		thumb, err := s.store.NewStaged()
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		defer thumb.Abort()
		if _, err := image.Derive(up.staged.Path(), info, []storage.Variant{image.Validate}, func(storage.Variant) (string, error) {
			return thumb.Path(), nil
		}); err != nil {
			s.log.Warn("derive thumb", "album", a.ID, "photo", photo.ID, "err", err)
			writeError(w, http.StatusUnprocessableEntity, "invalid_image", "could not process image")
			return
		}
		if !isNew {
			// Hide the photo while its stored variants describe the old
			// original; the worker shows it again once they are regenerated.
			if err := s.db.MarkVariantsPending(r.Context(), a.ID, photo.ID); err != nil {
				s.internalError(w, r, err)
				return
			}
		}
		if err := s.store.Commit(up.staged, a.ID, photo.ID, storage.Original); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	if photo.Filename == "" {
		photo.Filename = photo.ID + ".jpg"
	}

	var err error
	if isNew {
		err = s.db.InsertPhoto(r.Context(), photo)
	} else {
		err = s.db.UpdatePhoto(r.Context(), photo)
	}
	if err != nil {
		if isNew {
			_ = s.store.RemovePhoto(a.ID, photo.ID)
		}
		s.internalError(w, r, err)
		return
	}
	if up.staged != nil {
		s.variants.kick()
	}
	status := http.StatusOK
	if isNew {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]string{"id": photo.ID, "url": s.photoURL(a.Slug, photo.ID)})
}

// applyMetadata copies the optional text fields onto photo, validating them.
func applyMetadata(w http.ResponseWriter, photo *db.Photo, f map[string]string) bool {
	if v, ok := f["filename"]; ok {
		if fn := sanitizeFilename(v); fn != "" {
			photo.Filename = fn
		}
	}
	if v, ok := f["title"]; ok {
		photo.Title = strings.TrimSpace(v)
	}
	if v, ok := f["caption"]; ok {
		photo.Caption = strings.TrimSpace(v)
	}
	if v, ok := f["taken_at"]; ok {
		v = strings.TrimSpace(v)
		if len(v) > maxTakenAtLen {
			writeError(w, http.StatusBadRequest, "invalid_taken_at", "")
			return false
		}
		photo.TakenAt = v
	}
	if v, ok := f["keywords"]; ok {
		kws, err := parseKeywords(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_keywords", err.Error())
			return false
		}
		photo.Keywords = kws
	}
	if v, ok := f["exif"]; ok {
		v = strings.TrimSpace(v)
		if v == "" {
			photo.Exif = nil
		} else {
			var obj map[string]any
			if err := json.Unmarshal([]byte(v), &obj); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_exif", "exif must be a JSON object")
				return false
			}
			compact, _ := json.Marshal(obj)
			photo.Exif = compact
		}
	}
	return true
}

// parseKeywords accepts a JSON array of strings or a comma-separated list.
func parseKeywords(v string) ([]string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, nil
	}
	var out []string
	if strings.HasPrefix(v, "[") {
		if err := json.Unmarshal([]byte(v), &out); err != nil {
			return nil, errors.New("keywords must be a JSON array of strings")
		}
	} else {
		for _, k := range strings.Split(v, ",") {
			if k = strings.TrimSpace(k); k != "" {
				out = append(out, k)
			}
		}
	}
	clean := out[:0]
	for _, k := range out {
		if k = strings.TrimSpace(k); k != "" {
			clean = append(clean, k)
		}
	}
	return clean, nil
}

// sanitizeFilename keeps only the base name and strips characters that are
// unsafe in Content-Disposition or zip entries.
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '/', r == '\\', r == '"', r == ':', r == '*', r == '?', r == '<', r == '>', r == '|':
			b.WriteByte('_')
		case unicode.IsControl(r):
			// drop
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "." || out == ".." {
		return ""
	}
	if len(out) > maxFilenameLen {
		out = out[:maxFilenameLen]
	}
	return out
}
