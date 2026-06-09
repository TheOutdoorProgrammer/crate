-- +goose Up
ALTER TABLE tracks ADD COLUMN downloaded_filename TEXT;

-- +goose Down
ALTER TABLE tracks DROP COLUMN downloaded_filename;
