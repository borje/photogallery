package db

import (
	"context"
	"fmt"
	"time"
)

// Folder is a node in the album tree. It never holds photos directly.
type Folder struct {
	ID        string
	ParentID  string // "" = root
	Slug      string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// FolderSummary is a Folder with the aggregates used by the folder listing.
type FolderSummary struct {
	Folder
	CoverAlbumSlug string // slug of the first listed album in the subtree, "" if none
	NewestTakenAt  string // latest taken_at across the subtree, used for ordering
}

const folderCols = `f.id, COALESCE(f.parent_id, ''), f.slug, f.name, f.created_at, f.updated_at`

// nullIfEmpty maps "" to a SQL NULL parameter, used for the nullable
// parent_id / folder_id foreign keys where "" means root/no-folder.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func scanFolder(r rowScanner) (*Folder, error) {
	var f Folder
	var created, updated string
	if err := r.Scan(&f.ID, &f.ParentID, &f.Slug, &f.Name, &created, &updated); err != nil {
		return nil, err
	}
	f.CreatedAt = parseTime(created)
	f.UpdatedAt = parseTime(updated)
	return &f, nil
}

// CreateFolder inserts a new folder. Returns ErrSlugTaken on a slug collision.
func (d *DB) CreateFolder(ctx context.Context, f *Folder) error {
	_, err := d.ExecContext(ctx, `INSERT INTO folders (id, parent_id, slug, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		f.ID, nullIfEmpty(f.ParentID), f.Slug, f.Name, formatTime(f.CreatedAt), formatTime(f.UpdatedAt))
	if isUniqueViolation(err) {
		return ErrSlugTaken
	}
	if err != nil {
		return fmt.Errorf("insert folder: %w", err)
	}
	return nil
}

// GetFolder returns the folder with the given id or ErrNotFound.
func (d *DB) GetFolder(ctx context.Context, id string) (*Folder, error) {
	f, err := scanFolder(d.QueryRowContext(ctx, `SELECT `+folderCols+` FROM folders f WHERE f.id = ?`, id))
	return f, wrapNotFound(err, "get folder")
}

// GetFolderBySlug returns the folder with the given slug or ErrNotFound.
func (d *DB) GetFolderBySlug(ctx context.Context, slug string) (*Folder, error) {
	f, err := scanFolder(d.QueryRowContext(ctx, `SELECT `+folderCols+` FROM folders f WHERE f.slug = ?`, slug))
	return f, wrapNotFound(err, "get folder by slug")
}

// UpdateFolder writes name and parent_id. The slug is never changed.
func (d *DB) UpdateFolder(ctx context.Context, f *Folder) error {
	res, err := d.ExecContext(ctx, `UPDATE folders SET name = ?, parent_id = ?, updated_at = ? WHERE id = ?`,
		f.Name, nullIfEmpty(f.ParentID), formatTime(f.UpdatedAt), f.ID)
	if err != nil {
		return fmt.Errorf("update folder: %w", err)
	}
	return checkAffected(res)
}

// DeleteFolder removes the folder. ON DELETE CASCADE takes care of every
// descendant folder, every album in the subtree and their photos.
func (d *DB) DeleteFolder(ctx context.Context, id string) error {
	res, err := d.ExecContext(ctx, `DELETE FROM folders WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete folder: %w", err)
	}
	return checkAffected(res)
}

