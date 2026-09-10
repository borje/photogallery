package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Photo is one uploaded image in an album.
type Photo struct {
	ID          string
	AlbumID     string
	LrPhotoUUID string
	Filename    string
	MimeType    string
	SizeBytes   int64
	Width       int // display dimensions, after EXIF orientation
	Height      int
	Title       string
	Caption     string
	Keywords    []string
	TakenAt     string          // as delivered by Lightroom, no time zone
	Exif        json.RawMessage // JSON object or nil
	ContentHash string          // sha256 hex of the original file
	SortOrder   int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// PhotoRef identifies a photo's directory on disk.
type PhotoRef struct {
	AlbumID string
	ID      string
}

const photoCols = `id, album_id, lr_photo_uuid, filename, mime_type, size_bytes, width, height, COALESCE(title, ''), COALESCE(caption, ''), COALESCE(keywords, '[]'), COALESCE(taken_at, ''), COALESCE(exif, ''), COALESCE(content_hash, ''), sort_order, created_at, updated_at`

// photoOrder is the default display order.
const photoOrder = ` ORDER BY sort_order, taken_at, filename`

func scanPhoto(r rowScanner) (*Photo, error) {
	var p Photo
	var created, updated, keywords, exif string
	if err := r.Scan(&p.ID, &p.AlbumID, &p.LrPhotoUUID, &p.Filename, &p.MimeType, &p.SizeBytes, &p.Width, &p.Height,
		&p.Title, &p.Caption, &keywords, &p.TakenAt, &exif, &p.ContentHash, &p.SortOrder, &created, &updated); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(keywords), &p.Keywords); err != nil {
		p.Keywords = nil
	}
	if exif != "" {
		p.Exif = json.RawMessage(exif)
	}
	p.CreatedAt = parseTime(created)
	p.UpdatedAt = parseTime(updated)
	return &p, nil
}

func (p *Photo) keywordsJSON() string {
	if len(p.Keywords) == 0 {
		return "[]"
	}
	b, err := json.Marshal(p.Keywords)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func (p *Photo) exifText() any {
	if len(p.Exif) == 0 {
		return nil
	}
	return string(p.Exif)
}

// InsertPhoto adds a photo. Returns ErrPhotoExists if (album, lr_photo_uuid)
// is already present.
func (d *DB) InsertPhoto(ctx context.Context, p *Photo) error {
	_, err := d.ExecContext(ctx, `INSERT INTO photos (id, album_id, lr_photo_uuid, filename, mime_type, size_bytes, width, height, title, caption, keywords, taken_at, exif, content_hash, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, NULLIF(?, ''), ?, NULLIF(?, ''), ?, ?, ?)`,
		p.ID, p.AlbumID, p.LrPhotoUUID, p.Filename, p.MimeType, p.SizeBytes, p.Width, p.Height, p.Title, p.Caption, p.keywordsJSON(), p.TakenAt, p.exifText(), p.ContentHash, p.SortOrder, formatTime(p.CreatedAt), formatTime(p.UpdatedAt))
	if isUniqueViolation(err) {
		return ErrPhotoExists
	}
	if err != nil {
		return fmt.Errorf("insert photo: %w", err)
	}
	return nil
}

// UpdatePhoto rewrites the file- and metadata columns of an existing photo.
// ID, album, lr_photo_uuid, sort_order and created_at are left untouched.
func (d *DB) UpdatePhoto(ctx context.Context, p *Photo) error {
	res, err := d.ExecContext(ctx, `UPDATE photos SET filename = ?, mime_type = ?, size_bytes = ?, width = ?, height = ?, title = NULLIF(?, ''), caption = NULLIF(?, ''), keywords = ?, taken_at = NULLIF(?, ''), exif = ?, content_hash = NULLIF(?, ''), updated_at = ? WHERE id = ? AND album_id = ?`,
		p.Filename, p.MimeType, p.SizeBytes, p.Width, p.Height, p.Title, p.Caption, p.keywordsJSON(), p.TakenAt, p.exifText(), p.ContentHash, formatTime(p.UpdatedAt), p.ID, p.AlbumID)
	if err != nil {
		return fmt.Errorf("update photo: %w", err)
	}
	return checkAffected(res)
}

// GetPhoto returns a photo by album and id, or ErrNotFound.
func (d *DB) GetPhoto(ctx context.Context, albumID, id string) (*Photo, error) {
	p, err := scanPhoto(d.QueryRowContext(ctx, `SELECT `+photoCols+` FROM photos WHERE album_id = ? AND id = ?`, albumID, id))
	return p, wrapNotFound(err, "get photo")
}

// GetPhotoByLrUUID finds a photo by its Lightroom catalog uuid within an album.
func (d *DB) GetPhotoByLrUUID(ctx context.Context, albumID, lrUUID string) (*Photo, error) {
	p, err := scanPhoto(d.QueryRowContext(ctx, `SELECT `+photoCols+` FROM photos WHERE album_id = ? AND lr_photo_uuid = ?`, albumID, lrUUID))
	return p, wrapNotFound(err, "get photo by lr uuid")
}

// ListPhotos returns the album's photos in display order.
func (d *DB) ListPhotos(ctx context.Context, albumID string) ([]*Photo, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+photoCols+` FROM photos WHERE album_id = ?`+photoOrder, albumID)
	if err != nil {
		return nil, fmt.Errorf("list photos: %w", err)
	}
	defer rows.Close()
	var out []*Photo
	for rows.Next() {
		p, err := scanPhoto(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeletePhoto removes one photo row.
func (d *DB) DeletePhoto(ctx context.Context, albumID, id string) error {
	res, err := d.ExecContext(ctx, `DELETE FROM photos WHERE album_id = ? AND id = ?`, albumID, id)
	if err != nil {
		return fmt.Errorf("delete photo: %w", err)
	}
	return checkAffected(res)
}

// SetPhotoOrder assigns sort_order 1..n following ids. Photos in the album
// that are not listed are placed after them.
func (d *DB) SetPhotoOrder(ctx context.Context, albumID string, ids []string) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE photos SET sort_order = ? WHERE album_id = ?`, len(ids)+1, albumID); err != nil {
		return fmt.Errorf("reset order: %w", err)
	}
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE photos SET sort_order = ? WHERE album_id = ? AND id = ?`, i+1, albumID, id); err != nil {
			return fmt.Errorf("set order: %w", err)
		}
	}
	return tx.Commit()
}

// ListPhotoRefs returns (album, photo) id pairs for every photo. Used by gc.
func (d *DB) ListPhotoRefs(ctx context.Context) ([]PhotoRef, error) {
	rows, err := d.QueryContext(ctx, `SELECT album_id, id FROM photos`)
	if err != nil {
		return nil, fmt.Errorf("list photo refs: %w", err)
	}
	defer rows.Close()
	var out []PhotoRef
	for rows.Next() {
		var r PhotoRef
		if err := rows.Scan(&r.AlbumID, &r.ID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
