package activity

import (
	"fmt"
	"testing"
	"time"
)

func TestRecordAndList(t *testing.T) {
	log, err := NewLog(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	log.Record("download_complete", "track", 1, "Downloaded track")
	log.Record("download_failed", "track", 2, "Failed download")

	items, err := log.List(10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	// Should be ordered DESC by created_at
	if items[0].Action != "download_failed" {
		t.Errorf("expected most recent first, got %q", items[0].Action)
	}
}

func TestListPagination(t *testing.T) {
	log, err := NewLog(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	for i := 0; i < 10; i++ {
		log.Record("test", "track", int64(i), fmt.Sprintf("event %d", i))
	}

	items, err := log.List(3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}

	items, err = log.List(3, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items on page 2, got %d", len(items))
	}

	items, err = log.List(3, 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item on last page, got %d", len(items))
	}

	items, err = log.List(3, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items past end, got %d", len(items))
	}
}

func TestCount(t *testing.T) {
	log, err := NewLog(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	count, err := log.Count()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}

	log.Record("test", "track", 1, "event")
	log.Record("test", "track", 2, "event")

	count, err = log.Count()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

func TestPurge(t *testing.T) {
	log, err := NewLog(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	// Insert an old record by writing directly with a past timestamp
	log.db.Exec(
		`INSERT INTO activity_log (action, entity_type, entity_id, details, created_at) VALUES (?, ?, ?, ?, ?)`,
		"old", "track", 1, "old event", time.Now().UTC().Add(-2*time.Hour).Format(time.RFC3339),
	)
	log.Record("new", "track", 2, "new event")

	deleted, err := log.Purge(1 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	items, err := log.List(10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 remaining, got %d", len(items))
	}
}
