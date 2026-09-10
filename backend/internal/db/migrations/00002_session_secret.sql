-- +goose Up
CREATE TABLE session_secret (
    id     INTEGER PRIMARY KEY CHECK (id = 1),
    secret BLOB NOT NULL
);

-- +goose Down
DROP TABLE session_secret;
