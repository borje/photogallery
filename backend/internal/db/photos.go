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
	// VariantsReady is false while the display variants are still being
	// generated in the background. Visitors never see such a photo.
	VariantsReady bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// PhotoRef identifies a photo's directory on disk.
type PhotoRef struct {
	AlbumID string
	ID      string
}

const photoCols = `id, album_id, lr_photo_uuid, filename, mime_type, size_bytes, width, height, COALESCE(title, ''), COALESCE(caption, ''), COALESCE(keywords, '[]'), COALESCE(taken_at, ''), COALESCE(exif, ''), COALESCE(content_hash, ''), sort_order, variants_ready, created_at, updated_at`

// photoOrder is the default display order.
const photoOrder = ` ORDER BY sort_order, taken_at, filename`

func scanPhoto(r rowScanner) (*Photo, error) {
	var p Photo
	var created, updated, keywords, exif string
	var ready int
	if err := r.Scan(&p.ID, &p.AlbumID, &p.LrPhotoUUID, &p.Filename, &p.MimeType, &p.SizeBytes, &p.Width, &p.Height,
		&p.Title, &p.Caption, &keywords, &p.TakenAt, &exif, &p.ContentHash, &p.SortOrder, &ready, &created, &updated); err != nil {
		return nil, err
	}
	p.VariantsReady = ready != 0
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
	_, err := d.ExecContext(ctx, `INSERT INTO photos (id, album_id, lr_photo_uuid, filename, mime_type, size_bytes, width, height, title, caption, keywords, taken_at, exif, content_hash, sort_order, variants_ready, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, NULLIF(?, ''), ?, NULLIF(?, ''), ?, ?, ?, ?)`,
		p.ID, p.AlbumID, p.LrPhotoUUID, p.Filename, p.MimeType, p.SizeBytes, p.Width, p.Height, p.Title, p.Caption, p.keywordsJSON(), p.TakenAt, p.exifText(), p.ContentHash, p.SortOrder, boolToInt(p.VariantsReady), formatTime(p.CreatedAt), formatTime(p.UpdatedAt))
	if isUniqueViolation(err) {
		return ErrPhotoExists
	}
	if err != nil {
		return fmt.Errorf("insert photo: %w", err)
	}
	return nil
}

// UpdatePhoto rewrites the file- and metadata columns of an existing photo.
// ID, album, lr_photo_uuid, sort_order, variants_ready and created_at are
// left untouched.
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

// ListPhotos returns all of the album's photos in display order, including
// those whose variants are still pending. For visitors use ListReadyPhotos.
func (d *DB) ListPhotos(ctx context.Context, albumID string) ([]*Photo, error) {
	return d.listPhotos(ctx, `SELECT `+photoCols+` FROM photos WHERE album_id = ?`+photoOrder, albumID)
}

// ListReadyPhotos returns the album's photos whose variants exist, in
// display order. This is what visitors see.
func (d *DB) ListReadyPhotos(ctx context.Context, albumID string) ([]*Photo, error) {
	return d.listPhotos(ctx, `SELECT `+photoCols+` FROM photos WHERE album_id = ? AND variants_ready = 1`+photoOrder, albumID)
}

// ListPhotosPendingVariants returns every photo whose variants have not been
// generated yet, oldest update first. The background worker drains this
// list, and it is what makes an interrupted generation resume after a
// restart: the row is the queue.
func (d *DB) ListPhotosPendingVariants(ctx context.Context) ([]*Photo, error) {
	return d.listPhotos(ctx, `SELECT `+photoCols+` FROM photos WHERE variants_ready = 0 ORDER BY updated_at, id`)
}

func (d *DB) listPhotos(ctx context.Context, query string, args ...any) ([]*Photo, error) {
	rows, err := d.QueryContext(ctx, query, args...)
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

// MarkVariantsPending hides a photo from visitors until its variants are
// regenerated. Called before a replacement original is put in place.
func (d *DB) MarkVariantsPending(ctx context.Context, albumID, id string) error {
	res, err := d.ExecContext(ctx, `UPDATE photos SET variants_ready = 0 WHERE album_id = ? AND id = ?`, albumID, id)
	if err != nil {
		return fmt.Errorf("mark variants pending: %w", err)
	}
	return checkAffected(res)
}

// MarkVariantsReady publishes a photo to visitors once its variants exist.
// contentHash must match the row, so that variants generated from an
// original that has since been replaced never mark the new one ready. It
// reports whether the row was updated; false means the photo is gone or its
// original changed underneath the generation.
func (d *DB) MarkVariantsReady(ctx context.Context, albumID, id, contentHash string) (bool, error) {
	res, err := d.ExecContext(ctx, `UPDATE photos SET variants_ready = 1 WHERE album_id = ? AND id = ? AND content_hash = ?`, albumID, id, contentHash)
	if err != nil {
		return false, fmt.Errorf("mark variants ready: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
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
