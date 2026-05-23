-- +goose Up
CREATE TABLE IF NOT EXISTS slskd_blacklist (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL,
    filename TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_blacklist_user_file ON slskd_blacklist(username, filename);

-- +goose Down
DROP TABLE IF EXISTS slskd_blacklist;
