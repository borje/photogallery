// Package db provides SQLite access for albums, photos and API keys.
//
// All SQL is plain SQLite without driver-specific extensions so the schema
// stays portable. Timestamps are stored as RFC 3339 text in UTC.
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/pressly/goose/v3"
	sqlite "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Sentinel errors returned by the repository functions.
var (
	ErrNotFound    = errors.New("not found")
	ErrSlugTaken   = errors.New("slug already taken")
	ErrPhotoExists = errors.New("photo with this lr_photo_uuid already exists in album")
	// ErrIdempotencyKeyTaken: a row with the same client idempotency key
	// already exists; the caller should return that row instead.
	ErrIdempotencyKeyTaken = errors.New("idempotency key already used")
)

// DB wraps the SQL connection pool.
type DB struct {
	*sql.DB
}

// Open opens (creating if needed) the SQLite database at path, applies the
// pragmas the design requires and runs all pending migrations.
func Open(ctx context.Context, path string) (*DB, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite allows one writer at a time; a single connection avoids
	// SQLITE_BUSY entirely for this low-traffic service.
	sqldb.SetMaxOpenConns(1)
	sqldb.SetConnMaxLifetime(0)
	if err := sqldb.PingContext(ctx); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := Migrate(ctx, sqldb); err != nil {
		sqldb.Close()
		return nil, err
	}
	return &DB{sqldb}, nil
}

// Migrate applies all pending embedded migrations.
func Migrate(ctx context.Context, sqldb *sql.DB) error {
	p, err := newProvider(sqldb)
	if err != nil {
		return err
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

func newProvider(sqldb *sql.DB) (*goose.Provider, error) {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, sqldb, sub)
	if err != nil {
		return nil, fmt.Errorf("migration provider: %w", err)
	}
	return p, nil
}

// Ping verifies the database answers a trivial query.
func (d *DB) Ping(ctx context.Context) error {
	var one int
	return d.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
}

const sqliteConstraintUnique = 2067

func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqliteConstraintUnique
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func formatNullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func parseNullTime(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t := parseTime(s.String)
	return &t
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type rowScanner interface {
	Scan(dest ...any) error
}

// hasIdempotencyKey reports whether table has a row with the given key.
func (d *DB) hasIdempotencyKey(ctx context.Context, table, key string) bool {
	if key == "" {
		return false
	}
	var n int
	err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE idempotency_key = ?`, key).Scan(&n)
	return err == nil && n > 0
}
