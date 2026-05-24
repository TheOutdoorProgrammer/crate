package api_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/TheOutdoorProgrammer/crate/internal/models"
)

func TestLidarrSystemStatus(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/v1/system/status", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decode[map[string]any](t, w)
	if resp["appName"] != "Crate" {
		t.Errorf("expected appName Crate, got %v", resp["appName"])
	}
	if resp["startTime"] == nil {
		t.Error("expected startTime to be set")
	}
}

func TestLidarrQualityProfile(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/v1/qualityprofile", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decode[[]map[string]any](t, w)
	if len(resp) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(resp))
	}
	if resp[0]["name"] != "Crate Quality" {
		t.Errorf("expected Crate Quality, got %v", resp[0]["name"])
	}
}

func TestLidarrMetadataProfileReturnsProvider(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/v1/metadataprofile", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decode[[]map[string]any](t, w)
	if len(resp) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(resp))
	}
	if resp[0]["name"] != "Test Provider" {
		t.Errorf("expected Test Provider, got %v", resp[0]["name"])
	}
}

func TestLidarrRootFolders(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/v1/rootfolder", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decode[[]map[string]any](t, w)
	if len(resp) != 1 {
		t.Fatalf("expected 1 folder, got %d", len(resp))
	}
	if resp[0]["path"] != "/music" {
		t.Errorf("expected /music, got %v", resp[0]["path"])
	}
}

func TestLidarrHealthReportsProviders(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/v1/health", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decode[[]map[string]any](t, w)
	// All providers healthy in test env, should be empty
	if len(resp) != 0 {
		t.Errorf("expected no health issues, got %d", len(resp))
	}
}

func TestLidarrSearchReturnsNestedFormat(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/v1/search?term=test", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decode[[]map[string]any](t, w)
	if len(resp) == 0 {
		t.Fatal("expected search results")
	}
	first := resp[0]
	if first["foreignId"] == nil {
		t.Error("expected foreignId in search result")
	}
	artist, ok := first["artist"].(map[string]any)
	if !ok {
		t.Fatal("expected nested artist object")
	}
	if artist["artistName"] == nil {
		t.Error("expected artistName in nested artist")
	}
	if artist["monitorNewItems"] == nil {
		t.Error("expected monitorNewItems in nested artist")
	}
}

func TestLidarrSearchEmptyTerm(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/v1/search?term=", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decode[[]map[string]any](t, w)
	if len(resp) != 0 {
		t.Errorf("expected empty results for empty term, got %d", len(resp))
	}
}

func TestLidarrArtistLookupReturnsFlatArray(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/v1/artist/lookup?term=test", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decode[[]map[string]any](t, w)
	if len(resp) == 0 {
		t.Fatal("expected lookup results")
	}
	if resp[0]["artistName"] == nil {
		t.Error("expected flat artistName field")
	}
	if resp[0]["foreignArtistId"] == nil {
		t.Error("expected foreignArtistId field")
	}
}

func TestLidarrAddArtistMonitorAll(t *testing.T) {
	env := newTestEnv(t)
	body := `{"foreignArtistId":"1000","artistName":"Test Artist","addOptions":{"monitor":"all","searchForMissingAlbums":false}}`
	w := env.do("POST", "/api/v1/artist", body)
	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decode[map[string]any](t, w)
	if resp["artistName"] != "Test Artist" {
		t.Errorf("expected Test Artist, got %v", resp["artistName"])
	}
	if resp["monitorNewItems"] != "all" {
		t.Errorf("expected monitorNewItems=all, got %v", resp["monitorNewItems"])
	}

	// Verify both albums were saved
	albums := env.do("GET", "/api/v1/album?artistId=1", "")
	albumList := decode[[]map[string]any](t, albums)
	if len(albumList) != 2 {
		t.Errorf("expected 2 albums for monitor=all, got %d", len(albumList))
	}
}

