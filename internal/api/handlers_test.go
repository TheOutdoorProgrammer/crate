package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/TheOutdoorProgrammer/crate/internal/activity"
	"github.com/TheOutdoorProgrammer/crate/internal/api"
	"github.com/TheOutdoorProgrammer/crate/internal/cache"
	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/models"
	"github.com/TheOutdoorProgrammer/crate/internal/provider"
	"github.com/TheOutdoorProgrammer/crate/internal/services/downloader"
	"github.com/TheOutdoorProgrammer/crate/internal/services/slskd"
	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

type testEnv struct {
	server      *api.Server
	queries     *db.Queries
	activityLog *activity.Log
	fakeSlskd   *httptest.Server
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	c, err := cache.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })

	actLog, err := activity.NewLog(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { actLog.Close() })

	grpcAddr := startFakeProvider(t)

	fakeSlskd := newFakeSlskd()
	t.Cleanup(fakeSlskd.Close)

	queries := db.NewQueries(database)
	providerMgr := provider.NewManager(c, queries)

	if err := providerMgr.RegisterProvider(context.Background(), "test", grpcAddr); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	queries.SetSetting("provider_primary", "test")

	slskdClient := slskd.NewClient(fakeSlskd.URL, "test-key")
	org := &noopOrganizer{}
	dl := downloader.NewService(queries, slskdClient, org, actLog)

	srv := api.NewServer(queries, providerMgr, c, dl, actLog, nil, "/music", "test")

	return &testEnv{
		server:      srv,
		queries:     queries,
		activityLog: actLog,
		fakeSlskd:   fakeSlskd,
	}
}

type noopOrganizer struct{}

func (o *noopOrganizer) Organize(track *models.Track) error { return nil }

func (e *testEnv) do(method, path string, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.server.ServeHTTP(w, req)
	e.server.WaitBackground()
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(w.Body).Decode(&v); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, w.Body.String())
	}
	return v
}

// --- Fake gRPC Provider ---

type fakeProvider struct {
	pb.UnimplementedMusicProviderServer
}

func (f *fakeProvider) Info(ctx context.Context, req *pb.InfoRequest) (*pb.InfoResponse, error) {
	return &pb.InfoResponse{
		Name:        "test",
		DisplayName: "Test Provider",
		Version:     "1.0.0",
	}, nil
}

func (f *fakeProvider) SearchArtists(ctx context.Context, req *pb.SearchRequest) (*pb.ArtistSearchResponse, error) {
	all := []*pb.ArtistResult{
		{Id: "1000", Name: "Test Artist", ImageUrl: "http://img/artist.jpg", AlbumCount: 2, Rank: 5000},
		{Id: "1001", Name: "Test Artist 2", ImageUrl: "http://img/artist2.jpg", AlbumCount: 1, Rank: 100},
		{Id: "1002", Name: "Test Artist 3", ImageUrl: "", AlbumCount: 0, Rank: 50},
	}
	offset := int(req.Offset)
	if offset >= len(all) {
		return &pb.ArtistSearchResponse{Artists: nil, Total: int32(len(all))}, nil
	}
	end := offset + int(req.Limit)
	if end > len(all) {
		end = len(all)
	}
	return &pb.ArtistSearchResponse{Artists: all[offset:end], Total: int32(len(all))}, nil
}

func (f *fakeProvider) GetArtist(ctx context.Context, req *pb.EntityRequest) (*pb.ArtistDetail, error) {
	if req.Id == "1000" {
		return &pb.ArtistDetail{
			Id:       "1000",
			Name:     "Test Artist",
			ImageUrl: "http://img/artist.jpg",
		}, nil
	}
	return nil, fmt.Errorf("artist not found")
}

func (f *fakeProvider) GetArtistAlbums(ctx context.Context, req *pb.EntityRequest) (*pb.AlbumList, error) {
	if req.Id == "1000" {
		return &pb.AlbumList{
			Albums: []*pb.AlbumSummary{
				{Id: "2000", Title: "Album One", CoverUrl: "http://img/a1.jpg", Year: 2023, RecordType: "album", Rank: 2, Metadata: map[string]string{"release_date": "2023-01-15"}},
				{Id: "2001", Title: "Album Two", CoverUrl: "http://img/a2.jpg", Year: 2024, RecordType: "album", Rank: 1, Metadata: map[string]string{"release_date": "2024-06-01"}},
			},
		}, nil
	}
	return &pb.AlbumList{}, nil
}

func (f *fakeProvider) SearchArtistTracks(ctx context.Context, req *pb.ArtistTrackSearchRequest) (*pb.ArtistTrackSearchResponse, error) {
	if req.ArtistId == "1000" {
		all := []*pb.TrackSearchResult{
			{Id: "3000", Title: "Track A1", DurationMs: 200000, AlbumId: "2000", AlbumTitle: "Album One", AlbumYear: 2023},
			{Id: "3001", Title: "Track A2", DurationMs: 180000, AlbumId: "2000", AlbumTitle: "Album One", AlbumYear: 2023},
			{Id: "3002", Title: "Track B1", DurationMs: 240000, AlbumId: "2001", AlbumTitle: "Album Two", AlbumYear: 2024},
		}
		q := strings.ToLower(req.Query)
		var matched []*pb.TrackSearchResult
		for _, t := range all {
			if strings.Contains(strings.ToLower(t.Title), q) {
				matched = append(matched, t)
			}
		}
		return &pb.ArtistTrackSearchResponse{Tracks: matched}, nil
	}
	return &pb.ArtistTrackSearchResponse{}, nil
}

func (f *fakeProvider) GetAlbum(ctx context.Context, req *pb.EntityRequest) (*pb.AlbumDetail, error) {
	switch req.Id {
	case "2000":
		return &pb.AlbumDetail{
			Id: "2000", Title: "Album One", CoverUrl: "http://img/a1.jpg", Year: 2023, ArtistName: "Test Artist",
			Tracks: []*pb.TrackInfo{
				{Id: "3000", Title: "Track A1", TrackNumber: 1, DiscNumber: 1, DurationMs: 200000, Rank: 1},
				{Id: "3001", Title: "Track A2", TrackNumber: 2, DiscNumber: 1, DurationMs: 180000, Rank: 2},
			},
		}, nil
	case "2001":
		return &pb.AlbumDetail{
			Id: "2001", Title: "Album Two", CoverUrl: "http://img/a2.jpg", Year: 2024, ArtistName: "Test Artist",
			Tracks: []*pb.TrackInfo{
				{Id: "3002", Title: "Track B1", TrackNumber: 1, DiscNumber: 1, DurationMs: 240000, Rank: 1},
			},
		}, nil
	}
	return nil, fmt.Errorf("album not found")
}

func startFakeProvider(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()
	pb.RegisterMusicProviderServer(s, &fakeProvider{})
	go s.Serve(lis)
	t.Cleanup(s.Stop)
	return lis.Addr().String()
}

// --- Fake slskd API ---

func newFakeSlskd() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v0/transfers/downloads/", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(slskd.EnqueueResponse{
			Enqueued: []slskd.Transfer{{ID: "transfer-1", Username: "testuser", Filename: "test.flac"}},
		})
	})
	mux.HandleFunc("/api/v0/transfers/downloads", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]slskd.UserDownloads{})
	})
	searchResp := slskd.SearchResponse{
		ID: "search-1", IsComplete: true,
		Responses: []slskd.SearchResult{
			{
				Username:          "user1",
				HasFreeUploadSlot: true,
				Files: []slskd.SearchFile{
					{Filename: "Music/Test Artist - Track A1.flac", Size: 30000000, BitRate: 0},
					{Filename: "Music/Test Artist - Track A1.mp3", Size: 8000000, BitRate: 320},
				},
			},
			{
				Username: "user2",
				Files: []slskd.SearchFile{
					{Filename: "Music/Test Artist - Track A1.mp3", Size: 7000000, BitRate: 256},
				},
			},
		},
	}
	mux.HandleFunc("/api/v0/searches/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		json.NewEncoder(w).Encode(searchResp)
	})
	mux.HandleFunc("/api/v0/searches", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(searchResp)
	})
	return httptest.NewServer(mux)
}

