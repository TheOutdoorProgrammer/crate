-- +goose Up
CREATE TABLE IF NOT EXISTS user_cooldowns (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_cooldowns_username ON user_cooldowns(username);
CREATE INDEX IF NOT EXISTS idx_cooldowns_expires ON user_cooldowns(expires_at);

-- +goose Down
DROP TABLE IF EXISTS user_cooldowns;
