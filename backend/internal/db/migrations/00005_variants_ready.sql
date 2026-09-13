-- +goose Up
-- Display variants are generated in the background after an upload. A photo
-- is hidden from visitors until they exist. Photos that already exist had
-- their variants generated synchronously, so they are ready.
ALTER TABLE photos ADD COLUMN variants_ready INTEGER NOT NULL DEFAULT 0;
UPDATE photos SET variants_ready = 1;

-- +goose Down
ALTER TABLE photos DROP COLUMN variants_ready;