// --- Tests ---

func TestSearchReturnsArtists(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/search?q=test&limit=25", "")

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	result := decode[map[string]any](t, w)
	artists := result["artists"].([]any)
	if len(artists) != 3 {
		t.Fatalf("expected 3 artists, got %d", len(artists))
	}
	first := artists[0].(map[string]any)
	if first["name"] != "Test Artist" {
		t.Errorf("expected first artist 'Test Artist', got %v", first["name"])
	}
	if int(result["total"].(float64)) != 3 {
		t.Errorf("expected total 3, got %v", result["total"])
	}
}

func TestSearchPagination(t *testing.T) {
	env := newTestEnv(t)

	w := env.do("GET", "/api/search?q=test&limit=2&offset=0", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	result := decode[map[string]any](t, w)
	artists := result["artists"].([]any)
	if len(artists) != 2 {
		t.Errorf("expected 2 artists in first page, got %d", len(artists))
	}
	if int(result["total"].(float64)) != 3 {
		t.Errorf("expected total 3, got %v", result["total"])
	}

	w = env.do("GET", "/api/search?q=test&limit=2&offset=2", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	result = decode[map[string]any](t, w)
	artists = result["artists"].([]any)
	if len(artists) != 1 {
		t.Errorf("expected 1 artist in second page, got %d", len(artists))
	}

	w = env.do("GET", "/api/search?q=test&limit=2&offset=10", "")
	result = decode[map[string]any](t, w)
	artists = result["artists"].([]any)
	if len(artists) != 0 {
		t.Errorf("expected 0 artists past end, got %d", len(artists))
	}
}

func TestSearchWithProviderOverride(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/search?q=test&provider=test", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	result := decode[map[string]any](t, w)
	artists := result["artists"].([]any)
	if len(artists) != 3 {
		t.Errorf("expected 3 artists, got %d", len(artists))
	}
}

func TestSearchWithInvalidProviderFails(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/search?q=test&provider=nonexistent", "")
	if w.Code != 500 {
		t.Fatalf("expected 500 for unknown provider, got %d", w.Code)
	}
}

func TestSearchRequiresQuery(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/search", "")
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestBrowseArtist(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/browse/artist/1000", "")

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	result := decode[map[string]any](t, w)
	if result["name"] != "Test Artist" {
		t.Errorf("expected 'Test Artist', got %v", result["name"])
	}
	albums := result["albums"].([]any)
	if len(albums) != 2 {
		t.Errorf("expected 2 albums, got %d", len(albums))
	}
}

func TestBrowseAlbum(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/browse/album/2000", "")

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	result := decode[map[string]any](t, w)
	if result["title"] != "Album One" {
		t.Errorf("expected 'Album One', got %v", result["title"])
	}
	tracks := result["tracks"].([]any)
	if len(tracks) != 2 {
		t.Errorf("expected 2 tracks, got %d", len(tracks))
	}
}

func TestWatchArtistCreatesFullDiscography(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("POST", "/api/watch/artist/1000", `{}`)


	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	artists, _ := env.queries.ListArtists()
	if len(artists) != 1 {
		t.Fatalf("expected 1 artist, got %d", len(artists))
	}
	if artists[0].Name != "Test Artist" {
		t.Errorf("expected 'Test Artist', got %q", artists[0].Name)
	}
	if artists[0].Provider != "test" {
		t.Errorf("expected provider 'test', got %q", artists[0].Provider)
	}
	if artists[0].ProviderID != "1000" {
		t.Errorf("expected provider_id '1000', got %q", artists[0].ProviderID)
	}

	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	if len(albums) != 2 {
		t.Fatalf("expected 2 albums, got %d", len(albums))
	}

	tracks1, _ := env.queries.ListTracksByAlbum(albums[0].ID)
	tracks2, _ := env.queries.ListTracksByAlbum(albums[1].ID)
	total := len(tracks1) + len(tracks2)
	if total != 3 {
		t.Errorf("expected 3 total tracks, got %d", total)
	}

	for _, t2 := range append(tracks1, tracks2...) {
		if t2.Status != models.TrackStatusWanted {
			t.Errorf("track %q has status %q, expected 'wanted'", t2.Title, t2.Status)
		}
	}
}

func TestWatchArtistWithNewReleases(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("POST", "/api/watch/artist/1000", `{"watch_new_releases": true}`)

	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	artists, _ := env.queries.ListArtists()
	if !artists[0].WatchNewReleases {
		t.Error("expected watch_new_releases to be true")
	}
	if artists[0].WatchNewReleasesSince == nil {
		t.Error("expected watch_new_releases_since to be set")
	}
}

func TestWatchArtistIdempotentForFullWatch(t *testing.T) {
	env := newTestEnv(t)

	env.do("POST", "/api/watch/artist/1000", `{}`)

	w := env.do("POST", "/api/watch/artist/1000", `{}`)
	if w.Code != 200 {
		t.Fatalf("expected 200 on re-watch, got %d", w.Code)
	}

	artists, _ := env.queries.ListArtists()
	if len(artists) != 1 {
		t.Errorf("expected 1 artist after re-watch, got %d", len(artists))
	}
}

func TestReWatchArtistSyncsMissingTracks(t *testing.T) {
	env := newTestEnv(t)

	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)

	// Find album with 2 tracks and delete one
	var targetAlbum *models.Album
	for i := range albums {
		tracks, _ := env.queries.ListTracksByAlbum(albums[i].ID)
		if len(tracks) == 2 {
			targetAlbum = &albums[i]
			// Delete the second track to simulate a missing track
			env.queries.DeleteTrack(tracks[1].ID)
			break
		}
	}
	if targetAlbum == nil {
		t.Fatal("expected an album with 2 tracks")
	}

	// Verify track is gone
	tracksAfterDelete, _ := env.queries.ListTracksByAlbum(targetAlbum.ID)
	if len(tracksAfterDelete) != 1 {
		t.Fatalf("expected 1 track after delete, got %d", len(tracksAfterDelete))
	}

	// Re-watch the artist
	w := env.do("POST", "/api/watch/artist/1000", `{}`)
	if w.Code != 200 {
		t.Fatalf("expected 200 on re-watch, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the missing track was re-added
	tracksAfterSync, _ := env.queries.ListTracksByAlbum(targetAlbum.ID)
	if len(tracksAfterSync) != 2 {
		t.Fatalf("expected 2 tracks after re-watch sync, got %d", len(tracksAfterSync))
	}

	// Verify no duplicate artists or albums
	allArtists, _ := env.queries.ListArtists()
	if len(allArtists) != 1 {
		t.Errorf("expected 1 artist, got %d", len(allArtists))
	}
	allAlbums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	if len(allAlbums) != 2 {
		t.Errorf("expected 2 albums, got %d", len(allAlbums))
	}
}

func TestReWatchArtistSkipsIgnoredAlbums(t *testing.T) {
	env := newTestEnv(t)

	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)

	// Ignore the first album and delete its tracks
	env.do("PUT", fmt.Sprintf("/api/albums/%d/ignore", albums[0].ID), ``)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)
	for _, tr := range tracks {
		env.queries.DeleteTrack(tr.ID)
	}

	// Re-watch the artist
	w := env.do("POST", "/api/watch/artist/1000", `{}`)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Ignored album should NOT get tracks re-added
	tracksAfterSync, _ := env.queries.ListTracksByAlbum(albums[0].ID)
	if len(tracksAfterSync) != 0 {
		t.Errorf("expected 0 tracks for ignored album, got %d", len(tracksAfterSync))
	}
}

func TestWatchAllAlbumsForPartialArtist(t *testing.T) {
	env := newTestEnv(t)

	w := env.do("POST", "/api/watch/track/3000", `{
		"artist_provider_id": "1000",
		"artist_name": "Test Artist",
		"artist_image_url": "http://img/artist.jpg",
		"album_provider_id": "2000",
		"album_title": "Album One",
		"album_cover_url": "http://img/a1.jpg",
		"album_year": 2023,
		"title": "Track A1",
		"track_number": 1,
		"disc_number": 1,
		"duration_ms": 200000
	}`)
	if w.Code != 201 {
		t.Fatalf("watch track: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	artists, _ := env.queries.ListArtists()
	if artists[0].Status != models.ArtistStatusPartial {
		t.Fatalf("expected partial artist, got %q", artists[0].Status)
	}

	w = env.do("POST", "/api/watch/artist/1000", `{}`)

	if w.Code != 200 && w.Code != 201 {
		t.Fatalf("watch all: expected 200/201, got %d: %s", w.Code, w.Body.String())
	}

	artists, _ = env.queries.ListArtists()
	if len(artists) != 1 {
		t.Fatalf("expected 1 artist, got %d", len(artists))
	}

	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	if len(albums) != 2 {
		t.Errorf("expected 2 albums after watch-all, got %d", len(albums))
	}
}

func TestWatchSingleAlbum(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("POST", "/api/watch/album/2000", `{
		"artist_provider_id": "1000",
		"artist_name": "Test Artist",
		"artist_image_url": "http://img/artist.jpg"
	}`)

	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	artists, _ := env.queries.ListArtists()
	if len(artists) != 1 {
		t.Fatalf("expected 1 artist, got %d", len(artists))
	}
	if artists[0].Status != models.ArtistStatusPartial {
		t.Errorf("expected partial status, got %q", artists[0].Status)
	}

	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	if len(albums) != 1 {
		t.Fatalf("expected 1 album, got %d", len(albums))
	}
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)
	if len(tracks) != 2 {
		t.Errorf("expected 2 tracks, got %d", len(tracks))
	}
}

func TestWatchSingleTrack(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("POST", "/api/watch/track/3000", `{
		"artist_provider_id": "1000",
		"artist_name": "Test Artist",
		"artist_image_url": "http://img/artist.jpg",
		"album_provider_id": "2000",
		"album_title": "Album One",
		"album_cover_url": "http://img/a1.jpg",
		"album_year": 2023,
		"title": "Track A1",
		"track_number": 1,
		"disc_number": 1,
		"duration_ms": 200000
	}`)

	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)
	if len(tracks) != 1 {
		t.Errorf("expected 1 track, got %d", len(tracks))
	}
	if tracks[0].Title != "Track A1" {
		t.Errorf("expected 'Track A1', got %q", tracks[0].Title)
	}
}

func TestListArtistsEmpty(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/artists", "")

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var artists []any
	json.NewDecoder(w.Body).Decode(&artists)
	if len(artists) != 0 {
		t.Errorf("expected empty list, got %d", len(artists))
	}
}

func TestGetArtistWithAlbumsAndTracks(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)


	artists, _ := env.queries.ListArtists()
	w := env.do("GET", fmt.Sprintf("/api/artists/%d", artists[0].ID), "")

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	artist := decode[models.Artist](t, w)
	if len(artist.Albums) != 2 {
		t.Fatalf("expected 2 albums, got %d", len(artist.Albums))
	}
	for _, album := range artist.Albums {
		if len(album.Tracks) == 0 {
			t.Errorf("album %q has no tracks", album.Title)
		}
	}
}

func TestGetArtistNotFound(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/artists/999", "")
	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetAlbumWithTracks(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)


	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)

	w := env.do("GET", fmt.Sprintf("/api/albums/%d", albums[0].ID), "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	album := decode[models.Album](t, w)
	if len(album.Tracks) == 0 {
		t.Error("expected album to include tracks")
	}
	if album.ArtistName != "Test Artist" {
		t.Errorf("expected artist_name 'Test Artist', got %q", album.ArtistName)
	}
}

func TestUnwatchArtistCascades(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	w := env.do("DELETE", fmt.Sprintf("/api/artists/%d", artists[0].ID), "")
	if w.Code != 204 {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	artists, _ = env.queries.ListArtists()
	if len(artists) != 0 {
		t.Errorf("expected 0 artists after unwatch, got %d", len(artists))
	}

	wanted, _ := env.queries.ListWantedTracks()
	if len(wanted) != 0 {
		t.Errorf("expected 0 wanted tracks after cascade delete, got %d", len(wanted))
	}
}

func TestUnwatchAlbum(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)

	w := env.do("DELETE", fmt.Sprintf("/api/albums/%d", albums[0].ID), "")
	if w.Code != 204 {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	remaining, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	if len(remaining) != 1 {
		t.Errorf("expected 1 album remaining, got %d", len(remaining))
	}
}

func TestUnwatchTrack(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)

	w := env.do("DELETE", fmt.Sprintf("/api/tracks/%d", tracks[0].ID), "")
	if w.Code != 204 {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	remaining, _ := env.queries.ListTracksByAlbum(albums[0].ID)
	if len(remaining) != len(tracks)-1 {
		t.Errorf("expected %d tracks remaining, got %d", len(tracks)-1, len(remaining))
	}
}

func TestQueueTrackForDownload(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)

	w := env.do("POST", fmt.Sprintf("/api/tracks/%d/queue", tracks[0].ID), "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	downloads, _ := env.queries.ListDownloads("pending")
	if len(downloads) != 1 {
		t.Fatalf("expected 1 pending download, got %d", len(downloads))
	}
	if downloads[0].TrackID != tracks[0].ID {
		t.Errorf("expected track_id %d, got %d", tracks[0].ID, downloads[0].TrackID)
	}
}

func TestQueueTrackIsIdempotent(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)

	env.do("POST", fmt.Sprintf("/api/tracks/%d/queue", tracks[0].ID), "")
	env.do("POST", fmt.Sprintf("/api/tracks/%d/queue", tracks[0].ID), "")

	downloads, _ := env.queries.ListDownloads("pending")
	if len(downloads) != 1 {
		t.Errorf("expected 1 download after double-queue, got %d", len(downloads))
	}
}

func TestQueueAllWantedTracks(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	w := env.do("POST", "/api/downloads/queue", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	result := decode[map[string]int](t, w)
	if result["queued"] != 3 {
		t.Errorf("expected 3 queued, got %d", result["queued"])
	}

	downloads, _ := env.queries.ListDownloads("pending")
	if len(downloads) != 3 {
		t.Errorf("expected 3 pending downloads, got %d", len(downloads))
	}
}

func TestListDownloadsWithTrackInfo(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)
	env.do("POST", "/api/downloads/queue", "")

	w := env.do("GET", "/api/downloads", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	downloads := decode[[]models.DownloadQueueItem](t, w)
	if len(downloads) != 3 {
		t.Fatalf("expected 3 downloads, got %d", len(downloads))
	}
	for _, d := range downloads {
		if d.Track == nil {
			t.Error("expected track info on download item")
		}
	}
}

func TestListDownloadsFilterByStatus(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)
	env.do("POST", "/api/downloads/queue", "")

	w := env.do("GET", "/api/downloads?status=complete", "")
	downloads := decode[[]models.DownloadQueueItem](t, w)
	if len(downloads) != 0 {
		t.Errorf("expected 0 complete downloads, got %d", len(downloads))
	}

	w = env.do("GET", "/api/downloads?status=pending", "")
	downloads = decode[[]models.DownloadQueueItem](t, w)
	if len(downloads) != 3 {
		t.Errorf("expected 3 pending downloads, got %d", len(downloads))
	}
}

func TestRetryDownload(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)
	env.do("POST", "/api/downloads/queue", "")

	downloads, _ := env.queries.ListDownloads("pending")
	errStr := "test error"
	env.queries.UpdateDownloadStatus(downloads[0].ID, models.DownloadStatusFailed, nil, &errStr)

	w := env.do("POST", fmt.Sprintf("/api/downloads/%d/retry", downloads[0].ID), "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated, _ := env.queries.ListDownloads("pending")
	found := false
	for _, d := range updated {
		if d.ID == downloads[0].ID {
			found = true
		}
	}
	if !found {
		t.Error("expected retried download to be pending")
	}
}

func TestDeleteDownload(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)
	env.do("POST", "/api/downloads/queue", "")

	downloads, _ := env.queries.ListDownloads("pending")
	w := env.do("DELETE", fmt.Sprintf("/api/downloads/%d", downloads[0].ID), "")
	if w.Code != 204 {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	remaining, _ := env.queries.ListDownloads("")
	if len(remaining) != len(downloads)-1 {
		t.Errorf("expected %d downloads remaining, got %d", len(downloads)-1, len(remaining))
	}
}

func TestToggleNewReleases(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()

	w := env.do("PUT", fmt.Sprintf("/api/artists/%d/new-releases", artists[0].ID), `{"enabled": true}`)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	artist, _ := env.queries.GetArtist(artists[0].ID)
	if !artist.WatchNewReleases {
		t.Error("expected watch_new_releases to be true")
	}
	if artist.WatchNewReleasesSince == nil {
		t.Error("expected watch_new_releases_since to be set")
	}

	env.do("PUT", fmt.Sprintf("/api/artists/%d/new-releases", artists[0].ID), `{"enabled": false}`)
	artist, _ = env.queries.GetArtist(artists[0].ID)
	if artist.WatchNewReleases {
		t.Error("expected watch_new_releases to be false after disable")
	}
}

func TestSettingsCRUD(t *testing.T) {
	env := newTestEnv(t)

	w := env.do("GET", "/api/settings", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	w = env.do("PUT", "/api/settings", `{"theme": "dark", "language": "en"}`)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	w = env.do("GET", "/api/settings", "")
	settings := decode[map[string]string](t, w)
	if settings["theme"] != "dark" {
		t.Errorf("expected theme 'dark', got %q", settings["theme"])
	}
	if settings["language"] != "en" {
		t.Errorf("expected language 'en', got %q", settings["language"])
	}

	env.do("PUT", "/api/settings", `{"theme": "light"}`)
	w = env.do("GET", "/api/settings", "")
	settings = decode[map[string]string](t, w)
	if settings["theme"] != "light" {
		t.Errorf("expected theme 'light' after update, got %q", settings["theme"])
	}
	if settings["language"] != "en" {
		t.Errorf("expected language 'en' to persist, got %q", settings["language"])
	}
}

func TestSettingsHiddenKeysNotReturned(t *testing.T) {
	env := newTestEnv(t)

	for _, key := range []string{"slskd_url", "slskd_api_key", "library_path", "scan_interval", "download_format_preference", "min_bitrate"} {
		env.queries.SetSetting(key, "secret_value_"+key)
	}

	w := env.do("GET", "/api/settings", "")
	settings := decode[map[string]string](t, w)
	for _, key := range []string{"slskd_url", "slskd_api_key", "library_path", "scan_interval", "download_format_preference", "min_bitrate"} {
		if _, ok := settings[key]; ok {
			t.Errorf("hidden setting %q should not be returned by GET /api/settings", key)
		}
	}
}

func TestSettingsHiddenKeysNotWritable(t *testing.T) {
	env := newTestEnv(t)

	env.do("PUT", "/api/settings", `{"slskd_api_key": "injected", "max_concurrent_slskd": "5"}`)

	v, _ := env.queries.GetSetting("slskd_api_key")
	if v == "injected" {
		t.Error("hidden setting slskd_api_key should not be writable via API")
	}

	v, _ = env.queries.GetSetting("max_concurrent_slskd")
	if v != "5" {
		t.Errorf("expected max_concurrent_slskd '5', got %q", v)
	}
}

func TestSettingsSensitiveRedacted(t *testing.T) {
	env := newTestEnv(t)

	env.do("PUT", "/api/settings", `{"navidrome_password": "my_secret"}`)

	w := env.do("GET", "/api/settings", "")
	settings := decode[map[string]string](t, w)
	if settings["navidrome_password"] != "••••••••" {
		t.Errorf("expected redacted password, got %q", settings["navidrome_password"])
	}
}

func TestSettingsSensitiveNotOverwrittenWithPlaceholder(t *testing.T) {
	env := newTestEnv(t)

	env.do("PUT", "/api/settings", `{"navidrome_password": "real_password"}`)
	env.do("PUT", "/api/settings", `{"navidrome_password": "••••••••"}`)

	v, _ := env.queries.GetSetting("navidrome_password")
	if v != "real_password" {
		t.Errorf("expected password to remain 'real_password', got %q", v)
	}
}

func TestStatusEndpoint(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)
	env.do("POST", "/api/downloads/queue", "")

	w := env.do("GET", "/api/status", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	status := decode[map[string]any](t, w)
	if status["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", status["status"])
	}
	if int(status["artists_count"].(float64)) != 1 {
		t.Errorf("expected 1 artist, got %v", status["artists_count"])
	}
	if int(status["total_tracks"].(float64)) != 3 {
		t.Errorf("expected 3 total tracks, got %v", status["total_tracks"])
	}
	if int(status["pending_downloads"].(float64)) != 3 {
		t.Errorf("expected 3 pending downloads, got %v", status["pending_downloads"])
	}
}

func TestDownloadProgress(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/downloads/progress", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestListProviders(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/providers", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	providers := decode[[]provider.ProviderInfo](t, w)
	if len(providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(providers))
	}
	if providers[0].Name != "test" {
		t.Errorf("expected provider name 'test', got %q", providers[0].Name)
	}
}

