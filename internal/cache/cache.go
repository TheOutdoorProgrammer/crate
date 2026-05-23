package cache

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Cache struct {
	db *sql.DB
}

func Open(path string) (*Cache, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(wal)")
	if err != nil {
		return nil, fmt.Errorf("open cache db: %w", err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS cache (
		key TEXT PRIMARY KEY,
		value BLOB NOT NULL,
		created_at TEXT NOT NULL,
		expires_at TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create cache table: %w", err)
	}

	c := &Cache{db: db}
	c.ClearExpired()
	return c, nil
}

func (c *Cache) Close() error {
	return c.db.Close()
}

func (c *Cache) Get(key string) ([]byte, bool) {
	var value []byte
	var expiresAt string
	err := c.db.QueryRow(`SELECT value, expires_at FROM cache WHERE key = ?`, key).Scan(&value, &expiresAt)
	if err != nil {
		return nil, false
	}
	t, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || time.Now().UTC().After(t) {
		c.db.Exec(`DELETE FROM cache WHERE key = ?`, key)
		return nil, false
	}
	return value, true
}

func (c *Cache) Set(key string, value []byte, ttl time.Duration) {
	now := time.Now().UTC()
	c.db.Exec(
		`INSERT INTO cache (key, value, created_at, expires_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = ?, created_at = ?, expires_at = ?`,
		key, value, now.Format(time.RFC3339), now.Add(ttl).Format(time.RFC3339),
		value, now.Format(time.RFC3339), now.Add(ttl).Format(time.RFC3339),
	)
}

func (c *Cache) Clear() {
	c.db.Exec(`DELETE FROM cache`)
}

func (c *Cache) ClearExpired() {
	c.db.Exec(`DELETE FROM cache WHERE expires_at < ?`, time.Now().UTC().Format(time.RFC3339))
}
