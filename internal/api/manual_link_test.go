package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/models"
	"github.com/TheOutdoorProgrammer/crate/internal/provider"
)

func seedProviderAlbum(t *testing.T, q *db.Queries, artistID int64, title, pid string, year int) int64 {
	t.Helper()
	album := models.Album{
		ArtistID: artistID, Title: title, Year: intp(year),
		Provider: "test", ProviderID: pid, RecordType: "album", Status: models.AlbumStatusWatched,
	}
	if err := q.CreateAlbum(&album); err != nil {
		t.Fatalf("seed provider album %q: %v", title, err)
	}
	return album.ID
}

func seedWantedTrack(t *testing.T, q *db.Queries, albumID int64, title, pid string, trackNo int) int64 {
	t.Helper()
	tr := models.Track{
		AlbumID: albumID, Title: title, TrackNumber: trackNo, DiscNumber: 1, DurationMs: 200000,
		Provider: "test", ProviderID: pid, Status: models.TrackStatusWanted,
	}
	if err := q.CreateTrack(&tr); err != nil {
		t.Fatalf("seed wanted track %q: %v", title, err)
	}
	return tr.ID
}

// TestLinkAlbumMergesIntoSibling: a mistitled local album is manually merged
// into the provider gap album — owned files claim the matching wanted tracks,
// the provider doesn't know the bonus track so it moves over as owned+local, and
// the empty local shell is deleted.
func TestLinkAlbumMergesIntoSibling(t *testing.T) {
	e := newTestEnv(t)
	q := e.queries

	artist := models.Artist{Name: "Test Artist", Provider: "test", ProviderID: "1000", Status: models.ArtistStatusWatched}
	if err := q.CreateArtist(&artist); err != nil {
		t.Fatal(err)
	}

	// The provider gap album (as reconcile would have created it): all wanted.
	gapID := seedProviderAlbum(t, q, artist.ID, "Album One", "2000", 2023)
	seedWantedTrack(t, q, gapID, "Track A1", "3000", 1)
	seedWantedTrack(t, q, gapID, "Track A2", "3001", 2)

	// The user's mistitled local copy: owns Track A1 plus a bonus the provider lacks.
	localID := seedLocalAlbum(t, q, artist.ID, "album one (2023 rip)", "loc-a1", 2023)
	seedOwnedLocalTrack(t, q, localID, "Track A1", "loc-a1-t1", 1)
	seedOwnedLocalTrack(t, q, localID, "Hidden Bonus", "loc-a1-bonus", 99)

	w := e.do("POST", fmt.Sprintf("/api/albums/%d/link", localID), fmt.Sprintf(`{"target_album_id":%d}`, gapID))
	if w.Code != http.StatusOK {
		t.Fatalf("link status = %d, body %s", w.Code, w.Body.String())
	}

	// Local shell is gone.
	if _, err := q.GetAlbum(localID); err == nil {
		t.Error("local album still exists, want deleted after merge")
	}

	tracks, err := q.ListTracksByAlbum(gapID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 3 {
		t.Fatalf("gap album track count = %d, want 3 (A1 claimed, A2 wanted, bonus moved)", len(tracks))
	}
	a1 := trackByTitle(t, tracks, "Track A1")
	if a1.Status != models.TrackStatusOwned || a1.FilePath == nil || a1.Provider != "test" {
		t.Errorf("Track A1 = %s prov=%s file=%v, want owned test with file", a1.Status, a1.Provider, a1.FilePath)
	}
	a2 := trackByTitle(t, tracks, "Track A2")
	if a2.Status != models.TrackStatusWanted {
		t.Errorf("Track A2 = %s, want still wanted", a2.Status)
	}
	bonus := trackByTitle(t, tracks, "Hidden Bonus")
	if bonus.Provider != provider.LocalProvider || bonus.Status != models.TrackStatusOwned {
		t.Errorf("Hidden Bonus = %s/%s, want local/owned (moved extra)", bonus.Provider, bonus.Status)
	}
}

// TestLinkTrackClaimsWanted: a leftover local track in an already-linked album
// claims the wanted provider track and disappears.
func TestLinkTrackClaimsWanted(t *testing.T) {
	e := newTestEnv(t)
	q := e.queries

	artist := models.Artist{Name: "Test Artist", Provider: "test", ProviderID: "1000", Status: models.ArtistStatusWatched}
	if err := q.CreateArtist(&artist); err != nil {
		t.Fatal(err)
	}
	albumID := seedProviderAlbum(t, q, artist.ID, "Album One", "2000", 2023)
	wantedID := seedWantedTrack(t, q, albumID, "Track A2", "3001", 2)
	// A local file that didn't fold-match during reconcile, sitting in the album.
	seedOwnedLocalTrack(t, q, albumID, "Track A2 (Bonus Version)", "loc-a2", 2)
	localTracks, _ := q.ListTracksByAlbum(albumID)
	localID := trackByTitle(t, localTracks, "Track A2 (Bonus Version)").ID

	w := e.do("POST", fmt.Sprintf("/api/tracks/%d/link", localID), fmt.Sprintf(`{"target_track_id":%d}`, wantedID))
	if w.Code != http.StatusOK {
		t.Fatalf("link track status = %d, body %s", w.Code, w.Body.String())
	}

	claimed, err := q.GetTrack(wantedID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Status != models.TrackStatusOwned || claimed.FilePath == nil {
		t.Errorf("target = %s file=%v, want owned with file", claimed.Status, claimed.FilePath)
	}
	if _, err := q.GetTrack(localID); err == nil {
		t.Error("local track still exists, want deleted after claim")
	}
}

func TestLinkAlbumGuards(t *testing.T) {
	e := newTestEnv(t)
	q := e.queries

	a1 := models.Artist{Name: "A1", Provider: "test", ProviderID: "1000", Status: models.ArtistStatusWatched}
	a2 := models.Artist{Name: "A2", Provider: "test", ProviderID: "1001", Status: models.ArtistStatusWatched}
	if err := q.CreateArtist(&a1); err != nil {
		t.Fatal(err)
	}
	if err := q.CreateArtist(&a2); err != nil {
		t.Fatal(err)
	}
	localA1 := seedLocalAlbum(t, q, a1.ID, "Local", "loc-x", 2020)
	providerA2 := seedProviderAlbum(t, q, a2.ID, "Other", "2222", 2020)
	providerA1 := seedProviderAlbum(t, q, a1.ID, "Prov", "3333", 2020)

	// Cross-artist target rejected.
	if w := e.do("POST", fmt.Sprintf("/api/albums/%d/link", localA1), fmt.Sprintf(`{"target_album_id":%d}`, providerA2)); w.Code != http.StatusBadRequest {
		t.Errorf("cross-artist link status = %d, want 400", w.Code)
	}
	// A non-local source can't be linked.
	if w := e.do("POST", fmt.Sprintf("/api/albums/%d/link", providerA1), fmt.Sprintf(`{"target_album_id":%d}`, providerA1)); w.Code != http.StatusBadRequest {
		t.Errorf("non-local source link status = %d, want 400", w.Code)
	}
}