func TestLidarrAddArtistMonitorLatest(t *testing.T) {
	env := newTestEnv(t)
	body := `{"foreignArtistId":"1000","artistName":"Test Artist","addOptions":{"monitor":"latest","searchForMissingAlbums":false}}`
	w := env.do("POST", "/api/v1/artist", body)
	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decode[map[string]any](t, w)
	if resp["monitorNewItems"] != "latest" {
		t.Errorf("expected monitorNewItems=latest, got %v", resp["monitorNewItems"])
	}

	// Verify only 1 album (the latest by year) was saved
	id := int(resp["id"].(float64))
	albums := env.do("GET", "/api/v1/album?artistId="+itoa(id), "")
	albumList := decode[[]map[string]any](t, albums)
	if len(albumList) != 1 {
		t.Errorf("expected 1 album for monitor=latest, got %d", len(albumList))
	}
}

func TestLidarrAddArtistDuplicate(t *testing.T) {
	env := newTestEnv(t)
	body := `{"foreignArtistId":"1000","artistName":"Test Artist","addOptions":{"monitor":"all"}}`
	w1 := env.do("POST", "/api/v1/artist", body)
	if w1.Code != 201 {
		t.Fatalf("first add: expected 201, got %d", w1.Code)
	}
	w2 := env.do("POST", "/api/v1/artist", body)
	if w2.Code != 200 {
		t.Fatalf("second add: expected 200 (existing), got %d", w2.Code)
	}
}

