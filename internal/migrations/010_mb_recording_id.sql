-- +goose Up
ALTER TABLE tracks ADD COLUMN mb_recording_id TEXT;

-- +goose Down
ALTER TABLE tracks DROP COLUMN mb_recording_id;
