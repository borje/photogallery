-- +goose Up
CREATE TABLE albums (
    id               TEXT PRIMARY KEY,
    slug             TEXT UNIQUE NOT NULL,
    name             TEXT NOT NULL,
    description      TEXT,
    password_hash    BLOB,
    password_version INTEGER NOT NULL DEFAULT 0,
    is_listed        INTEGER NOT NULL DEFAULT 1,
    cover_photo_id   TEXT,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

CREATE TABLE photos (
    id             TEXT PRIMARY KEY,
    album_id       TEXT NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    lr_photo_uuid  TEXT NOT NULL,
    filename       TEXT NOT NULL,
    mime_type      TEXT NOT NULL,
    size_bytes     INTEGER NOT NULL,
    width          INTEGER NOT NULL,
    height         INTEGER NOT NULL,
    title          TEXT,
    caption        TEXT,
    keywords       TEXT,
    taken_at       TEXT,
    exif           TEXT,
    content_hash   TEXT,
    sort_order     INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    UNIQUE (album_id, lr_photo_uuid)
);
CREATE INDEX photos_album_sort ON photos(album_id, sort_order);

CREATE TABLE api_keys (
    id           TEXT PRIMARY KEY,
    key_prefix   TEXT NOT NULL,
    key_hash     BLOB NOT NULL,
    label        TEXT,
    created_at   TEXT NOT NULL,
    last_used_at TEXT,
    revoked_at   TEXT
);
CREATE INDEX api_keys_prefix ON api_keys(key_prefix);

-- +goose Down
DROP TABLE api_keys;
DROP TABLE photos;
DROP TABLE albums;