func TestClearCache(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("DELETE", "/api/cache", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRelinkArtist(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	w := env.do("POST", fmt.Sprintf("/api/relink/artist/%d", artists[0].ID), `{"provider_id": "9999"}`)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	artist, _ := env.queries.GetArtist(artists[0].ID)
	if artist.ProviderID != "9999" {
		t.Errorf("expected provider_id '9999', got %q", artist.ProviderID)
	}
}

func TestQueueArtistTracks(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	w := env.do("POST", fmt.Sprintf("/api/artists/%d/queue", artists[0].ID), "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	result := decode[map[string]int](t, w)
	if result["queued"] != 3 {
		t.Errorf("expected 3 queued, got %d", result["queued"])
	}

	downloads, _ := env.queries.ListDownloads("pending")
	if len(downloads) != 3 {
		t.Errorf("expected 3 pending downloads, got %d", len(downloads))
	}
}

func TestQueueAlbumTracks(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)

	w := env.do("POST", fmt.Sprintf("/api/albums/%d/queue", albums[0].ID), "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	result := decode[map[string]int](t, w)
	trackCount := len(albums[0].Tracks)
	if trackCount == 0 {
		tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)
		trackCount = len(tracks)
	}
	if result["queued"] != trackCount {
		t.Errorf("expected %d queued, got %d", trackCount, result["queued"])
	}
}

func TestActivityListPagination(t *testing.T) {
	env := newTestEnv(t)

	for i := 0; i < 5; i++ {
		env.activityLog.Record("test_action", "test", int64(i), fmt.Sprintf("event %d", i))
	}

	w := env.do("GET", "/api/activity?limit=2&offset=0", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	result := decode[map[string]any](t, w)
	items := result["items"].([]any)
	if len(items) != 2 {
		t.Errorf("expected 2 activity items, got %d", len(items))
	}
	if int(result["total"].(float64)) != 5 {
		t.Errorf("expected total 5, got %v", result["total"])
	}

	w = env.do("GET", "/api/activity?limit=2&offset=4", "")
	result = decode[map[string]any](t, w)
	items = result["items"].([]any)
	if len(items) != 1 {
		t.Errorf("expected 1 activity item on last page, got %d", len(items))
	}

	w = env.do("GET", "/api/activity?limit=2&offset=10", "")
	result = decode[map[string]any](t, w)
	items = result["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected 0 activity items past end, got %d", len(items))
	}
}

func TestActivityListEmpty(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/activity", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	result := decode[map[string]any](t, w)
	items := result["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestBrowseArtistWithProviderOverride(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/browse/artist/1000?provider=test", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	result := decode[map[string]any](t, w)
	if result["name"] != "Test Artist" {
		t.Errorf("expected 'Test Artist', got %v", result["name"])
	}
}

func TestBrowseAlbumWithProviderOverride(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/browse/album/2000?provider=test", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	result := decode[map[string]any](t, w)
	if result["title"] != "Album One" {
		t.Errorf("expected 'Album One', got %v", result["title"])
	}
}

func TestManualSearchTrack(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)

	w := env.do("POST", fmt.Sprintf("/api/tracks/%d/search", tracks[0].ID), "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	results := decode[[]map[string]any](t, w)
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	// Results should be sorted by score descending
	if len(results) >= 2 {
		score0 := results[0]["score"].(float64)
		score1 := results[1]["score"].(float64)
		if score0 < score1 {
			t.Errorf("expected results sorted by score desc, got %v then %v", score0, score1)
		}
	}
	// First result should have username
	if results[0]["username"] == "" {
		t.Error("expected username on result")
	}
}

func TestManualDownloadTrack(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)

	w := env.do("POST", fmt.Sprintf("/api/tracks/%d/download", tracks[0].ID), `{
		"username": "testuser",
		"filename": "Music/Test Artist - Track A1.flac",
		"size": 30000000,
		"bit_rate": 0
	}`)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	result := decode[map[string]string](t, w)
	if result["status"] != "downloading" {
		t.Errorf("expected status 'downloading', got %q", result["status"])
	}

	track, _ := env.queries.GetTrackWithMeta(tracks[0].ID)
	if track.DownloadFormat == nil || *track.DownloadFormat != "flac" {
		t.Errorf("expected download_format 'flac', got %v", track.DownloadFormat)
	}
}

func TestManualDownloadTrackRecordsBitrate(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)

	w := env.do("POST", fmt.Sprintf("/api/tracks/%d/download", tracks[0].ID), `{
		"username": "testuser",
		"filename": "Music/Test Artist - Track A1.mp3",
		"size": 8000000,
		"bit_rate": 320
	}`)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	track, _ := env.queries.GetTrackWithMeta(tracks[0].ID)
	if track.DownloadFormat == nil || *track.DownloadFormat != "mp3" {
		t.Errorf("expected download_format 'mp3', got %v", track.DownloadFormat)
	}
	if track.DownloadBitrate == nil || *track.DownloadBitrate != 320 {
		t.Errorf("expected download_bitrate 320, got %v", track.DownloadBitrate)
	}
}

func TestUpdateTrackQuality(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)

	err := env.queries.UpdateTrackQuality(tracks[0].ID, "flac", 0)
	if err != nil {
		t.Fatal(err)
	}

	track, _ := env.queries.GetTrackWithMeta(tracks[0].ID)
	if track.DownloadFormat == nil || *track.DownloadFormat != "flac" {
		t.Errorf("expected flac, got %v", track.DownloadFormat)
	}

	err = env.queries.UpdateTrackQuality(tracks[0].ID, "mp3", 320)
	if err != nil {
		t.Fatal(err)
	}
	track, _ = env.queries.GetTrackWithMeta(tracks[0].ID)
	if track.DownloadBitrate == nil || *track.DownloadBitrate != 320 {
		t.Errorf("expected 320, got %v", track.DownloadBitrate)
	}
}

