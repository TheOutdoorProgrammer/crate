-- +goose Up
ALTER TABLE download_queue ADD COLUMN last_progress_bytes INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE download_queue DROP COLUMN last_progress_bytes;
