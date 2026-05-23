-- +goose Up
DROP INDEX IF EXISTS idx_artists_provider;
DROP INDEX IF EXISTS idx_albums_provider;
DROP INDEX IF EXISTS idx_tracks_provider;
CREATE UNIQUE INDEX idx_artists_provider ON artists(provider, provider_id);
CREATE UNIQUE INDEX idx_albums_provider ON albums(provider, provider_id);
CREATE UNIQUE INDEX idx_tracks_provider ON tracks(provider, provider_id);

-- +goose Down
DROP INDEX IF EXISTS idx_artists_provider;
DROP INDEX IF EXISTS idx_albums_provider;
DROP INDEX IF EXISTS idx_tracks_provider;
CREATE INDEX idx_artists_provider ON artists(provider, provider_id);
CREATE INDEX idx_albums_provider ON albums(provider, provider_id);
CREATE INDEX idx_tracks_provider ON tracks(provider, provider_id);