func TestNextArtistForUpgrade(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artist, err := env.queries.NextArtistForUpgrade(0)
	if err != nil {
		t.Fatal(err)
	}
	if artist.Name != "Test Artist" {
		t.Errorf("expected 'Test Artist', got %q", artist.Name)
	}

	// Wrap around: past all artists should fail, then from 0 should work
	_, err = env.queries.NextArtistForUpgrade(999999)
	if err == nil {
		t.Error("expected error for ID past all artists")
	}
}

func TestListOwnedTracksByArtist(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)

	// No owned tracks initially
	owned, err := env.queries.ListOwnedTracksByArtist(artists[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(owned) != 0 {
		t.Errorf("expected 0 owned tracks, got %d", len(owned))
	}

	// Mark one as owned
	env.queries.UpdateTrackFilePath(tracks[0].ID, "/music/test.flac")

	owned, err = env.queries.ListOwnedTracksByArtist(artists[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(owned) != 1 {
		t.Errorf("expected 1 owned track, got %d", len(owned))
	}
	if owned[0].ArtistName != "Test Artist" {
		t.Errorf("expected artist name populated, got %q", owned[0].ArtistName)
	}
}

func TestLibrarySearch(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	w := env.do("GET", "/api/library/search?q=Track+A", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	results := decode[[]db.LibrarySearchResult](t, w)
	if len(results) != 2 {
		t.Fatalf("expected 2 results for 'Track A', got %d", len(results))
	}
	if results[0].ArtistName != "Test Artist" {
		t.Errorf("expected artist name, got %q", results[0].ArtistName)
	}
}

func TestLibrarySearchNoQuery(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/library/search", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	results := decode[[]db.LibrarySearchResult](t, w)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty query, got %d", len(results))
	}
}

func TestBrowseArtistTrackSearch(t *testing.T) {
	env := newTestEnv(t)

	w := env.do("GET", "/api/browse/artist/1000/tracks?q=A1", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result struct {
		Tracks []map[string]any `json:"tracks"`
	}
	json.NewDecoder(w.Body).Decode(&result)
	if len(result.Tracks) != 1 {
		t.Fatalf("expected 1 track matching 'A1', got %d", len(result.Tracks))
	}
}

func TestBrowseArtistTrackSearchEmptyQuery(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/browse/artist/1000/tracks?q=", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result struct {
		Tracks []map[string]any `json:"tracks"`
	}
	json.NewDecoder(w.Body).Decode(&result)
	if len(result.Tracks) != 0 {
		t.Errorf("expected 0 tracks for empty query, got %d", len(result.Tracks))
	}
}

func TestClearDownloadsByStatus(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)
	env.do("POST", "/api/downloads/queue", "")

	downloads, _ := env.queries.ListDownloads("pending")
	errStr := "test error"
	for _, d := range downloads[:2] {
		env.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusFailed, nil, &errStr)
	}

	w := env.do("DELETE", "/api/downloads/clear?status=failed", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	result := decode[map[string]int](t, w)
	if result["deleted"] != 2 {
		t.Errorf("expected 2 deleted, got %d", result["deleted"])
	}

	remaining, _ := env.queries.ListDownloads("")
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining download, got %d", len(remaining))
	}
}

