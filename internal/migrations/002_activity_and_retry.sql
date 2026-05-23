-- +goose Up
ALTER TABLE tracks ADD COLUMN downloaded_from TEXT;
ALTER TABLE download_queue ADD COLUMN next_retry_at TEXT;

-- +goose Down
-- SQLite doesn't support DROP COLUMN before 3.35.0, so we leave the columns.
