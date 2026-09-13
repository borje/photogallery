-- +goose Up
ALTER TABLE albums ADD COLUMN idempotency_key TEXT;
ALTER TABLE folders ADD COLUMN idempotency_key TEXT;
CREATE UNIQUE INDEX albums_idempotency ON albums(idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE UNIQUE INDEX folders_idempotency ON folders(idempotency_key) WHERE idempotency_key IS NOT NULL;

-- +goose Down
DROP INDEX folders_idempotency;
DROP INDEX albums_idempotency;
ALTER TABLE folders DROP COLUMN idempotency_key;
ALTER TABLE albums DROP COLUMN idempotency_key;