func TestClearDownloadsBadStatus(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("DELETE", "/api/downloads/clear?status=bogus", "")
	if w.Code != 400 {
		t.Errorf("expected 400 for invalid clear status, got %d", w.Code)
	}
}

func TestWantedTracksWithCooldown(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	// All 3 tracks are wanted
	all, _ := env.queries.ListWantedTracks()
	if len(all) != 3 {
		t.Fatalf("expected 3 wanted tracks, got %d", len(all))
	}

	// Enqueue and fail one track
	env.queries.EnqueueDownload(all[0].ID)
	downloads, _ := env.queries.ListDownloads("pending")
	errStr := "no results"
	env.queries.UpdateDownloadStatus(downloads[0].ID, models.DownloadStatusFailed, nil, &errStr)
	env.queries.UpdateTrackStatus(all[0].ID, models.TrackStatusWanted)

	// Past cutoff: any failure after 2000 is "recent" → failed track excluded
	cutoff := "2000-01-01T00:00:00Z"
	filtered, _ := env.queries.ListWantedTracksWithCooldown(cutoff, 0)
	if len(filtered) != 2 {
		t.Errorf("expected 2 tracks (failed one excluded by past cutoff), got %d", len(filtered))
	}

	// Future cutoff: nothing failed after 2099 → all tracks included
	cutoff = "2099-01-01T00:00:00Z"
	filtered, _ = env.queries.ListWantedTracksWithCooldown(cutoff, 0)
	if len(filtered) != 3 {
		t.Errorf("expected 3 tracks (nothing excluded by future cutoff), got %d", len(filtered))
	}
}

