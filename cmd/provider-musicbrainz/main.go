package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"

	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

const mbBaseURL = "https://musicbrainz.org/ws/2"

type server struct {
	pb.UnimplementedMusicProviderServer
	http      *http.Client
	limiter   *rate.Limiter
	userAgent string
	baseURL   string
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	port := os.Getenv("PORT")
	if port == "" {
		port = "50051"
	}

	userAgent := os.Getenv("MB_USER_AGENT")
	if userAgent == "" {
		userAgent = "Crate/0.1.0 (https://github.com/TheOutdoorProgrammer/crate)"
	}

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		slog.Error("failed to listen", "error", err)
		os.Exit(1)
	}

	s := grpc.NewServer()
	pb.RegisterMusicProviderServer(s, &server{
		http:      &http.Client{Timeout: 15 * time.Second},
		limiter:   rate.NewLimiter(1, 1),
		userAgent: userAgent,
		baseURL:   mbBaseURL,
	})

	go func() {
		slog.Info("musicbrainz provider starting", "port", port)
		if err := s.Serve(lis); err != nil {
			slog.Error("serve error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("musicbrainz provider shutting down")
	s.GracefulStop()
}

func (s *server) Info(ctx context.Context, req *pb.InfoRequest) (*pb.InfoResponse, error) {
	return &pb.InfoResponse{
		Name:        "musicbrainz",
		DisplayName: "MusicBrainz",
		Version:     "1.0.0",
	}, nil
}

func (s *server) SearchArtists(ctx context.Context, req *pb.SearchRequest) (*pb.ArtistSearchResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 25
	}

	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	var resp struct {
		Count  int `json:"count"`
		Artists []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			Score          int    `json:"score"`
			Country        string `json:"country"`
			Disambiguation string `json:"disambiguation"`
			Type           string `json:"type"`
		} `json:"artists"`
	}

	path := fmt.Sprintf("/artist/?query=%s&limit=%d&offset=%d&fmt=json", url.QueryEscape(req.Query), limit, offset)
	if err := s.get(ctx, path, &resp); err != nil {
		return nil, err
	}

	var artists []*pb.ArtistResult
	for _, a := range resp.Artists {
		meta := map[string]string{}
		if a.Country != "" {
			meta["country"] = a.Country
		}
		if a.Disambiguation != "" {
			meta["disambiguation"] = a.Disambiguation
		}
		if a.Type != "" {
			meta["type"] = a.Type
		}
		artists = append(artists, &pb.ArtistResult{
			Id:       a.ID,
			Name:     a.Name,
			Rank:     int32(a.Score),
			Metadata: meta,
		})
	}

	return &pb.ArtistSearchResponse{Artists: artists, Total: int32(resp.Count)}, nil
}

func (s *server) GetArtist(ctx context.Context, req *pb.EntityRequest) (*pb.ArtistDetail, error) {
	var resp struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Country        string `json:"country"`
		Disambiguation string `json:"disambiguation"`
		Type           string `json:"type"`
		LifeSpan       struct {
			Begin string `json:"begin"`
			End   string `json:"end"`
		} `json:"life-span"`
	}

	if err := s.get(ctx, "/artist/"+req.Id+"?fmt=json", &resp); err != nil {
		return nil, err
	}

	meta := map[string]string{}
	if resp.Country != "" {
		meta["country"] = resp.Country
	}
	if resp.Disambiguation != "" {
		meta["disambiguation"] = resp.Disambiguation
	}
	if resp.Type != "" {
		meta["type"] = resp.Type
	}
	if resp.LifeSpan.Begin != "" {
		meta["active_since"] = resp.LifeSpan.Begin
	}

	return &pb.ArtistDetail{
		Id:       resp.ID,
		Name:     resp.Name,
		Metadata: meta,
	}, nil
}

func (s *server) GetArtistAlbums(ctx context.Context, req *pb.EntityRequest) (*pb.AlbumList, error) {
	var resp struct {
		ReleaseGroups []struct {
			ID               string `json:"id"`
			Title            string `json:"title"`
			PrimaryType      string `json:"primary-type"`
			FirstReleaseDate string `json:"first-release-date"`
		} `json:"release-groups"`
	}

	path := fmt.Sprintf("/release-group?artist=%s&limit=100&fmt=json", req.Id)
	if err := s.get(ctx, path, &resp); err != nil {
		return nil, err
	}

	var albums []*pb.AlbumSummary
	for i, rg := range resp.ReleaseGroups {
		year := parseYear(rg.FirstReleaseDate)
		recordType := "album"
		switch rg.PrimaryType {
		case "Single":
			recordType = "single"
		case "EP":
			recordType = "ep"
		case "Compilation":
			recordType = "compilation"
		}
		albums = append(albums, &pb.AlbumSummary{
			Id:         rg.ID,
			Title:      rg.Title,
			Year:       int32(year),
			CoverUrl:   "https://coverartarchive.org/release-group/" + rg.ID + "/front-250",
			RecordType: recordType,
			Rank:       int32(len(resp.ReleaseGroups) - i),
			Metadata: map[string]string{
				"release_date": rg.FirstReleaseDate,
			},
		})
	}

	return &pb.AlbumList{Albums: albums}, nil
}

