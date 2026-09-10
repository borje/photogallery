-- +goose Up
CREATE TABLE folders (
    id         TEXT PRIMARY KEY,
    parent_id  TEXT REFERENCES folders(id) ON DELETE CASCADE,
    slug       TEXT UNIQUE NOT NULL,
    name       TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX folders_parent ON folders(parent_id);

ALTER TABLE albums ADD COLUMN folder_id TEXT REFERENCES folders(id) ON DELETE CASCADE;
CREATE INDEX albums_folder ON albums(folder_id);

-- +goose Down
DROP INDEX albums_folder;
ALTER TABLE albums DROP COLUMN folder_id;
DROP TABLE folders;