func TestWantedTracksLimited(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	all, _ := env.queries.ListWantedTracks()
	if len(all) != 3 {
		t.Fatalf("expected 3 wanted tracks, got %d", len(all))
	}

	limited, _ := env.queries.ListWantedTracksLimited(2)
	if len(limited) != 2 {
		t.Errorf("expected 2 tracks with limit=2, got %d", len(limited))
	}

	unlimited, _ := env.queries.ListWantedTracksLimited(0)
	if len(unlimited) != 3 {
		t.Errorf("expected 3 tracks with limit=0, got %d", len(unlimited))
	}
}

func TestWantedTracksWithCooldownAndLimit(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	all, _ := env.queries.ListWantedTracks()
	env.queries.EnqueueDownload(all[0].ID)
	downloads, _ := env.queries.ListDownloads("pending")
	errStr := "no results"
	env.queries.UpdateDownloadStatus(downloads[0].ID, models.DownloadStatusFailed, nil, &errStr)
	env.queries.UpdateTrackStatus(all[0].ID, models.TrackStatusWanted)

	// Past cutoff excludes the failed one (2 remain), limit to 1
	cutoff := "2000-01-01T00:00:00Z"
	filtered, _ := env.queries.ListWantedTracksWithCooldown(cutoff, 1)
	if len(filtered) != 1 {
		t.Errorf("expected 1 track with cooldown+limit, got %d", len(filtered))
	}
}

// seedLargeLibrary creates numArtists artists, each with albumsPerArtist albums,
// each with tracksPerAlbum tracks. All tracks start as "wanted".
func seedLargeLibrary(t *testing.T, q *db.Queries, numArtists, albumsPerArtist, tracksPerAlbum int) []int64 {
	t.Helper()
	var trackIDs []int64
	for i := 0; i < numArtists; i++ {
		artist := models.Artist{
			Name:       fmt.Sprintf("Artist %03d", i),
			Provider:   "test",
			ProviderID: fmt.Sprintf("a-%d", i),
			Status:     models.ArtistStatusWatched,
		}
		if err := q.CreateArtist(&artist); err != nil {
			t.Fatalf("create artist %d: %v", i, err)
		}
		for j := 0; j < albumsPerArtist; j++ {
			album := models.Album{
				ArtistID:   artist.ID,
				Title:      fmt.Sprintf("Album %03d-%02d", i, j),
				Provider:   "test",
				ProviderID: fmt.Sprintf("al-%d-%d", i, j),
				RecordType: "album",
				Status:     models.AlbumStatusWatched,
			}
			if err := q.CreateAlbum(&album); err != nil {
				t.Fatalf("create album %d-%d: %v", i, j, err)
			}
			for k := 0; k < tracksPerAlbum; k++ {
				track := models.Track{
					AlbumID:     album.ID,
					Title:       fmt.Sprintf("Song %03d-%02d-%02d", i, j, k),
					TrackNumber: k + 1,
					DiscNumber:  1,
					DurationMs:  200000,
					Provider:    "test",
					ProviderID:  fmt.Sprintf("t-%d-%d-%d", i, j, k),
					Status:      models.TrackStatusWanted,
				}
				if err := q.CreateTrack(&track); err != nil {
					t.Fatalf("create track %d-%d-%d: %v", i, j, k, err)
				}
				trackIDs = append(trackIDs, track.ID)
			}
		}
	}
	return trackIDs
}

