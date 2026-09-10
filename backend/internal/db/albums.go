package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Album is a published collection.
type Album struct {
	ID              string
	FolderID        string // "" = root
	Slug            string
	Name            string
	Description     string
	PasswordHash    []byte // nil or empty = public album
	PasswordVersion int
	IsListed        bool
	CoverPhotoID    string // "" = first photo in sort order
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Protected reports whether the album requires a password.
func (a *Album) Protected() bool { return len(a.PasswordHash) > 0 }

// AlbumSummary is an Album with aggregates used by the album list.
type AlbumSummary struct {
	Album
	PhotoCount    int
	TakenFrom     string // earliest taken_at, "" if none
	TakenTo       string // latest taken_at, "" if none
	ResolvedCover string // cover photo id after fallback to first photo, "" if empty album
}

const albumCols = `a.id, COALESCE(a.folder_id, ''), a.slug, a.name, COALESCE(a.description, ''), a.password_hash, a.password_version, a.is_listed, COALESCE(a.cover_photo_id, ''), a.created_at, a.updated_at`

func scanAlbum(r rowScanner) (*Album, error) {
	var a Album
	var created, updated string
	var listed int
	if err := r.Scan(&a.ID, &a.FolderID, &a.Slug, &a.Name, &a.Description, &a.PasswordHash, &a.PasswordVersion, &listed, &a.CoverPhotoID, &created, &updated); err != nil {
		return nil, err
	}
	a.IsListed = listed != 0
	a.CreatedAt = parseTime(created)
	a.UpdatedAt = parseTime(updated)
	return &a, nil
}

// CreateAlbum inserts a new album. Returns ErrSlugTaken on a slug collision.
func (d *DB) CreateAlbum(ctx context.Context, a *Album) error {
	_, err := d.ExecContext(ctx, `INSERT INTO albums (id, folder_id, slug, name, description, password_hash, password_version, is_listed, cover_photo_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, NULLIF(?, ''), ?, ?)`,
		a.ID, nullIfEmpty(a.FolderID), a.Slug, a.Name, a.Description, a.PasswordHash, a.PasswordVersion, boolToInt(a.IsListed), a.CoverPhotoID, formatTime(a.CreatedAt), formatTime(a.UpdatedAt))
	if isUniqueViolation(err) {
		return ErrSlugTaken
	}
	if err != nil {
		return fmt.Errorf("insert album: %w", err)
	}
	return nil
}

// GetAlbum returns the album with the given id or ErrNotFound.
func (d *DB) GetAlbum(ctx context.Context, id string) (*Album, error) {
	a, err := scanAlbum(d.QueryRowContext(ctx, `SELECT `+albumCols+` FROM albums a WHERE a.id = ?`, id))
	return a, wrapNotFound(err, "get album")
}

// GetAlbumBySlug returns the album with the given slug or ErrNotFound.
func (d *DB) GetAlbumBySlug(ctx context.Context, slug string) (*Album, error) {
	a, err := scanAlbum(d.QueryRowContext(ctx, `SELECT `+albumCols+` FROM albums a WHERE a.slug = ?`, slug))
	return a, wrapNotFound(err, "get album by slug")
}

// UpdateAlbum writes all mutable columns of a, including which folder it
// lives in. The slug is never changed.
func (d *DB) UpdateAlbum(ctx context.Context, a *Album) error {
	res, err := d.ExecContext(ctx, `UPDATE albums SET folder_id = ?, name = ?, description = NULLIF(?, ''), password_hash = ?, password_version = ?, is_listed = ?, cover_photo_id = NULLIF(?, ''), updated_at = ? WHERE id = ?`,
		nullIfEmpty(a.FolderID), a.Name, a.Description, a.PasswordHash, a.PasswordVersion, boolToInt(a.IsListed), a.CoverPhotoID, formatTime(a.UpdatedAt), a.ID)
	if err != nil {
		return fmt.Errorf("update album: %w", err)
	}
	return checkAffected(res)
}

// DeleteAlbum removes the album and (via ON DELETE CASCADE) its photos.
func (d *DB) DeleteAlbum(ctx context.Context, id string) error {
	res, err := d.ExecContext(ctx, `DELETE FROM albums WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete album: %w", err)
	}
	return checkAffected(res)
}

// ListAlbums returns every album, newest first. Used by the admin CLI.
func (d *DB) ListAlbums(ctx context.Context) ([]*Album, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+albumCols+` FROM albums a ORDER BY a.created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list albums: %w", err)
	}
	defer rows.Close()
	var out []*Album
	for rows.Next() {
		a, err := scanAlbum(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

const summarySelect = `SELECT ` + albumCols + `,
	(SELECT COUNT(*) FROM photos p WHERE p.album_id = a.id) AS photo_count,
	COALESCE((SELECT MIN(p.taken_at) FROM photos p WHERE p.album_id = a.id AND p.taken_at IS NOT NULL AND p.taken_at <> ''), '') AS taken_from,
	COALESCE((SELECT MAX(p.taken_at) FROM photos p WHERE p.album_id = a.id AND p.taken_at IS NOT NULL AND p.taken_at <> ''), '') AS taken_to,
	COALESCE(
		(SELECT p.id FROM photos p WHERE p.album_id = a.id AND p.id = a.cover_photo_id),
		(SELECT p.id FROM photos p WHERE p.album_id = a.id ORDER BY p.sort_order, p.taken_at, p.filename LIMIT 1),
		'') AS cover
	FROM albums a`

func scanSummary(r rowScanner) (*AlbumSummary, error) {
	var s AlbumSummary
	var created, updated string
	var listed int
	if err := r.Scan(&s.ID, &s.FolderID, &s.Slug, &s.Name, &s.Description, &s.PasswordHash, &s.PasswordVersion, &listed, &s.CoverPhotoID, &created, &updated,
		&s.PhotoCount, &s.TakenFrom, &s.TakenTo, &s.ResolvedCover); err != nil {
		return nil, err
	}
	s.IsListed = listed != 0
	s.CreatedAt = parseTime(created)
	s.UpdatedAt = parseTime(updated)
	return &s, nil
}

// ListAlbumSummaries returns albums with aggregates, most recently
// photographed first. With listedOnly, unlisted albums are excluded.
func (d *DB) ListAlbumSummaries(ctx context.Context, listedOnly bool) ([]*AlbumSummary, error) {
	rows, err := d.QueryContext(ctx, summarySelect+` WHERE (? = 0 OR a.is_listed = 1) ORDER BY taken_to DESC, a.created_at DESC`, boolToInt(listedOnly))
	if err != nil {
		return nil, fmt.Errorf("list album summaries: %w", err)
	}
	defer rows.Close()
	var out []*AlbumSummary
	for rows.Next() {
		s, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetAlbumSummaryBySlug returns one album with aggregates or ErrNotFound.
func (d *DB) GetAlbumSummaryBySlug(ctx context.Context, slug string) (*AlbumSummary, error) {
	s, err := scanSummary(d.QueryRowContext(ctx, summarySelect+` WHERE a.slug = ?`, slug))
	return s, wrapNotFound(err, "get album summary")
}

func wrapNotFound(err error, op string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", op, err)
}

func checkAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
