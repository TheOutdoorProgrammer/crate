package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/models"
	"github.com/TheOutdoorProgrammer/crate/internal/provider"
)

func strp(s string) *string { return &s }
func intp(i int) *int       { return &i }

// seedOwnedLocalTrack inserts a track exactly as the importer would: owned, on
// the local provider, with a file on disk.
func seedOwnedLocalTrack(t *testing.T, q *db.Queries, albumID int64, title, pid string, trackNo int) {
	t.Helper()
	fp := "somewhere/" + pid + ".flac"
	fmtStr := "flac"
	if err := q.CreateImportedTrack(&models.Track{
		AlbumID:         albumID,
		Title:           title,
		TrackNumber:     trackNo,
		DiscNumber:      1,
		DurationMs:      200000,
		Provider:        provider.LocalProvider,
		ProviderID:      pid,
		Status:          models.TrackStatusOwned,
		FilePath:        strp(fp),
		DownloadFormat:  &fmtStr,
		DownloadBitrate: intp(0),
	}); err != nil {
		t.Fatalf("seed local track %q: %v", title, err)
	}
}

func seedLocalAlbum(t *testing.T, q *db.Queries, artistID int64, title, pid string, year int) int64 {
	t.Helper()
	album := models.Album{
		ArtistID:   artistID,
		Title:      title,
		Year:       intp(year),
		Provider:   provider.LocalProvider,
		ProviderID: pid,
		RecordType: "album",
		Status:     models.AlbumStatusOwned,
	}
	if err := q.CreateAlbum(&album); err != nil {
		t.Fatalf("seed local album %q: %v", title, err)
	}
	return album.ID
}

func albumByTitle(t *testing.T, albums []models.Album, title string) models.Album {
	t.Helper()
	for _, a := range albums {
		if a.Title == title {
			return a
		}
	}
	t.Fatalf("album %q not found in %d albums", title, len(albums))
	return models.Album{}
}

func trackByTitle(t *testing.T, tracks []models.Track, title string) models.Track {
	t.Helper()
	for _, tr := range tracks {
		if tr.Title == title {
			return tr
		}
	}
	t.Fatalf("track %q not found in %d tracks", title, len(tracks))
	return models.Track{}
}