func TestLargeLibraryBatchCap(t *testing.T) {
	env := newTestEnv(t)
	trackIDs := seedLargeLibrary(t, env.queries, 40, 5, 20) // 40 × 5 × 20 = 4000 tracks

	if len(trackIDs) != 4000 {
		t.Fatalf("expected 4000 tracks seeded, got %d", len(trackIDs))
	}

	all, err := env.queries.ListWantedTracks()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4000 {
		t.Fatalf("expected 4000 wanted, got %d", len(all))
	}

	// Batch cap should limit results
	limited, err := env.queries.ListWantedTracksLimited(50)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 50 {
		t.Errorf("expected 50 tracks with limit, got %d", len(limited))
	}

	limited, err = env.queries.ListWantedTracksLimited(200)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 200 {
		t.Errorf("expected 200 tracks with limit, got %d", len(limited))
	}

	// Unlimited returns all
	unlimited, err := env.queries.ListWantedTracksLimited(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(unlimited) != 4000 {
		t.Errorf("expected 4000 tracks unlimited, got %d", len(unlimited))
	}
}

func TestLargeLibraryCooldownAtScale(t *testing.T) {
	env := newTestEnv(t)
	trackIDs := seedLargeLibrary(t, env.queries, 40, 5, 20) // 4000 tracks

	// Fail 500 of them
	for _, id := range trackIDs[:500] {
		env.queries.EnqueueDownload(id)
	}
	downloads, _ := env.queries.ListDownloads("pending")
	errStr := "no results"
	for _, d := range downloads {
		env.queries.UpdateDownloadStatus(d.ID, models.DownloadStatusFailed, nil, &errStr)
		env.queries.UpdateTrackStatus(d.TrackID, models.TrackStatusWanted)
	}

	// Past cutoff: 500 failed tracks excluded, 3500 remain
	cutoff := "2000-01-01T00:00:00Z"
	filtered, err := env.queries.ListWantedTracksWithCooldown(cutoff, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 3500 {
		t.Errorf("expected 3500 tracks after cooldown excludes 500, got %d", len(filtered))
	}

	// Cooldown + batch cap: 3500 eligible, cap at 50
	filtered, err = env.queries.ListWantedTracksWithCooldown(cutoff, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 50 {
		t.Errorf("expected 50 tracks with cooldown+cap, got %d", len(filtered))
	}

	// Future cutoff: nothing excluded, all 4000 returned
	cutoff = "2099-01-01T00:00:00Z"
	filtered, err = env.queries.ListWantedTracksWithCooldown(cutoff, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 4000 {
		t.Errorf("expected 4000 tracks with expired cooldown, got %d", len(filtered))
	}
}

func TestLargeLibrarySearchResults(t *testing.T) {
	env := newTestEnv(t)
	seedLargeLibrary(t, env.queries, 40, 5, 20) // 4000 tracks, titles like "Song 000-00-00"

	// Search for a specific artist's tracks
	results, err := env.queries.SearchLibrary("Song 005", 200)
	if err != nil {
		t.Fatal(err)
	}
	// Artist 005 has 5 albums × 20 tracks = 100 tracks matching "Song 005-"
	if len(results) != 100 {
		t.Errorf("expected 100 results for 'Song 005', got %d", len(results))
	}
	for _, r := range results {
		if r.ArtistName != "Artist 005" {
			t.Errorf("expected artist 'Artist 005', got %q", r.ArtistName)
			break
		}
	}

	// Search for a specific track
	results, err = env.queries.SearchLibrary("Song 003-01-07", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for exact track, got %d", len(results))
	}

	// Search with limit smaller than matches
	results, err = env.queries.SearchLibrary("Song 00", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 5 {
		t.Errorf("expected 5 results with limit, got %d", len(results))
	}

	// Broad search returns many across artists
	results, err = env.queries.SearchLibrary("Song 0", 4000)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4000 {
		t.Errorf("expected 4000 results for broad search, got %d", len(results))
	}
}

func TestLargeLibraryEnqueueBatch(t *testing.T) {
	env := newTestEnv(t)
	trackIDs := seedLargeLibrary(t, env.queries, 40, 5, 20) // 4000 tracks

	queued, err := env.queries.EnqueueDownloadBatch(trackIDs)
	if err != nil {
		t.Fatal(err)
	}
	if queued != 4000 {
		t.Errorf("expected 4000 queued, got %d", queued)
	}

	downloads, _ := env.queries.ListDownloads("pending")
	if len(downloads) != 4000 {
		t.Errorf("expected 4000 pending downloads, got %d", len(downloads))
	}

	// Batch enqueue again should be idempotent
	queued, err = env.queries.EnqueueDownloadBatch(trackIDs)
	if err != nil {
		t.Fatal(err)
	}
	if queued != 0 {
		t.Errorf("expected 0 queued on duplicate batch, got %d", queued)
	}
}

func TestUserQueuedDownloadHasSourceUser(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)

	env.do("POST", fmt.Sprintf("/api/tracks/%d/queue", tracks[0].ID), "")

	downloads, _ := env.queries.ListDownloads("")
	if len(downloads) != 1 {
		t.Fatalf("expected 1 download, got %d", len(downloads))
	}
	if downloads[0].Source != "user" {
		t.Errorf("expected source 'user', got %q", downloads[0].Source)
	}
}

func TestQueueAllWantedSetsSourceUser(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)
	env.do("POST", "/api/downloads/queue", "")

	downloads, _ := env.queries.ListDownloads("")
	for _, d := range downloads {
		if d.Source != "user" {
			t.Errorf("download %d: expected source 'user', got %q", d.ID, d.Source)
		}
	}
}

func TestDownloadsAPIReturnsSource(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)
	env.do("POST", "/api/downloads/queue", "")

	w := env.do("GET", "/api/downloads", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	downloads := decode[[]models.DownloadQueueItem](t, w)
	for _, d := range downloads {
		if d.Source != "user" {
			t.Errorf("download %d: expected source 'user' in API response, got %q", d.ID, d.Source)
		}
	}
}

func TestSchedulerSourcedDownloadAutoDeleted(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)

	env.queries.ReenqueueDownload(tracks[0].ID)

	downloads, _ := env.queries.ListDownloads("")
	if len(downloads) != 1 {
		t.Fatalf("expected 1 download, got %d", len(downloads))
	}
	if downloads[0].Source != "scheduler" {
		t.Fatalf("expected source 'scheduler', got %q", downloads[0].Source)
	}

	errMsg := "no results found"
	for i := 0; i < 4; i++ {
		env.queries.ScheduleRetry(downloads[0].ID, "2000-01-01T00:00:00Z", errMsg)
	}

	updated, _ := env.queries.ListDownloads("")
	if len(updated) != 1 {
		t.Fatalf("expected download to still exist before tick, got %d", len(updated))
	}
	if updated[0].Attempts != 4 {
		t.Fatalf("expected 4 attempts, got %d", updated[0].Attempts)
	}

	retryable, _ := env.queries.ListRetryableDownloads()
	if len(retryable) != 1 {
		t.Fatalf("expected 1 retryable, got %d", len(retryable))
	}
	if retryable[0].Source != "scheduler" {
		t.Errorf("retryable download source: expected 'scheduler', got %q", retryable[0].Source)
	}
}

func TestUserSourcedDownloadKeptAsFailed(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)

	env.do("POST", fmt.Sprintf("/api/tracks/%d/queue", tracks[0].ID), "")

	downloads, _ := env.queries.ListDownloads("")
	if downloads[0].Source != "user" {
		t.Fatalf("expected source 'user', got %q", downloads[0].Source)
	}

	errMsg := "transfer rejected"
	for i := 0; i < 4; i++ {
		env.queries.ScheduleRetry(downloads[0].ID, "2000-01-01T00:00:00Z", errMsg)
	}

	updated, _ := env.queries.ListDownloads("")
	if len(updated) != 1 {
		t.Fatalf("expected download to still exist, got %d", len(updated))
	}
	if updated[0].Attempts != 4 {
		t.Fatalf("expected 4 attempts, got %d", updated[0].Attempts)
	}
	if updated[0].Source != "user" {
		t.Errorf("source should still be 'user', got %q", updated[0].Source)
	}
}

func TestListBlacklistEmpty(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/blacklist", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var entries []models.BlacklistEntry
	json.Unmarshal(w.Body.Bytes(), &entries)
	if len(entries) != 0 {
		t.Errorf("expected empty blacklist, got %d", len(entries))
	}
}

func TestBlacklistCRUD(t *testing.T) {
	env := newTestEnv(t)

	env.queries.BlacklistFile("testuser", "bad/file.mp3", "transfer failed")
	env.queries.BlacklistFile("testuser2", "bad/file2.flac", "stale")

	w := env.do("GET", "/api/blacklist", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var entries []models.BlacklistEntry
	json.Unmarshal(w.Body.Bytes(), &entries)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	w = env.do("DELETE", fmt.Sprintf("/api/blacklist/%d", entries[0].ID), "")
	if w.Code != 204 {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	w = env.do("GET", "/api/blacklist", "")
	json.Unmarshal(w.Body.Bytes(), &entries)
	if len(entries) != 1 {
		t.Errorf("expected 1 entry after delete, got %d", len(entries))
	}
}

func TestListCooldownsEmpty(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("GET", "/api/cooldowns", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var cooldowns []models.UserCooldown
	json.Unmarshal(w.Body.Bytes(), &cooldowns)
	if len(cooldowns) != 0 {
		t.Errorf("expected empty cooldowns, got %d", len(cooldowns))
	}
}

func TestCooldownCRUD(t *testing.T) {
	env := newTestEnv(t)

	env.queries.CooldownUser("banned_user", "offline", 60*time.Minute)

	w := env.do("GET", "/api/cooldowns", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var cooldowns []models.UserCooldown
	json.Unmarshal(w.Body.Bytes(), &cooldowns)
	if len(cooldowns) != 1 {
		t.Fatalf("expected 1 cooldown, got %d", len(cooldowns))
	}
	if cooldowns[0].Username != "banned_user" {
		t.Errorf("expected banned_user, got %s", cooldowns[0].Username)
	}

	w = env.do("DELETE", fmt.Sprintf("/api/cooldowns/%d", cooldowns[0].ID), "")
	if w.Code != 204 {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	w = env.do("GET", "/api/cooldowns", "")
	json.Unmarshal(w.Body.Bytes(), &cooldowns)
	if len(cooldowns) != 0 {
		t.Errorf("expected 0 cooldowns after delete, got %d", len(cooldowns))
	}
}

func TestDeleteBlacklistBadID(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("DELETE", "/api/blacklist/abc", "")
	if w.Code != 400 {
		t.Errorf("expected 400 for invalid id, got %d", w.Code)
	}
}

func TestDeleteCooldownBadID(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("DELETE", "/api/cooldowns/abc", "")
	if w.Code != 400 {
		t.Errorf("expected 400 for invalid id, got %d", w.Code)
	}
}

// --- Reject Track ---

func newTestEnvWithLibrary(t *testing.T, libraryDir string) *testEnv {
	t.Helper()

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	c, err := cache.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })

	actLog, err := activity.NewLog(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { actLog.Close() })

	grpcAddr := startFakeProvider(t)

	fakeSlskd := newFakeSlskd()
	t.Cleanup(fakeSlskd.Close)

	queries := db.NewQueries(database)
	providerMgr := provider.NewManager(c, queries)

	if err := providerMgr.RegisterProvider(context.Background(), "test", grpcAddr); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	queries.SetSetting("provider_primary", "test")

	slskdClient := slskd.NewClient(fakeSlskd.URL, "test-key")
	org := &noopOrganizer{}
	dl := downloader.NewService(queries, slskdClient, org, actLog)

	srv := api.NewServer(queries, providerMgr, c, dl, actLog, nil, libraryDir, "test")

	return &testEnv{
		server:      srv,
		queries:     queries,
		activityLog: actLog,
		fakeSlskd:   fakeSlskd,
	}
}

func TestRejectTrackSuccess(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)

	env.queries.UpdateTrackFilePath(tracks[0].ID, "Test Artist/Album One/01 Track One.flac")
	env.queries.UpdateTrackDownloadedFrom(tracks[0].ID, "someuser")
	env.queries.UpdateTrackDownloadedFilename(tracks[0].ID, `@@someuser\Music\Test Artist\01 Track One.flac`)

	w := env.do("POST", fmt.Sprintf("/api/tracks/%d/reject", tracks[0].ID), "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	result := decode[map[string]string](t, w)
	if result["status"] != "rejected" {
		t.Errorf("expected status rejected, got %s", result["status"])
	}

	track, _ := env.queries.GetTrackWithMeta(tracks[0].ID)
	if track.Status != models.TrackStatusWanted {
		t.Errorf("expected track status wanted, got %s", track.Status)
	}
	if track.FilePath != nil {
		t.Errorf("expected file_path nil, got %v", track.FilePath)
	}
	if track.DownloadedFrom != nil {
		t.Errorf("expected downloaded_from nil, got %v", track.DownloadedFrom)
	}
	if track.DownloadedFilename != nil {
		t.Errorf("expected downloaded_filename nil, got %v", track.DownloadedFilename)
	}
	if track.DownloadFormat != nil {
		t.Errorf("expected download_format nil, got %v", track.DownloadFormat)
	}
}

func TestRejectTrackBlacklists(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)

	env.queries.UpdateTrackFilePath(tracks[0].ID, "Test Artist/Album One/01 Track One.flac")
	env.queries.UpdateTrackDownloadedFrom(tracks[0].ID, "baduser")
	env.queries.UpdateTrackDownloadedFilename(tracks[0].ID, `@@baduser\Music\Track One.flac`)

	env.do("POST", fmt.Sprintf("/api/tracks/%d/reject", tracks[0].ID), "")

	if !env.queries.IsBlacklisted("baduser", `@@baduser\Music\Track One.flac`) {
		t.Error("expected source to be blacklisted after reject")
	}
}

func TestRejectTrackNoBlacklistWithoutFilename(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)

	env.queries.UpdateTrackFilePath(tracks[0].ID, "Test Artist/Album One/01 Track One.flac")
	env.queries.UpdateTrackDownloadedFrom(tracks[0].ID, "someuser")

	w := env.do("POST", fmt.Sprintf("/api/tracks/%d/reject", tracks[0].ID), "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	blacklist, _ := env.queries.ListBlacklist()
	if len(blacklist) != 0 {
		t.Errorf("expected no blacklist entries when downloaded_filename is nil, got %d", len(blacklist))
	}
}

func TestRejectTrackReenqueues(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)

	env.queries.UpdateTrackFilePath(tracks[0].ID, "Test Artist/Album One/01 Track One.flac")

	env.do("POST", fmt.Sprintf("/api/tracks/%d/reject", tracks[0].ID), "")

	downloads, _ := env.queries.ListDownloads("pending")
	found := false
	for _, d := range downloads {
		if d.TrackID == tracks[0].ID {
			found = true
		}
	}
	if !found {
		t.Error("expected rejected track to be re-enqueued for download")
	}
}

func TestRejectTrackNotOwned(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)

	w := env.do("POST", fmt.Sprintf("/api/tracks/%d/reject", tracks[0].ID), "")
	if w.Code != 400 {
		t.Errorf("expected 400 for non-owned track, got %d", w.Code)
	}
}

func TestRejectTrackNotFound(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("POST", "/api/tracks/99999/reject", "")
	if w.Code != 404 {
		t.Errorf("expected 404 for unknown track, got %d", w.Code)
	}
}

func TestRejectTrackBadID(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("POST", "/api/tracks/abc/reject", "")
	if w.Code != 400 {
		t.Errorf("expected 400 for invalid id, got %d", w.Code)
	}
}

func TestRejectTrackByName(t *testing.T) {
	env := newTestEnv(t)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)

	env.queries.UpdateTrackFilePath(tracks[0].ID, "Test Artist/Album One/01 Track A1.flac")
	env.queries.UpdateTrackDownloadedFrom(tracks[0].ID, "someuser")
	env.queries.UpdateTrackDownloadedFilename(tracks[0].ID, `@@someuser\Music\Track A1.flac`)

	w := env.do("POST", "/api/tracks/reject", `{"artist":"Test Artist","title":"Track A1"}`)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	track, _ := env.queries.GetTrackWithMeta(tracks[0].ID)
	if track.Status != models.TrackStatusWanted {
		t.Errorf("expected track status wanted, got %s", track.Status)
	}

	if !env.queries.IsBlacklisted("someuser", `@@someuser\Music\Track A1.flac`) {
		t.Error("expected source blacklisted")
	}
}