func TestLidarrListArtists(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/v1/artist", `{"foreignArtistId":"1000","artistName":"Test Artist","addOptions":{"monitor":"all"}}`)

	w := env.do("GET", "/api/v1/artist", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decode[[]map[string]any](t, w)
	if len(resp) != 1 {
		t.Errorf("expected 1 artist, got %d", len(resp))
	}
	if resp[0]["statistics"] == nil {
		t.Error("expected statistics on artist")
	}
}

func TestLidarrGetArtist(t *testing.T) {
	env := newTestEnv(t)
	add := env.do("POST", "/api/v1/artist", `{"foreignArtistId":"1000","artistName":"Test Artist","addOptions":{"monitor":"all"}}`)
	addResp := decode[map[string]any](t, add)
	id := int(addResp["id"].(float64))

	w := env.do("GET", "/api/v1/artist/"+itoa(id), "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decode[map[string]any](t, w)
	if resp["artistName"] != "Test Artist" {
		t.Errorf("expected Test Artist, got %v", resp["artistName"])
	}
}

func TestLidarrDeleteArtist(t *testing.T) {
	env := newTestEnv(t)
	add := env.do("POST", "/api/v1/artist", `{"foreignArtistId":"1000","artistName":"Test Artist","addOptions":{"monitor":"all"}}`)
	addResp := decode[map[string]any](t, add)
	id := int(addResp["id"].(float64))

	w := env.do("DELETE", "/api/v1/artist/"+itoa(id), "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	get := env.do("GET", "/api/v1/artist/"+itoa(id), "")
	if get.Code != 404 {
		t.Errorf("expected 404 after delete, got %d", get.Code)
	}
}

func TestLidarrListAlbums(t *testing.T) {
	env := newTestEnv(t)
	add := env.do("POST", "/api/v1/artist", `{"foreignArtistId":"1000","artistName":"Test Artist","addOptions":{"monitor":"all"}}`)
	addResp := decode[map[string]any](t, add)
	id := int(addResp["id"].(float64))

	w := env.do("GET", "/api/v1/album?artistId="+itoa(id), "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decode[[]map[string]any](t, w)
	if len(resp) < 1 {
		t.Error("expected at least 1 album")
	}
	if resp[0]["statistics"] == nil {
		t.Error("expected statistics on album")
	}
}

func TestLidarrDeleteAlbum(t *testing.T) {
	env := newTestEnv(t)
	add := env.do("POST", "/api/v1/artist", `{"foreignArtistId":"1000","artistName":"Test Artist","addOptions":{"monitor":"all"}}`)
	addResp := decode[map[string]any](t, add)
	artistID := int(addResp["id"].(float64))

	albums := env.do("GET", "/api/v1/album?artistId="+itoa(artistID), "")
	albumList := decode[[]map[string]any](t, albums)
	if len(albumList) == 0 {
		t.Fatal("no albums to delete")
	}
	albumID := int(albumList[0]["id"].(float64))

	w := env.do("DELETE", "/api/v1/album/"+itoa(albumID), "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLidarrMonitorAlbum(t *testing.T) {
	env := newTestEnv(t)
	add := env.do("POST", "/api/v1/artist", `{"foreignArtistId":"1000","artistName":"Test Artist","addOptions":{"monitor":"all"}}`)
	addResp := decode[map[string]any](t, add)
	artistID := int(addResp["id"].(float64))

	albums := env.do("GET", "/api/v1/album?artistId="+itoa(artistID), "")
	albumList := decode[[]map[string]any](t, albums)
	if len(albumList) == 0 {
		t.Fatal("no albums")
	}
	albumID := int(albumList[0]["id"].(float64))

	// Unmonitor
	body, _ := json.Marshal(map[string]any{"albumIds": []int{albumID}, "monitored": false})
	w := env.do("PUT", "/api/v1/album/monitor", string(body))
	if w.Code != 202 {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	// Verify album is now ignored
	album, _ := env.queries.GetAlbum(int64(albumID))
	if album.Status != models.AlbumStatusIgnored {
		t.Errorf("expected album status ignored, got %s", album.Status)
	}

	// Verify tracks are ignored
	tracks, _ := env.queries.ListTracksByAlbum(int64(albumID))
	for _, tr := range tracks {
		if tr.Status != models.TrackStatusIgnored {
			t.Errorf("expected track %d status ignored, got %s", tr.ID, tr.Status)
		}
	}

	// Re-monitor
	body, _ = json.Marshal(map[string]any{"albumIds": []int{albumID}, "monitored": true})
	env.do("PUT", "/api/v1/album/monitor", string(body))

	album, _ = env.queries.GetAlbum(int64(albumID))
	if album.Status != models.AlbumStatusWatched {
		t.Errorf("expected album status watched, got %s", album.Status)
	}
	tracks, _ = env.queries.ListTracksByAlbum(int64(albumID))
	for _, tr := range tracks {
		if tr.Status != models.TrackStatusWanted {
			t.Errorf("expected track %d status wanted, got %s", tr.ID, tr.Status)
		}
	}
}

func TestLidarrListTracks(t *testing.T) {
	env := newTestEnv(t)
	add := env.do("POST", "/api/v1/artist", `{"foreignArtistId":"1000","artistName":"Test Artist","addOptions":{"monitor":"all"}}`)
	addResp := decode[map[string]any](t, add)
	artistID := int(addResp["id"].(float64))

	albums := env.do("GET", "/api/v1/album?artistId="+itoa(artistID), "")
	albumList := decode[[]map[string]any](t, albums)
	if len(albumList) == 0 {
		t.Fatal("no albums")
	}
	albumID := int(albumList[0]["id"].(float64))

	w := env.do("GET", "/api/v1/track?albumId="+itoa(albumID), "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decode[[]map[string]any](t, w)
	if len(resp) == 0 {
		t.Error("expected tracks")
	}
}

func TestLidarrQueue(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/v1/queue?page=1&pageSize=500", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decode[map[string]any](t, w)
	if resp["records"] == nil {
		t.Error("expected records field")
	}
	if resp["totalRecords"] == nil {
		t.Error("expected totalRecords field")
	}
}

func TestLidarrCommandArtistSearch(t *testing.T) {
	env := newTestEnv(t)
	add := env.do("POST", "/api/v1/artist", `{"foreignArtistId":"1000","artistName":"Test Artist","addOptions":{"monitor":"all"}}`)
	addResp := decode[map[string]any](t, add)
	id := int(addResp["id"].(float64))

	body, _ := json.Marshal(map[string]any{"name": "ArtistSearch", "artistId": id})
	w := env.do("POST", "/api/v1/command", string(body))
	if w.Code != 201 {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	resp := decode[map[string]any](t, w)
	if resp["name"] != "ArtistSearch" {
		t.Errorf("expected ArtistSearch, got %v", resp["name"])
	}
}

func TestLidarrCommandAlbumSearchArray(t *testing.T) {
	env := newTestEnv(t)
	add := env.do("POST", "/api/v1/artist", `{"foreignArtistId":"1000","artistName":"Test Artist","addOptions":{"monitor":"all"}}`)
	addResp := decode[map[string]any](t, add)
	artistID := int(addResp["id"].(float64))

	albums := env.do("GET", "/api/v1/album?artistId="+itoa(artistID), "")
	albumList := decode[[]map[string]any](t, albums)
	if len(albumList) == 0 {
		t.Fatal("no albums")
	}
	albumID := int(albumList[0]["id"].(float64))

	body, _ := json.Marshal(map[string]any{"name": "AlbumSearch", "albumIds": []int{albumID}})
	w := env.do("POST", "/api/v1/command", string(body))
	if w.Code != 201 {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

func TestLidarrDiskspace(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/v1/diskspace", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decode[[]map[string]any](t, w)
	if len(resp) != 1 {
		t.Fatalf("expected 1 disk, got %d", len(resp))
	}
	if resp[0]["path"] != "/music" {
		t.Errorf("expected /music path, got %v", resp[0]["path"])
	}
}

func TestLidarrCustomFilter(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/v1/customfilter", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestLidarrCalendar(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/v1/calendar", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestLidarrAuthAcceptsAnyKey(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/v1/system/status", "")
	if w.Code != 200 {
		t.Fatalf("expected 200 without api key, got %d", w.Code)
	}
}

func TestLidarrCatchAllReturnsEmptyArray(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/v1/nonexistent/endpoint", "")
	if w.Code != 200 {
		t.Fatalf("expected 200 from catch-all, got %d", w.Code)
	}
	resp := decode[[]any](t, w)
	if len(resp) != 0 {
		t.Errorf("expected empty array from catch-all, got %d items", len(resp))
	}
}

func TestLidarrIgnoreAlbumCrateAPI(t *testing.T) {
	env := newTestEnv(t)
	// Watch artist via crate API
	w := env.do("POST", "/api/watch/artist/1000", `{"watch_new_releases":true}`)
	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	artists := decode[map[string]any](t, w)
	artistID := int(artists["id"].(float64))

	albumsResp := env.do("GET", "/api/artists/"+itoa(artistID), "")
	artist := decode[map[string]any](t, albumsResp)
	albumsRaw := artist["albums"].([]any)
	if len(albumsRaw) == 0 {
		t.Fatal("no albums")
	}
	firstAlbum := albumsRaw[0].(map[string]any)
	albumID := int(firstAlbum["id"].(float64))

	// Ignore album
	ig := env.do("PUT", "/api/albums/"+itoa(albumID)+"/ignore", "")
	if ig.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", ig.Code, ig.Body.String())
	}

	album, _ := env.queries.GetAlbum(int64(albumID))
	if album.Status != models.AlbumStatusIgnored {
		t.Errorf("expected ignored, got %s", album.Status)
	}

	// Unignore
	un := env.do("DELETE", "/api/albums/"+itoa(albumID)+"/ignore", "")
	if un.Code != 200 {
		t.Fatalf("expected 200, got %d", un.Code)
	}
	album, _ = env.queries.GetAlbum(int64(albumID))
	if album.Status != models.AlbumStatusWatched {
		t.Errorf("expected watched, got %s", album.Status)
	}
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}
