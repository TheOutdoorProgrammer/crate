package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

func startTestServer(t *testing.T, fakeURL string) pb.MusicProviderClient {
	t.Helper()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()
	pb.RegisterMusicProviderServer(s, &server{
		http:      &http.Client{},
		limiter:   rate.NewLimiter(rate.Inf, 1),
		userAgent: "CrateTest/1.0",
		baseURL:   fakeURL,
	})
	go s.Serve(lis)
	t.Cleanup(s.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return pb.NewMusicProviderClient(conn)
}

func newFakeMusicBrainzAPI() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/artist/", func(w http.ResponseWriter, r *http.Request) {
		// /artist/?query=... (search) vs /artist/{id} (get)
		if r.URL.Query().Get("query") != "" {
			json.NewEncoder(w).Encode(map[string]any{
				"artists": []map[string]any{
					{
						"id":             "a74b1b7f-71a5-4011-9441-d0b5e4122711",
						"name":           "Radiohead",
						"score":          100,
						"country":        "GB",
						"disambiguation": "UK rock band",
						"type":           "Group",
					},
					{
						"id":    "b74b1b7f-71a5-4011-9441-d0b5e4122722",
						"name":  "Radiohead Tribute Band",
						"score": 42,
						"type":  "Group",
					},
				},
			})
			return
		}
		// GET /artist/{id}?fmt=json
		json.NewEncoder(w).Encode(map[string]any{
			"id":             "a74b1b7f-71a5-4011-9441-d0b5e4122711",
			"name":           "Radiohead",
			"country":        "GB",
			"disambiguation": "UK rock band",
			"type":           "Group",
			"life-span":      map[string]any{"begin": "1985", "end": ""},
		})
	})

	mux.HandleFunc("/release-group", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"release-groups": []map[string]any{
				{
					"id":                 "b1a9c1e6-1a3f-4a4a-9a5a-1a2b3c4d5e6f",
					"title":              "OK Computer",
					"primary-type":       "Album",
					"first-release-date": "1997-06-16",
				},
				{
					"id":                 "c2b3d4e5-2b4c-5d6e-0f1a-2b3c4d5e6f7a",
					"title":              "Creep",
					"primary-type":       "Single",
					"first-release-date": "1992-09-21",
				},
			},
		})
	})

	mux.HandleFunc("/release-group/", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "b1a9c1e6-1a3f-4a4a-9a5a-1a2b3c4d5e6f",
			"title": "OK Computer",
			"releases": []map[string]any{
				{"id": "rel-001", "date": "1997-06-16"},
			},
			"artist-credit": []map[string]any{
				{"artist": map[string]any{"name": "Radiohead"}},
			},
			"first-release-date": "1997-06-16",
		})
	})

	mux.HandleFunc("/release/", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"media": []map[string]any{
				{
					"position": 1,
					"tracks": []map[string]any{
						{"id": "t-001", "title": "Airbag", "number": "1", "position": 1, "length": 284000},
						{"id": "t-002", "title": "Paranoid Android", "number": "2", "position": 2, "length": 383000},
						{"id": "t-003", "title": "Subterranean Homesick Alien", "number": "3", "position": 3, "length": 267000},
					},
				},
			},
		})
	})

	return httptest.NewServer(mux)
}

