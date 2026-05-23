-- +goose Up
ALTER TABLE download_queue ADD COLUMN source TEXT NOT NULL DEFAULT 'user';

-- +goose Down
ALTER TABLE download_queue DROP COLUMN source;