// ListAllFolders returns every folder. Used by the admin CLI.
func (d *DB) ListAllFolders(ctx context.Context) ([]*Folder, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+folderCols+` FROM folders f ORDER BY f.created_at`)
	if err != nil {
		return nil, fmt.Errorf("list all folders: %w", err)
	}
	defer rows.Close()
	var out []*Folder
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FolderChain returns the path from the root folder down to and including
// folderID. Empty folderID (root) returns nil.
func (d *DB) FolderChain(ctx context.Context, folderID string) ([]*Folder, error) {
	if folderID == "" {
		return nil, nil
	}
	rows, err := d.QueryContext(ctx, `
		WITH RECURSIVE up(id, parent_id, slug, name, created_at, updated_at, depth) AS (
			SELECT id, parent_id, slug, name, created_at, updated_at, 0 FROM folders WHERE id = ?
		UNION ALL
			SELECT f.id, f.parent_id, f.slug, f.name, f.created_at, f.updated_at, up.depth + 1
			FROM folders f JOIN up ON f.id = up.parent_id
		)
		SELECT id, COALESCE(parent_id, ''), slug, name, created_at, updated_at FROM up ORDER BY depth DESC`, folderID)
	if err != nil {
		return nil, fmt.Errorf("folder chain: %w", err)
	}
	defer rows.Close()
	var out []*Folder
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SubtreeAlbumIDs returns the ids of every album in or beneath folderID.
// Used before deleting a folder to remove the albums' files from disk.
func (d *DB) SubtreeAlbumIDs(ctx context.Context, folderID string) ([]string, error) {
	rows, err := d.QueryContext(ctx, `
		WITH RECURSIVE sub(id) AS (
			SELECT id FROM folders WHERE id = ?
		UNION ALL
			SELECT f.id FROM folders f JOIN sub s ON f.parent_id = s.id
		)
		SELECT a.id FROM albums a WHERE a.folder_id IN (SELECT id FROM sub)`, folderID)
	if err != nil {
		return nil, fmt.Errorf("subtree album ids: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func scanFolderSummary(r rowScanner) (*FolderSummary, error) {
	var s FolderSummary
	var created, updated string
	if err := r.Scan(&s.ID, &s.ParentID, &s.Slug, &s.Name, &created, &updated, &s.CoverAlbumSlug, &s.NewestTakenAt); err != nil {
		return nil, err
	}
	s.CreatedAt = parseTime(created)
	s.UpdatedAt = parseTime(updated)
	return &s, nil
}

// ListChildFolders returns the immediate child folders of parentID (""
// meaning root) that have at least one listed album somewhere in their
// subtree, newest-content first. Each summary's cover is the newest listed
// album anywhere beneath it.
func (d *DB) ListChildFolders(ctx context.Context, parentID string) ([]*FolderSummary, error) {
	rows, err := d.QueryContext(ctx, `
		WITH RECURSIVE sub(root_id, folder_id) AS (
			SELECT id, id FROM folders WHERE parent_id IS ?
		UNION ALL
			SELECT s.root_id, f.id FROM folders f JOIN sub s ON f.parent_id = s.folder_id
		), av AS (
			SELECT s.root_id, a.slug, a.created_at,
			       COALESCE((SELECT MAX(p.taken_at) FROM photos p
			                  WHERE p.album_id = a.id AND p.taken_at IS NOT NULL AND p.taken_at <> ''), '') AS taken_to
			  FROM sub s JOIN albums a ON a.folder_id = s.folder_id
			 WHERE a.is_listed = 1
		)
		SELECT f.id, COALESCE(f.parent_id, ''), f.slug, f.name, f.created_at, f.updated_at,
		       COALESCE((SELECT slug FROM av WHERE av.root_id = f.id
		                  ORDER BY taken_to DESC, created_at DESC LIMIT 1), ''),
		       COALESCE((SELECT MAX(taken_to) FROM av WHERE av.root_id = f.id), '')
		  FROM folders f
		 WHERE f.parent_id IS ? AND EXISTS (SELECT 1 FROM av WHERE av.root_id = f.id)
		 ORDER BY 8 DESC, f.created_at DESC`, nullIfEmpty(parentID), nullIfEmpty(parentID))
	if err != nil {
		return nil, fmt.Errorf("list child folders: %w", err)
	}
	defer rows.Close()
	var out []*FolderSummary
	for rows.Next() {
		s, err := scanFolderSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListAlbumSummariesIn returns album summaries directly inside folderID
// (""  meaning root), most recently photographed first.
func (d *DB) ListAlbumSummariesIn(ctx context.Context, folderID string, listedOnly bool) ([]*AlbumSummary, error) {
	rows, err := d.QueryContext(ctx, summarySelect+` WHERE a.folder_id IS ? AND (? = 0 OR a.is_listed = 1) ORDER BY taken_to DESC, a.created_at DESC`,
		nullIfEmpty(folderID), boolToInt(listedOnly))
	if err != nil {
		return nil, fmt.Errorf("list album summaries in folder: %w", err)
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