func TestRejectTrackByNameNotFound(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("POST", "/api/tracks/reject", `{"artist":"Nobody","title":"Nothing"}`)
	if w.Code != 404 {
		t.Errorf("expected 404 for unknown track, got %d", w.Code)
	}
}

func TestRejectTrackByNameMissingFields(t *testing.T) {
	env := newTestEnv(t)
	w := env.do("POST", "/api/tracks/reject", `{"artist":""}`)
	if w.Code != 400 {
		t.Errorf("expected 400 for missing fields, got %d", w.Code)
	}
}

func TestRejectTrackDeletesFile(t *testing.T) {
	tmpDir := t.TempDir()
	env := newTestEnvWithLibrary(t, tmpDir)
	env.do("POST", "/api/watch/artist/1000", `{}`)

	artists, _ := env.queries.ListArtists()
	albums, _ := env.queries.ListAlbumsByArtist(artists[0].ID)
	tracks, _ := env.queries.ListTracksByAlbum(albums[0].ID)

	relPath := "Test Artist/Album One/01 Track One.flac"
	fullPath := filepath.Join(tmpDir, relPath)
	os.MkdirAll(filepath.Dir(fullPath), 0o755)
	os.WriteFile(fullPath, []byte("fake audio"), 0o644)

	env.queries.UpdateTrackFilePath(tracks[0].ID, relPath)

	env.do("POST", fmt.Sprintf("/api/tracks/%d/reject", tracks[0].ID), "")

	if _, err := os.Stat(fullPath); !os.IsNotExist(err) {
		t.Error("expected file to be deleted after reject")
	}
}