// TestReconcileLocalArtistPromotion is the headline flow: an imported (local)
// artist who owns part of one album and a bootleg the provider doesn't know is
// linked to the real provider. Owned files must be preserved (relinked, not
// re-fetched), the missing track filled as wanted, the album the user lacks
// entirely created as a gap, and the local-only content left untouched.
func TestReconcileLocalArtistPromotion(t *testing.T) {
	e := newTestEnv(t)
	q := e.queries

	artist := models.Artist{Name: "Test Artist", Provider: provider.LocalProvider, ProviderID: "loc-artist", Status: models.ArtistStatusOwned}
	if err := q.CreateArtist(&artist); err != nil {
		t.Fatal(err)
	}

	// Album One: user owns Track A1 (provider 3000) plus a bonus cut the provider
	// doesn't list. Missing Track A2 (provider 3001).
	albumOneID := seedLocalAlbum(t, q, artist.ID, "Album One", "loc-a1", 2023)
	seedOwnedLocalTrack(t, q, albumOneID, "Track A1", "loc-a1-t1", 1)
	seedOwnedLocalTrack(t, q, albumOneID, "Bonus Cut", "loc-a1-bonus", 99)

	// Rare Bootleg: not in the provider discography at all.
	bootlegID := seedLocalAlbum(t, q, artist.ID, "Rare Bootleg", "loc-boot", 2018)
	seedOwnedLocalTrack(t, q, bootlegID, "Bootleg Jam", "loc-boot-t1", 1)

	w := e.do("POST", fmt.Sprintf("/api/relink/artist/%d", artist.ID), `{"provider_id":"1000"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("relink status = %d, body %s", w.Code, w.Body.String())
	}

	// Artist promoted to the real provider and now watched.
	got, err := q.GetArtist(artist.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "test" || got.ProviderID != "1000" {
		t.Errorf("artist provider = %s/%s, want test/1000", got.Provider, got.ProviderID)
	}
	if got.Status != models.ArtistStatusWatched {
		t.Errorf("artist status = %s, want watched", got.Status)
	}

	albums, err := q.ListAlbumsByArtist(artist.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 3 {
		t.Fatalf("album count = %d, want 3 (Album One, Album Two gap, Rare Bootleg)", len(albums))
	}

	// Album One folded into the provider release; its tracks reconciled.
	one := albumByTitle(t, albums, "Album One")
	if one.Provider != "test" || one.ProviderID != "2000" {
		t.Errorf("Album One provider = %s/%s, want test/2000", one.Provider, one.ProviderID)
	}
	if one.ID != albumOneID {
		t.Errorf("Album One id changed (%d != %d) — should relink in place, not recreate", one.ID, albumOneID)
	}
	oneTracks, err := q.ListTracksByAlbum(one.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(oneTracks) != 3 {
		t.Fatalf("Album One track count = %d, want 3 (A1 owned, A2 wanted, Bonus local)", len(oneTracks))
	}

	a1 := trackByTitle(t, oneTracks, "Track A1")
	if a1.Provider != "test" || a1.ProviderID != "3000" {
		t.Errorf("Track A1 provider = %s/%s, want test/3000", a1.Provider, a1.ProviderID)
	}
	if a1.Status != models.TrackStatusOwned || a1.FilePath == nil {
		t.Errorf("Track A1 = %s file=%v, want owned with file preserved", a1.Status, a1.FilePath)
	}

	a2 := trackByTitle(t, oneTracks, "Track A2")
	if a2.Provider != "test" || a2.ProviderID != "3001" {
		t.Errorf("Track A2 provider = %s/%s, want test/3001", a2.Provider, a2.ProviderID)
	}
	if a2.Status != models.TrackStatusWanted || a2.FilePath != nil {
		t.Errorf("Track A2 = %s file=%v, want wanted with no file", a2.Status, a2.FilePath)
	}

	bonus := trackByTitle(t, oneTracks, "Bonus Cut")
	if bonus.Provider != provider.LocalProvider || bonus.Status != models.TrackStatusOwned {
		t.Errorf("Bonus Cut = %s/%s, want local/owned (unmatched leftover)", bonus.Provider, bonus.Status)
	}

	// Album Two: user had none of it — created as a gap, wanted.
	two := albumByTitle(t, albums, "Album Two")
	if two.Provider != "test" || two.ProviderID != "2001" {
		t.Errorf("Album Two provider = %s/%s, want test/2001", two.Provider, two.ProviderID)
	}
	twoTracks, err := q.ListTracksByAlbum(two.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(twoTracks) != 1 || twoTracks[0].Status != models.TrackStatusWanted || twoTracks[0].ProviderID != "3002" {
		t.Errorf("Album Two tracks = %+v, want one wanted test/3002", twoTracks)
	}

	// Rare Bootleg untouched — the provider doesn't know it, so it stays owned+local.
	boot := albumByTitle(t, albums, "Rare Bootleg")
	if boot.Provider != provider.LocalProvider {
		t.Errorf("Rare Bootleg provider = %s, want local (leftover)", boot.Provider)
	}
	bootTracks, _ := q.ListTracksByAlbum(boot.ID)
	if len(bootTracks) != 1 || bootTracks[0].Provider != provider.LocalProvider || bootTracks[0].Status != models.TrackStatusOwned {
		t.Errorf("Bootleg tracks = %+v, want one local/owned", bootTracks)
	}
}

// TestReconcileYearGuardCreatesGap proves a same-titled but distinct-year local
// album is NOT merged: it stays local and the provider release is created fresh.
func TestReconcileYearGuardCreatesGap(t *testing.T) {
	e := newTestEnv(t)
	q := e.queries

	artist := models.Artist{Name: "Test Artist", Provider: provider.LocalProvider, ProviderID: "loc-artist", Status: models.ArtistStatusOwned}
	if err := q.CreateArtist(&artist); err != nil {
		t.Fatal(err)
	}
	// Same title as provider album 2000 ("Album One") but a 1999 pressing — the
	// provider release is 2023, so the year guard must keep them separate.
	oldID := seedLocalAlbum(t, q, artist.ID, "Album One", "loc-a1-1999", 1999)
	seedOwnedLocalTrack(t, q, oldID, "Track A1", "loc-old-t1", 1)

	if w := e.do("POST", fmt.Sprintf("/api/relink/artist/%d", artist.ID), `{"provider_id":"1000"}`); w.Code != http.StatusOK {
		t.Fatalf("relink status = %d", w.Code)
	}

	albums, err := q.ListAlbumsByArtist(artist.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Expect: the 1999 local album (untouched) + a fresh 2023 provider "Album One"
	// + "Album Two" gap = 3.
	var localOne, providerOne int
	for _, a := range albums {
		if a.Title != "Album One" {
			continue
		}
		if a.Provider == provider.LocalProvider {
			localOne++
		}
		if a.Provider == "test" && a.ProviderID == "2000" {
			providerOne++
		}
	}
	if localOne != 1 {
		t.Errorf("local 'Album One' count = %d, want 1 (kept, not merged)", localOne)
	}
	if providerOne != 1 {
		t.Errorf("provider 'Album One' count = %d, want 1 (created as gap)", providerOne)
	}
}

// TestRelinkNonLocalArtistDoesNotReconcile confirms the local gate: relinking an
// already-provider-anchored artist is a plain re-stamp — no discography fetch,
// no wanted rows, no forced watch.
func TestRelinkNonLocalArtistDoesNotReconcile(t *testing.T) {
	e := newTestEnv(t)
	q := e.queries

	artist := models.Artist{Name: "Test Artist", Provider: "test", ProviderID: "999", Status: models.ArtistStatusOwned}
	if err := q.CreateArtist(&artist); err != nil {
		t.Fatal(err)
	}

	if w := e.do("POST", fmt.Sprintf("/api/relink/artist/%d", artist.ID), `{"provider_id":"1000"}`); w.Code != http.StatusOK {
		t.Fatalf("relink status = %d", w.Code)
	}

	got, _ := q.GetArtist(artist.ID)
	if got.ProviderID != "1000" {
		t.Errorf("provider_id = %s, want 1000 (re-stamped)", got.ProviderID)
	}
	if got.Status != models.ArtistStatusOwned {
		t.Errorf("status = %s, want owned (unchanged — no reconcile)", got.Status)
	}
	albums, _ := q.ListAlbumsByArtist(artist.ID)
	if len(albums) != 0 {
		t.Errorf("album count = %d, want 0 (no discography pulled)", len(albums))
	}
}

// TestReconcileIsIdempotent runs the reconcile twice and asserts nothing
// duplicates the second time.
func TestReconcileIsIdempotent(t *testing.T) {
	e := newTestEnv(t)
	q := e.queries

	artist := models.Artist{Name: "Test Artist", Provider: provider.LocalProvider, ProviderID: "loc-artist", Status: models.ArtistStatusOwned}
	if err := q.CreateArtist(&artist); err != nil {
		t.Fatal(err)
	}
	albumOneID := seedLocalAlbum(t, q, artist.ID, "Album One", "loc-a1", 2023)
	seedOwnedLocalTrack(t, q, albumOneID, "Track A1", "loc-a1-t1", 1)

	if w := e.do("POST", fmt.Sprintf("/api/relink/artist/%d", artist.ID), `{"provider_id":"1000"}`); w.Code != http.StatusOK {
		t.Fatalf("first relink status = %d", w.Code)
	}
	albums1, _ := q.ListAlbumsByArtist(artist.ID)
	one := albumByTitle(t, albums1, "Album One")
	tracks1, _ := q.ListTracksByAlbum(one.ID)

	// Flip the artist back to local (albums stay on the provider) and reconcile
	// again — a real re-run over already-linked children.
	if err := q.RelinkArtist(artist.ID, provider.LocalProvider, "loc-artist"); err != nil {
		t.Fatal(err)
	}
	if w := e.do("POST", fmt.Sprintf("/api/relink/artist/%d", artist.ID), `{"provider_id":"1000"}`); w.Code != http.StatusOK {
		t.Fatalf("second relink status = %d", w.Code)
	}

	albums2, _ := q.ListAlbumsByArtist(artist.ID)
	if len(albums2) != len(albums1) {
		t.Errorf("album count changed on re-run: %d -> %d", len(albums1), len(albums2))
	}
	tracks2, _ := q.ListTracksByAlbum(one.ID)
	if len(tracks2) != len(tracks1) {
		t.Errorf("Album One track count changed on re-run: %d -> %d", len(tracks1), len(tracks2))
	}
}

// TestRelinkLocalAlbumReconcilesTracks covers the manual album-link escape
// hatch: an album that stayed local (didn't fuzzy-match) is linked by hand, and
// its tracks reconcile — owned file kept, missing track filled.
func TestRelinkLocalAlbumReconcilesTracks(t *testing.T) {
	e := newTestEnv(t)
	q := e.queries

	// Artist already promoted; one album stayed local for the user to link.
	artist := models.Artist{Name: "Test Artist", Provider: "test", ProviderID: "1000", Status: models.ArtistStatusWatched}
	if err := q.CreateArtist(&artist); err != nil {
		t.Fatal(err)
	}
	albumID := seedLocalAlbum(t, q, artist.ID, "Album One", "loc-a1", 2023)
	seedOwnedLocalTrack(t, q, albumID, "Track A1", "loc-a1-t1", 1)

	if w := e.do("POST", fmt.Sprintf("/api/relink/album/%d", albumID), `{"provider_id":"2000"}`); w.Code != http.StatusOK {
		t.Fatalf("relink album status = %d, body %s", w.Code, w.Body.String())
	}

	album, _ := q.GetAlbum(albumID)
	if album.Provider != "test" || album.ProviderID != "2000" {
		t.Errorf("album provider = %s/%s, want test/2000", album.Provider, album.ProviderID)
	}
	tracks, _ := q.ListTracksByAlbum(albumID)
	if len(tracks) != 2 {
		t.Fatalf("track count = %d, want 2 (A1 owned relinked, A2 wanted)", len(tracks))
	}
	a1 := trackByTitle(t, tracks, "Track A1")
	if a1.Provider != "test" || a1.ProviderID != "3000" || a1.Status != models.TrackStatusOwned || a1.FilePath == nil {
		t.Errorf("Track A1 = %s/%s %s file=%v, want test/3000 owned with file", a1.Provider, a1.ProviderID, a1.Status, a1.FilePath)
	}
	a2 := trackByTitle(t, tracks, "Track A2")
	if a2.Provider != "test" || a2.Status != models.TrackStatusWanted {
		t.Errorf("Track A2 = %s/%s, want test wanted", a2.Provider, a2.Status)
	}
}
