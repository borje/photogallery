package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SessionSecret returns the persisted session-cookie signing secret, or
// ErrNotFound if none has been generated yet.
func (d *DB) SessionSecret(ctx context.Context) ([]byte, error) {
	var secret []byte
	err := d.QueryRowContext(ctx, `SELECT secret FROM session_secret WHERE id = 1`).Scan(&secret)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read session secret: %w", err)
	}
	return secret, nil
}

// SetSessionSecret stores the session secret generated on first boot.
func (d *DB) SetSessionSecret(ctx context.Context, secret []byte) error {
	_, err := d.ExecContext(ctx, `INSERT INTO session_secret (id, secret) VALUES (1, ?)`, secret)
	if err != nil {
		return fmt.Errorf("insert session secret: %w", err)
	}
	return nil
}
