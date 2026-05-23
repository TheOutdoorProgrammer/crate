package db_test

import (
	"testing"

	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/models"
)

func newTestQueries(t *testing.T) *db.Queries {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return db.NewQueries(database)
}

func seedTrack(t *testing.T, q *db.Queries) int64 {
	t.Helper()
	artist := &models.Artist{Name: "Test Artist", Provider: "test", ProviderID: "a1", Status: models.ArtistStatusWatched}
	if err := q.CreateArtist(artist); err != nil {
		t.Fatal(err)
	}
	album := &models.Album{ArtistID: artist.ID, Title: "Test Album", Provider: "test", ProviderID: "al1", RecordType: "album", Status: models.AlbumStatusWatched}
	if err := q.CreateAlbum(album); err != nil {
		t.Fatal(err)
	}
	track := &models.Track{AlbumID: album.ID, Title: "Test Track", TrackNumber: 1, DiscNumber: 1, DurationMs: 200000, Provider: "test", ProviderID: "t1", Status: models.TrackStatusWanted}
	if err := q.CreateTrack(track); err != nil {
		t.Fatal(err)
	}
	return track.ID
}

func TestEnqueueDownloadSetsSourceUser(t *testing.T) {
	q := newTestQueries(t)
	trackID := seedTrack(t, q)

	if err := q.EnqueueDownload(trackID); err != nil {
		t.Fatal(err)
	}

	downloads, err := q.ListDownloads("")
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 1 {
		t.Fatalf("expected 1 download, got %d", len(downloads))
	}
	if downloads[0].Source != "user" {
		t.Errorf("expected source 'user', got %q", downloads[0].Source)
	}
}

func TestReenqueueDownloadSetsSourceScheduler(t *testing.T) {
	q := newTestQueries(t)
	trackID := seedTrack(t, q)

	if err := q.ReenqueueDownload(trackID); err != nil {
		t.Fatal(err)
	}

	downloads, err := q.ListDownloads("")
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 1 {
		t.Fatalf("expected 1 download, got %d", len(downloads))
	}
	if downloads[0].Source != "scheduler" {
		t.Errorf("expected source 'scheduler', got %q", downloads[0].Source)
	}
}

func TestEnqueueDownloadBatchSetsSourceUser(t *testing.T) {
	q := newTestQueries(t)
	trackID := seedTrack(t, q)

	queued, err := q.EnqueueDownloadBatch([]int64{trackID})
	if err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("expected 1 queued, got %d", queued)
	}

	downloads, err := q.ListDownloads("")
	if err != nil {
		t.Fatal(err)
	}
	if downloads[0].Source != "user" {
		t.Errorf("expected source 'user', got %q", downloads[0].Source)
	}
}

func TestListDownloadsWithTrackIncludesSource(t *testing.T) {
	q := newTestQueries(t)
	trackID := seedTrack(t, q)

	q.EnqueueDownload(trackID)

	downloads, err := q.ListDownloadsWithTrack("")
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 1 {
		t.Fatalf("expected 1 download, got %d", len(downloads))
	}
	if downloads[0].Source != "user" {
		t.Errorf("expected source 'user', got %q", downloads[0].Source)
	}
	if downloads[0].Track == nil {
		t.Fatal("expected track info")
	}
}

func TestListRetryableDownloadsIncludesSource(t *testing.T) {
	q := newTestQueries(t)
	trackID := seedTrack(t, q)

	q.ReenqueueDownload(trackID)
	q.ScheduleRetry(1, "2000-01-01T00:00:00Z", "test error")

	downloads, err := q.ListRetryableDownloads()
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 1 {
		t.Fatalf("expected 1 retryable download, got %d", len(downloads))
	}
	if downloads[0].Source != "scheduler" {
		t.Errorf("expected source 'scheduler', got %q", downloads[0].Source)
	}
}

func TestReenqueueDeletesOldFailedBeforeInsert(t *testing.T) {
	q := newTestQueries(t)
	trackID := seedTrack(t, q)

	q.EnqueueDownload(trackID)
	errMsg := "some error"
	q.UpdateDownloadStatus(1, models.DownloadStatusFailed, nil, &errMsg)

	if err := q.ReenqueueDownload(trackID); err != nil {
		t.Fatal(err)
	}

	downloads, err := q.ListDownloads("")
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 1 {
		t.Fatalf("expected 1 download after reenqueue (old failed deleted), got %d", len(downloads))
	}
	if downloads[0].Source != "scheduler" {
		t.Errorf("expected source 'scheduler', got %q", downloads[0].Source)
	}
	if downloads[0].Status != models.DownloadStatusPending {
		t.Errorf("expected status 'pending', got %q", downloads[0].Status)
	}
}

func TestReenqueueDoesNotDuplicateActivePending(t *testing.T) {
	q := newTestQueries(t)
	trackID := seedTrack(t, q)

	q.EnqueueDownload(trackID)
	q.ReenqueueDownload(trackID)

	downloads, err := q.ListDownloads("")
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 1 {
		t.Errorf("expected 1 download (no duplicate), got %d", len(downloads))
	}
	if downloads[0].Source != "user" {
		t.Errorf("expected original source 'user' preserved, got %q", downloads[0].Source)
	}
}
