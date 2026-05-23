-- +goose Up
ALTER TABLE tracks ADD COLUMN download_format TEXT;
ALTER TABLE tracks ADD COLUMN download_bitrate INTEGER;

-- Seed default quality tiers
INSERT INTO settings (key, value) VALUES ('quality_tiers', '[{"format":"flac","label":"FLAC"},{"format":"mp3","min_bitrate":320,"label":"MP3 320kbps"},{"format":"mp3","min_bitrate":256,"label":"MP3 256kbps"}]')
ON CONFLICT(key) DO NOTHING;

-- Track which artist was last scanned for upgrades
INSERT INTO settings (key, value) VALUES ('upgrade_last_artist_id', '0')
ON CONFLICT(key) DO NOTHING;

-- +goose Down
-- SQLite doesn't support DROP COLUMN before 3.35.0; recreate table if needed.
