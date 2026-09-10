package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// APIKey is a Lightroom plugin credential. Only the sha256 hash is stored.
type APIKey struct {
	ID         string
	Prefix     string
	Hash       []byte
	Label      string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

const apiKeyCols = `id, key_prefix, key_hash, COALESCE(label, ''), created_at, last_used_at, revoked_at`

func scanAPIKey(r rowScanner) (*APIKey, error) {
	var k APIKey
	var created string
	var lastUsed, revoked sql.NullString
	if err := r.Scan(&k.ID, &k.Prefix, &k.Hash, &k.Label, &created, &lastUsed, &revoked); err != nil {
		return nil, err
	}
	k.CreatedAt = parseTime(created)
	k.LastUsedAt = parseNullTime(lastUsed)
	k.RevokedAt = parseNullTime(revoked)
	return &k, nil
}

// CreateAPIKey stores a new key record.
func (d *DB) CreateAPIKey(ctx context.Context, k *APIKey) error {
	_, err := d.ExecContext(ctx, `INSERT INTO api_keys (id, key_prefix, key_hash, label, created_at, last_used_at, revoked_at) VALUES (?, ?, ?, NULLIF(?, ''), ?, ?, ?)`,
		k.ID, k.Prefix, k.Hash, k.Label, formatTime(k.CreatedAt), formatNullTime(k.LastUsedAt), formatNullTime(k.RevokedAt))
	if err != nil {
		return fmt.Errorf("insert api key: %w", err)
	}
	return nil
}

// ListAPIKeysByPrefix returns all keys (including revoked) sharing a prefix.
func (d *DB) ListAPIKeysByPrefix(ctx context.Context, prefix string) ([]*APIKey, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+apiKeyCols+` FROM api_keys WHERE key_prefix = ?`, prefix)
	if err != nil {
		return nil, fmt.Errorf("list api keys by prefix: %w", err)
	}
	defer rows.Close()
	return collectAPIKeys(rows)
}

// ListAPIKeys returns all keys, oldest first.
func (d *DB) ListAPIKeys(ctx context.Context) ([]*APIKey, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+apiKeyCols+` FROM api_keys ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()
	return collectAPIKeys(rows)
}

func collectAPIKeys(rows *sql.Rows) ([]*APIKey, error) {
	var out []*APIKey
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// TouchAPIKey records when a key was last used.
func (d *DB) TouchAPIKey(ctx context.Context, id string, at time.Time) error {
	_, err := d.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, formatTime(at), id)
	return err
}

// RevokeAPIKey marks a key as revoked. Returns ErrNotFound for unknown ids.
func (d *DB) RevokeAPIKey(ctx context.Context, id string, at time.Time) error {
	res, err := d.ExecContext(ctx, `UPDATE api_keys SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, formatTime(at), id)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	return checkAffected(res)
}
