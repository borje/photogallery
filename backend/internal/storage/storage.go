// Package storage lays out photo files on local disk.
//
// Layout: <root>/photos/<album uuid>/<photo uuid>/<variant>.jpg. Paths are
// built only from validated UUIDs and a whitelist of variant names, so no
// user-controlled string ever becomes part of a path. Files are written to
// <root>/incoming first and renamed into place, which is atomic on one
// filesystem.
package storage

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// Variant names one stored rendition of a photo.
type Variant string

// The stored variants. Original is exactly what Lightroom exported.
const (
	Original Variant = "original"
	Large    Variant = "large"
	Medium   Variant = "medium"
	Small    Variant = "small"
	Thumb    Variant = "thumb"
	Blur     Variant = "blur"
)

var variants = map[Variant]bool{Original: true, Large: true, Medium: true, Small: true, Thumb: true, Blur: true}

// ParseVariant validates a variant name from a URL.
func ParseVariant(s string) (Variant, bool) {
	v := Variant(s)
	return v, variants[v]
}

// Filename is the on-disk name of the variant.
func (v Variant) Filename() string { return string(v) + ".jpg" }

// ErrInvalidID is returned when an id is not a UUID or a variant is unknown.
var ErrInvalidID = errors.New("invalid id or variant")

// Store is a photo file store rooted at one directory.
type Store struct {
	root     string
	photos   string
	incoming string
}

// New creates the store directories under root if needed.
func New(root string) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	s := &Store{root: abs, photos: filepath.Join(abs, "photos"), incoming: filepath.Join(abs, "incoming")}
	for _, d := range []string{s.photos, s.incoming} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("create %s: %w", d, err)
		}
	}
	return s, nil
}

// Root returns the absolute data directory.
func (s *Store) Root() string { return s.root }

func validID(id string) bool {
	u, err := uuid.Parse(id)
	return err == nil && u.String() == id
}

// AlbumDir returns the directory holding an album's photos.
func (s *Store) AlbumDir(albumID string) (string, error) {
	if !validID(albumID) {
		return "", ErrInvalidID
	}
	return filepath.Join(s.photos, albumID), nil
}

// PhotoDir returns the directory holding one photo's variants.
func (s *Store) PhotoDir(albumID, photoID string) (string, error) {
	if !validID(albumID) || !validID(photoID) {
		return "", ErrInvalidID
	}
	return filepath.Join(s.photos, albumID, photoID), nil
}

// Path returns the file path of a variant.
func (s *Store) Path(albumID, photoID string, v Variant) (string, error) {
	if !variants[v] {
		return "", ErrInvalidID
	}
	dir, err := s.PhotoDir(albumID, photoID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, v.Filename()), nil
}

// Stat returns file info for a variant, or an error satisfying
// errors.Is(err, fs.ErrNotExist) when it is absent.
func (s *Store) Stat(albumID, photoID string, v Variant) (os.FileInfo, error) {
	p, err := s.Path(albumID, photoID, v)
	if err != nil {
		return nil, err
	}
	return os.Stat(p)
}

// Open opens a variant for reading.
func (s *Store) Open(albumID, photoID string, v Variant) (*os.File, error) {
	p, err := s.Path(albumID, photoID, v)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

// Staged is a file being written to the incoming directory. Call Commit to
// move it into place or Abort to discard it. Abort after Commit is a no-op.
type Staged struct {
	f    *os.File
	path string
	done bool
}

// NewStaged creates a new temporary file in the incoming directory.
func (s *Store) NewStaged() (*Staged, error) {
	f, err := os.CreateTemp(s.incoming, "upload-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create staging file: %w", err)
	}
	return &Staged{f: f, path: f.Name()}, nil
}

// Write appends to the staged file.
func (st *Staged) Write(p []byte) (int, error) { return st.f.Write(p) }

// Path is the temporary path, usable for reading before Commit (for
// probing or generating derivatives).
func (st *Staged) Path() string { return st.path }

// Sync flushes written data to disk and closes the descriptor. It is safe to
// call more than once; other processes (libvips) may then write to Path.
func (st *Staged) Sync() error {
	if st.f == nil {
		return nil
	}
	err := st.f.Sync()
	cerr := st.f.Close()
	st.f = nil
	if err != nil {
		return err
	}
	return cerr
}

// Abort removes the temporary file unless it has been committed.
func (st *Staged) Abort() {
	if st.done {
		return
	}
	if st.f != nil {
		st.f.Close()
		st.f = nil
	}
	os.Remove(st.path)
	st.done = true
}

// Commit moves a staged file into place as the given variant, replacing any
// previous file atomically.
func (s *Store) Commit(st *Staged, albumID, photoID string, v Variant) error {
	if st.done {
		return errors.New("staged file already committed or aborted")
	}
	dst, err := s.Path(albumID, photoID, v)
	if err != nil {
		return err
	}
	if err := st.Sync(); err != nil {
		return fmt.Errorf("sync staged file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create photo dir: %w", err)
	}
	if err := os.Rename(st.path, dst); err != nil {
		return fmt.Errorf("rename into place: %w", err)
	}
	st.done = true
	return nil
}

// WriteFile stores r as the given variant in one step.
func (s *Store) WriteFile(albumID, photoID string, v Variant, r io.Reader) (int64, error) {
	if _, err := s.Path(albumID, photoID, v); err != nil {
		return 0, err
	}
	st, err := s.NewStaged()
	if err != nil {
		return 0, err
	}
	defer st.Abort()
	n, err := io.Copy(st, r)
	if err != nil {
		return n, err
	}
	return n, s.Commit(st, albumID, photoID, v)
}

// RemovePhoto deletes a photo directory and the album directory if it became empty.
func (s *Store) RemovePhoto(albumID, photoID string) error {
	dir, err := s.PhotoDir(albumID, photoID)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove photo dir: %w", err)
	}
	os.Remove(filepath.Dir(dir)) // only succeeds when empty
	return nil
}

// RemoveAlbum deletes an album directory with all photos.
func (s *Store) RemoveAlbum(albumID string) error {
	dir, err := s.AlbumDir(albumID)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove album dir: %w", err)
	}
	return nil
}

// Orphans returns photo directories (and stray entries under photos/) for
// which known reports false. Album directories without any photo directory
// are reported too.
func (s *Store) Orphans(known func(albumID, photoID string) bool) ([]string, error) {
	albums, err := os.ReadDir(s.photos)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, a := range albums {
		adir := filepath.Join(s.photos, a.Name())
		if !a.IsDir() || !validID(a.Name()) {
			out = append(out, adir)
			continue
		}
		entries, err := os.ReadDir(adir)
		if err != nil {
			return nil, err
		}
		if len(entries) == 0 {
			out = append(out, adir)
			continue
		}
		for _, p := range entries {
			pdir := filepath.Join(adir, p.Name())
			if !p.IsDir() || !validID(p.Name()) || !known(a.Name(), p.Name()) {
				out = append(out, pdir)
			}
		}
	}
	return out, nil
}

// StaleIncoming returns staging files older than maxAge, left behind by
// crashes mid-upload.
func (s *Store) StaleIncoming(now time.Time, maxAge time.Duration) ([]string, error) {
	entries, err := os.ReadDir(s.incoming)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if now.Sub(info.ModTime()) > maxAge {
			out = append(out, filepath.Join(s.incoming, e.Name()))
		}
	}
	return out, nil
}

// Remove deletes one variant file if present.
func (s *Store) Remove(albumID, photoID string, v Variant) error {
	p, err := s.Path(albumID, photoID, v)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
