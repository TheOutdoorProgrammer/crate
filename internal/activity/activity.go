package activity

import (
	"database/sql"
	"time"

	"github.com/TheOutdoorProgrammer/crate/internal/models"
	_ "modernc.org/sqlite"
)

type Log struct {
	db *sql.DB
}

func NewLog(dbPath string) (*Log, error) {
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS activity_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		action TEXT NOT NULL,
		entity_type TEXT NOT NULL,
		entity_id INTEGER NOT NULL DEFAULT 0,
		details TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
	)`)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_activity_created_at ON activity_log(created_at)`)
	if err != nil {
		return nil, err
	}

	return &Log{db: db}, nil
}

func (l *Log) Record(action, entityType string, entityID int64, details string) {
	l.db.Exec(
		`INSERT INTO activity_log (action, entity_type, entity_id, details, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		action, entityType, entityID, details,
		time.Now().UTC().Format(time.RFC3339),
	)
}

func (l *Log) List(limit, offset int) ([]models.ActivityLog, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := l.db.Query(
		`SELECT id, action, entity_type, entity_id, details, created_at
		 FROM activity_log ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.ActivityLog
	for rows.Next() {
		var a models.ActivityLog
		if err := rows.Scan(&a.ID, &a.Action, &a.EntityType, &a.EntityID, &a.Details, &a.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

func (l *Log) Count() (int, error) {
	var count int
	err := l.db.QueryRow(`SELECT COUNT(*) FROM activity_log`).Scan(&count)
	return count, err
}

func (l *Log) Purge(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan).Format(time.RFC3339)
	result, err := l.db.Exec(`DELETE FROM activity_log WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (l *Log) Close() error {
	return l.db.Close()
}
