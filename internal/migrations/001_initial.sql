-- +goose Up
CREATE TABLE artists (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    image_url TEXT,
    status TEXT NOT NULL DEFAULT 'watched' CHECK (status IN ('watched', 'partial', 'owned')),
    watch_new_releases INTEGER NOT NULL DEFAULT 0,
    watch_new_releases_since TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE albums (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    artist_id INTEGER NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    year INTEGER,
    provider TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    cover_url TEXT,
    record_type TEXT NOT NULL DEFAULT 'album',
    status TEXT NOT NULL DEFAULT 'watched' CHECK (status IN ('watched', 'owned', 'ignored')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE tracks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    album_id INTEGER NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    track_number INTEGER NOT NULL DEFAULT 1,
    disc_number INTEGER NOT NULL DEFAULT 1,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    provider TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'wanted' CHECK (status IN ('wanted', 'downloading', 'owned', 'ignored')),
    file_path TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE download_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    track_id INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    slskd_search_id TEXT,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'searching', 'downloading', 'organizing', 'complete', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    last_attempt TEXT,
    error TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE INDEX idx_albums_artist_id ON albums(artist_id);
CREATE INDEX idx_tracks_album_id ON tracks(album_id);
CREATE INDEX idx_tracks_status ON tracks(status);
CREATE INDEX idx_download_queue_status ON download_queue(status);
CREATE INDEX idx_download_queue_track_id ON download_queue(track_id);
CREATE UNIQUE INDEX idx_download_queue_track_active ON download_queue(track_id) WHERE status IN ('pending', 'searching', 'downloading', 'organizing');
CREATE INDEX idx_artists_provider ON artists(provider, provider_id);
CREATE INDEX idx_albums_provider ON albums(provider, provider_id);
CREATE INDEX idx_tracks_provider ON tracks(provider, provider_id);

-- +goose Down
DROP TABLE IF EXISTS download_queue;
DROP TABLE IF EXISTS tracks;
DROP TABLE IF EXISTS albums;
DROP TABLE IF EXISTS artists;
DROP TABLE IF EXISTS settings;