func (s *server) GetAlbum(ctx context.Context, req *pb.EntityRequest) (*pb.AlbumDetail, error) {
	var rgResp struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Releases []struct {
			ID   string `json:"id"`
			Date string `json:"date"`
		} `json:"releases"`
		ArtistCredit []struct {
			Artist struct {
				Name string `json:"name"`
			} `json:"artist"`
		} `json:"artist-credit"`
		FirstReleaseDate string `json:"first-release-date"`
	}

	if err := s.get(ctx, "/release-group/"+req.Id+"?inc=releases+artist-credits&fmt=json", &rgResp); err != nil {
		return nil, err
	}

	artistName := ""
	if len(rgResp.ArtistCredit) > 0 {
		artistName = rgResp.ArtistCredit[0].Artist.Name
	}

	year := parseYear(rgResp.FirstReleaseDate)

	var tracks []*pb.TrackInfo
	if len(rgResp.Releases) > 0 {
		releaseID := rgResp.Releases[0].ID

		var relResp struct {
			Media []struct {
				Position int `json:"position"`
				Tracks   []struct {
					ID       string `json:"id"`
					Title    string `json:"title"`
					Number   string `json:"number"`
					Position int    `json:"position"`
					Length   int    `json:"length"`
				} `json:"tracks"`
			} `json:"media"`
		}

		if err := s.get(ctx, "/release/"+releaseID+"?inc=recordings&fmt=json", &relResp); err == nil {
			for _, media := range relResp.Media {
				for _, t := range media.Tracks {
					trackNum, _ := strconv.Atoi(t.Number)
					if trackNum == 0 {
						trackNum = t.Position
					}
					tracks = append(tracks, &pb.TrackInfo{
						Id:          t.ID,
						Title:       t.Title,
						TrackNumber: int32(trackNum),
						DiscNumber:  int32(media.Position),
						DurationMs:  int32(t.Length),
						Rank:        int32(t.Position),
					})
				}
			}
		}
	}

	return &pb.AlbumDetail{
		Id:         rgResp.ID,
		Title:      rgResp.Title,
		Year:       int32(year),
		CoverUrl:   "https://coverartarchive.org/release-group/" + rgResp.ID + "/front-250",
		ArtistName: artistName,
		Tracks:     tracks,
	}, nil
}

func (s *server) SearchArtistTracks(ctx context.Context, req *pb.ArtistTrackSearchRequest) (*pb.ArtistTrackSearchResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}

	var resp struct {
		Recordings []struct {
			ID     string `json:"id"`
			Title  string `json:"title"`
			Length int    `json:"length"`
			Releases []struct {
				ID           string `json:"id"`
				Title        string `json:"title"`
				Date         string `json:"date"`
				ReleaseGroup struct {
					ID string `json:"id"`
				} `json:"release-group"`
			} `json:"releases"`
		} `json:"recordings"`
	}

	path := fmt.Sprintf("/recording/?query=arid:%s AND recording:%s&limit=%d&fmt=json",
		req.ArtistId, url.QueryEscape(req.Query), limit)
	if err := s.get(ctx, path, &resp); err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var tracks []*pb.TrackSearchResult
	for _, rec := range resp.Recordings {
		if seen[rec.ID] {
			continue
		}
		seen[rec.ID] = true

		t := &pb.TrackSearchResult{
			Id:         rec.ID,
			Title:      rec.Title,
			DurationMs: int32(rec.Length),
		}
		if len(rec.Releases) > 0 {
			rel := rec.Releases[0]
			rgID := rel.ReleaseGroup.ID
			if rgID == "" {
				rgID = rel.ID
			}
			t.AlbumId = rgID
			t.AlbumTitle = rel.Title
			t.AlbumCoverUrl = "https://coverartarchive.org/release-group/" + rgID + "/front-250"
			t.AlbumYear = int32(parseYear(rel.Date))
		}
		tracks = append(tracks, t)
	}

	return &pb.ArtistTrackSearchResponse{Tracks: tracks}, nil
}

func (s *server) get(ctx context.Context, path string, out any) error {
	if err := s.limiter.Wait(ctx); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", s.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", s.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("musicbrainz API returned status %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func parseYear(dateStr string) int {
	if len(dateStr) >= 4 {
		y, _ := strconv.Atoi(dateStr[:4])
		return y
	}
	return 0
}
