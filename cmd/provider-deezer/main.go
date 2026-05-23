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
	"sort"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"

	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

const deezerBaseURL = "https://api.deezer.com"

type server struct {
	pb.UnimplementedMusicProviderServer
	http    *http.Client
	limiter *rate.Limiter
	baseURL string
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	port := os.Getenv("PORT")
	if port == "" {
		port = "50052"
	}

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		slog.Error("failed to listen", "error", err)
		os.Exit(1)
	}

	s := grpc.NewServer()
	pb.RegisterMusicProviderServer(s, &server{
		http:    &http.Client{Timeout: 10 * time.Second},
		limiter: rate.NewLimiter(10, 10),
		baseURL: deezerBaseURL,
	})

	go func() {
		slog.Info("deezer provider starting", "port", port)
		if err := s.Serve(lis); err != nil {
			slog.Error("serve error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("deezer provider shutting down")
	s.GracefulStop()
}

func (s *server) Info(ctx context.Context, req *pb.InfoRequest) (*pb.InfoResponse, error) {
	return &pb.InfoResponse{
		Name:        "deezer",
		DisplayName: "Deezer",
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
		Data []struct {
			ID      int64  `json:"id"`
			Name    string `json:"name"`
			Picture string `json:"picture_medium"`
			NbAlbum int    `json:"nb_album"`
			NbFan   int    `json:"nb_fan"`
		} `json:"data"`
		Total int `json:"total"`
	}

	path := fmt.Sprintf("/search/artist?q=%s&limit=%d&index=%d", url.QueryEscape(req.Query), limit, offset)
	if err := s.get(ctx, path, &resp); err != nil {
		return nil, err
	}

	seen := make(map[int64]bool)
	var artists []*pb.ArtistResult
	for _, r := range resp.Data {
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		artists = append(artists, &pb.ArtistResult{
			Id:         strconv.FormatInt(r.ID, 10),
			Name:       r.Name,
			ImageUrl:   r.Picture,
			AlbumCount: int32(r.NbAlbum),
			Rank:       int32(r.NbFan),
			Metadata: map[string]string{
				"fans": strconv.Itoa(r.NbFan),
			},
		})
	}

	sort.Slice(artists, func(i, j int) bool {
		return artists[i].Rank > artists[j].Rank
	})

	total := resp.Total
	if total == 0 {
		total = len(artists)
	}
	return &pb.ArtistSearchResponse{Artists: artists, Total: int32(total)}, nil
}

func (s *server) GetArtist(ctx context.Context, req *pb.EntityRequest) (*pb.ArtistDetail, error) {
	var resp struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		Picture string `json:"picture_medium"`
		NbFan   int    `json:"nb_fan"`
	}

	if err := s.get(ctx, "/artist/"+req.Id, &resp); err != nil {
		return nil, err
	}

	return &pb.ArtistDetail{
		Id:       strconv.FormatInt(resp.ID, 10),
		Name:     resp.Name,
		ImageUrl: resp.Picture,
		Metadata: map[string]string{
			"fans": strconv.Itoa(resp.NbFan),
		},
	}, nil
}

func (s *server) GetArtistAlbums(ctx context.Context, req *pb.EntityRequest) (*pb.AlbumList, error) {
	var resp struct {
		Data []struct {
			ID          int64  `json:"id"`
			Title       string `json:"title"`
			CoverMedium string `json:"cover_medium"`
			ReleaseDate string `json:"release_date"`
			RecordType  string `json:"record_type"`
		} `json:"data"`
	}

	if err := s.get(ctx, "/artist/"+req.Id+"/albums?limit=100", &resp); err != nil {
		return nil, err
	}

	var albums []*pb.AlbumSummary
	for i, a := range resp.Data {
		year := parseYear(a.ReleaseDate)
		albums = append(albums, &pb.AlbumSummary{
			Id:         strconv.FormatInt(a.ID, 10),
			Title:      a.Title,
			CoverUrl:   a.CoverMedium,
			Year:       int32(year),
			RecordType: a.RecordType,
			Rank:       int32(len(resp.Data) - i),
			Metadata: map[string]string{
				"release_date": a.ReleaseDate,
			},
		})
	}

	return &pb.AlbumList{Albums: albums}, nil
}

func (s *server) GetAlbum(ctx context.Context, req *pb.EntityRequest) (*pb.AlbumDetail, error) {
	var resp struct {
		ID          int64  `json:"id"`
		Title       string `json:"title"`
		CoverMedium string `json:"cover_medium"`
		ReleaseDate string `json:"release_date"`
		Artist      struct {
			Name string `json:"name"`
		} `json:"artist"`
		Tracks struct {
			Data []struct {
				ID            int64  `json:"id"`
				Title         string `json:"title"`
				TrackPosition int    `json:"track_position"`
				DiskNumber    int    `json:"disk_number"`
				Duration      int    `json:"duration"`
			} `json:"data"`
		} `json:"tracks"`
	}

	if err := s.get(ctx, "/album/"+req.Id, &resp); err != nil {
		return nil, err
	}

	year := parseYear(resp.ReleaseDate)
	var tracks []*pb.TrackInfo
	for _, t := range resp.Tracks.Data {
		tracks = append(tracks, &pb.TrackInfo{
			Id:          strconv.FormatInt(t.ID, 10),
			Title:       t.Title,
			TrackNumber: int32(t.TrackPosition),
			DiscNumber:  int32(t.DiskNumber),
			DurationMs:  int32(t.Duration * 1000),
			Rank:        int32(t.TrackPosition),
		})
	}

	return &pb.AlbumDetail{
		Id:         strconv.FormatInt(resp.ID, 10),
		Title:      resp.Title,
		CoverUrl:   resp.CoverMedium,
		Year:       int32(year),
		ArtistName: resp.Artist.Name,
		Tracks:     tracks,
	}, nil
}

func (s *server) SearchArtistTracks(ctx context.Context, req *pb.ArtistTrackSearchRequest) (*pb.ArtistTrackSearchResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}

	// Deezer search supports artist and track filters
	q := fmt.Sprintf("artist:\"%s\" track:\"%s\"", req.ArtistId, req.Query)

	// First, get artist name since ArtistId is numeric
	var artistResp struct {
		Name string `json:"name"`
	}
	if err := s.get(ctx, "/artist/"+req.ArtistId, &artistResp); err == nil && artistResp.Name != "" {
		q = fmt.Sprintf("artist:\"%s\" track:\"%s\"", artistResp.Name, req.Query)
	}

	var resp struct {
		Data []struct {
			ID       int64  `json:"id"`
			Title    string `json:"title"`
			Duration int    `json:"duration"`
			Album    struct {
				ID          int64  `json:"id"`
				Title       string `json:"title"`
				CoverMedium string `json:"cover_medium"`
				ReleaseDate string `json:"release_date"`
			} `json:"album"`
		} `json:"data"`
	}

	path := fmt.Sprintf("/search/track?q=%s&limit=%d", url.QueryEscape(q), limit)
	if err := s.get(ctx, path, &resp); err != nil {
		return nil, err
	}

	seen := map[int64]bool{}
	var tracks []*pb.TrackSearchResult
	for _, t := range resp.Data {
		if seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		tracks = append(tracks, &pb.TrackSearchResult{
			Id:            strconv.FormatInt(t.ID, 10),
			Title:         t.Title,
			DurationMs:    int32(t.Duration * 1000),
			AlbumId:       strconv.FormatInt(t.Album.ID, 10),
			AlbumTitle:    t.Album.Title,
			AlbumCoverUrl: t.Album.CoverMedium,
			AlbumYear:     int32(parseYear(t.Album.ReleaseDate)),
		})
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
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("deezer API returned status %d", resp.StatusCode)
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