func TestMusicBrainzInfo(t *testing.T) {
	fake := newFakeMusicBrainzAPI()
	defer fake.Close()
	client := startTestServer(t, fake.URL)

	resp, err := client.Info(context.Background(), &pb.InfoRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "musicbrainz" {
		t.Errorf("expected name 'musicbrainz', got %q", resp.Name)
	}
	if resp.DisplayName != "MusicBrainz" {
		t.Errorf("expected display_name 'MusicBrainz', got %q", resp.DisplayName)
	}
}

func TestMusicBrainzSearchArtists(t *testing.T) {
	fake := newFakeMusicBrainzAPI()
	defer fake.Close()
	client := startTestServer(t, fake.URL)

	resp, err := client.SearchArtists(context.Background(), &pb.SearchRequest{Query: "radiohead", Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Artists) != 2 {
		t.Fatalf("expected 2 artists, got %d", len(resp.Artists))
	}
	if resp.Artists[0].Name != "Radiohead" {
		t.Errorf("expected 'Radiohead', got %q", resp.Artists[0].Name)
	}
	if resp.Artists[0].Id != "a74b1b7f-71a5-4011-9441-d0b5e4122711" {
		t.Errorf("expected MBID, got %q", resp.Artists[0].Id)
	}
	if resp.Artists[0].Rank != 100 {
		t.Errorf("expected rank 100 (MB score), got %d", resp.Artists[0].Rank)
	}
	if resp.Artists[0].Metadata["country"] != "GB" {
		t.Errorf("expected country 'GB', got %v", resp.Artists[0].Metadata)
	}
	if resp.Artists[0].Metadata["disambiguation"] != "UK rock band" {
		t.Errorf("expected disambiguation, got %v", resp.Artists[0].Metadata)
	}
	if resp.Artists[0].Metadata["type"] != "Group" {
		t.Errorf("expected type 'Group', got %v", resp.Artists[0].Metadata)
	}
}

func TestMusicBrainzSearchDefaultLimit(t *testing.T) {
	fake := newFakeMusicBrainzAPI()
	defer fake.Close()
	client := startTestServer(t, fake.URL)

	resp, err := client.SearchArtists(context.Background(), &pb.SearchRequest{Query: "radiohead"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Artists) == 0 {
		t.Error("expected results with default limit")
	}
}

func TestMusicBrainzGetArtist(t *testing.T) {
	fake := newFakeMusicBrainzAPI()
	defer fake.Close()
	client := startTestServer(t, fake.URL)

	resp, err := client.GetArtist(context.Background(), &pb.EntityRequest{Id: "a74b1b7f-71a5-4011-9441-d0b5e4122711"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "Radiohead" {
		t.Errorf("expected 'Radiohead', got %q", resp.Name)
	}
	if resp.Metadata["country"] != "GB" {
		t.Errorf("expected country 'GB', got %v", resp.Metadata)
	}
	if resp.Metadata["active_since"] != "1985" {
		t.Errorf("expected active_since '1985', got %v", resp.Metadata)
	}
}

func TestMusicBrainzGetArtistAlbums(t *testing.T) {
	fake := newFakeMusicBrainzAPI()
	defer fake.Close()
	client := startTestServer(t, fake.URL)

	resp, err := client.GetArtistAlbums(context.Background(), &pb.EntityRequest{Id: "a74b1b7f-71a5-4011-9441-d0b5e4122711"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Albums) != 2 {
		t.Fatalf("expected 2 albums, got %d", len(resp.Albums))
	}
	if resp.Albums[0].Title != "OK Computer" {
		t.Errorf("expected 'OK Computer', got %q", resp.Albums[0].Title)
	}
	if resp.Albums[0].Year != 1997 {
		t.Errorf("expected year 1997, got %d", resp.Albums[0].Year)
	}
	if resp.Albums[0].RecordType != "album" {
		t.Errorf("expected record_type 'album', got %q", resp.Albums[0].RecordType)
	}
	if resp.Albums[1].RecordType != "single" {
		t.Errorf("expected record_type 'single' for Creep, got %q", resp.Albums[1].RecordType)
	}
	if resp.Albums[0].Metadata["release_date"] != "1997-06-16" {
		t.Errorf("expected release_date metadata, got %v", resp.Albums[0].Metadata)
	}
}

func TestMusicBrainzGetAlbum(t *testing.T) {
	fake := newFakeMusicBrainzAPI()
	defer fake.Close()
	client := startTestServer(t, fake.URL)

	resp, err := client.GetAlbum(context.Background(), &pb.EntityRequest{Id: "b1a9c1e6-1a3f-4a4a-9a5a-1a2b3c4d5e6f"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Title != "OK Computer" {
		t.Errorf("expected 'OK Computer', got %q", resp.Title)
	}
	if resp.ArtistName != "Radiohead" {
		t.Errorf("expected artist 'Radiohead', got %q", resp.ArtistName)
	}
	if resp.Year != 1997 {
		t.Errorf("expected year 1997, got %d", resp.Year)
	}
	if len(resp.Tracks) != 3 {
		t.Fatalf("expected 3 tracks, got %d", len(resp.Tracks))
	}
	if resp.Tracks[0].Title != "Airbag" {
		t.Errorf("expected 'Airbag', got %q", resp.Tracks[0].Title)
	}
	if resp.Tracks[0].TrackNumber != 1 {
		t.Errorf("expected track_number 1, got %d", resp.Tracks[0].TrackNumber)
	}
	if resp.Tracks[0].DiscNumber != 1 {
		t.Errorf("expected disc_number 1, got %d", resp.Tracks[0].DiscNumber)
	}
	if resp.Tracks[0].DurationMs != 284000 {
		t.Errorf("expected duration_ms 284000, got %d", resp.Tracks[0].DurationMs)
	}
}

func TestMusicBrainzAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	fake := httptest.NewServer(mux)
	defer fake.Close()
	client := startTestServer(t, fake.URL)

	_, err := client.SearchArtists(context.Background(), &pb.SearchRequest{Query: "test"})
	if err == nil {
		t.Error("expected error for 503 response")
	}
}

func TestMusicBrainzUserAgent(t *testing.T) {
	var receivedUA string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		receivedUA = r.Header.Get("User-Agent")
		json.NewEncoder(w).Encode(map[string]any{"artists": []any{}})
	})
	fake := httptest.NewServer(mux)
	defer fake.Close()
	client := startTestServer(t, fake.URL)

	client.SearchArtists(context.Background(), &pb.SearchRequest{Query: "test"})
	if receivedUA != "CrateTest/1.0" {
		t.Errorf("expected User-Agent 'CrateTest/1.0', got %q", receivedUA)
	}
}
