package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/models"
	"github.com/TheOutdoorProgrammer/crate/internal/provider"
	"github.com/TheOutdoorProgrammer/crate/internal/services/downloader"
	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func parseID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

// Status

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	artists, _ := s.queries.ListArtists()
	downloads, _ := s.queries.ListDownloads("")

	pending := 0
	active := 0
	for _, d := range downloads {
		switch d.Status {
		case "pending":
			pending++
		case "searching", "downloading":
			active++
		}
	}

	totalTracks := 0
	ownedTracks := 0
	for _, a := range artists {
		totalTracks += a.TotalTracks
		ownedTracks += a.OwnedTracks
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":            "ok",
		"artists_count":     len(artists),
		"total_tracks":      totalTracks,
		"owned_tracks":      ownedTracks,
		"pending_downloads": pending,
		"active_downloads":  active,
	})
}

// Search

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}

	var limit, offset int32 = 25, 0
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		fmt.Sscanf(o, "%d", &offset)
	}

	providerName := r.URL.Query().Get("provider")
	var result *provider.SearchResult
	var err error
	if providerName != "" {
		result, err = s.providers.SearchWithProvider(r.Context(), providerName, query, limit, offset)
	} else {
		result, err = s.providers.Search(r.Context(), query, limit, offset)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleLibrarySearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	results, err := s.queries.SearchLibrary(query, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "library search failed")
		return
	}
	if results == nil {
		results = []db.LibrarySearchResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleBrowseArtistTrackSearch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	query := r.URL.Query().Get("q")
	if query == "" {
		writeJSON(w, http.StatusOK, map[string]any{"tracks": []any{}})
		return
	}
	providerName := r.URL.Query().Get("provider")
	if providerName == "" {
		providerName = s.providers.Primary()
	}
	result, err := s.providers.SearchArtistTracks(r.Context(), providerName, id, query, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "track search failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// Browse

func (s *Server) handleBrowseArtist(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	primary := r.URL.Query().Get("provider")
	if primary == "" {
		primary = s.providers.Primary()
	}

	artist, err := s.providers.GetArtist(r.Context(), primary, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to browse artist")
		return
	}

	albums, err := s.providers.GetArtistAlbums(r.Context(), primary, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get artist albums")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":          artist.Id,
		"name":        artist.Name,
		"image_url":   artist.ImageUrl,
		"metadata":    artist.Metadata,
		"album_count": len(albums.Albums),
		"albums":      albums.Albums,
	})
}

func (s *Server) handleBrowseAlbum(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	primary := r.URL.Query().Get("provider")
	if primary == "" {
		primary = s.providers.Primary()
	}

	album, err := s.providers.GetAlbum(r.Context(), primary, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to browse album")
		return
	}

	writeJSON(w, http.StatusOK, album)
}

// Watch

func (s *Server) handleWatchArtist(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "id")

	var req struct {
		WatchNewReleases bool   `json:"watch_new_releases"`
		Provider         string `json:"provider,omitempty"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	primary := req.Provider
	if primary == "" {
		primary = s.providers.Primary()
	}

	existing, err := s.queries.FindArtistByProvider(primary, providerID)
	if err == nil && existing != nil && existing.Status == models.ArtistStatusWatched {
		writeJSON(w, http.StatusOK, existing)
		return
	}

	artistDetail, err := s.providers.GetArtist(r.Context(), primary, providerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch artist")
		return
	}

	albumList, err := s.providers.GetArtistAlbums(r.Context(), primary, providerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch artist albums")
		return
	}

	if existing != nil {
		if err := s.queries.UpdateArtistStatus(existing.ID, models.ArtistStatusWatched); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update artist status")
			return
		}
		if req.WatchNewReleases {
			if err := s.queries.SetArtistWatchNewReleases(existing.ID, true); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update new releases setting")
				return
			}
		}

		albums := albumList.Albums
		s.bgWork.Add(1)
		go func() {
			defer s.bgWork.Done()
			s.saveAlbumsFromProvider(primary, existing.ID, albums)
		}()

		writeJSON(w, http.StatusOK, existing)
		return
	}

	imgURL := artistDetail.ImageUrl
	artist := &models.Artist{
		Name:       artistDetail.Name,
		Provider:   primary,
		ProviderID: providerID,
		ImageURL:   strPtrOrNil(imgURL),
		Status:     models.ArtistStatusWatched,
	}
	if req.WatchNewReleases {
		artist.WatchNewReleases = true
		ts := time.Now().UTC().Format(time.RFC3339)
		artist.WatchNewReleasesSince = &ts
	}
	if err := s.queries.CreateArtist(artist); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save artist")
		return
	}

	albums := albumList.Albums
	s.bgWork.Add(1)
	go func() {
		defer s.bgWork.Done()
		s.saveAlbumsFromProvider(primary, artist.ID, albums)
	}()

	writeJSON(w, http.StatusCreated, artist)
}

func (s *Server) saveAlbumsFromProvider(providerName string, artistID int64, albums []*pb.AlbumSummary) {
	ctx := context.Background()
	for _, pa := range albums {
		if existingAlbum, _ := s.queries.FindAlbumByProvider(providerName, pa.Id); existingAlbum != nil {
			continue
		}
		s.saveAlbumFromProvider(ctx, providerName, artistID, pa)
	}
}

func (s *Server) saveAlbumFromProvider(ctx context.Context, providerName string, artistID int64, pa *pb.AlbumSummary) {
	year := intPtrOrNil(int(pa.Year))
	cover := pa.CoverUrl
	album := &models.Album{
		ArtistID:   artistID,
		Title:      pa.Title,
		Year:       year,
		Provider:   providerName,
		ProviderID: pa.Id,
		CoverURL:   strPtrOrNil(cover),
		RecordType: pa.RecordType,
		Status:     models.AlbumStatusWatched,
	}
	if err := s.queries.CreateAlbum(album); err != nil {
		return
	}

	albumDetail, err := s.providers.GetAlbum(ctx, providerName, pa.Id)
	if err != nil {
		return
	}
	for _, pt := range albumDetail.Tracks {
		s.queries.CreateTrack(&models.Track{
			AlbumID:     album.ID,
			Title:       pt.Title,
			TrackNumber: int(pt.TrackNumber),
			DiscNumber:  int(pt.DiscNumber),
			DurationMs:  int(pt.DurationMs),
			Provider:    providerName,
			ProviderID:  pt.Id,
			Status:      models.TrackStatusWanted,
		})
	}
}

func (s *Server) handleWatchAlbum(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "id")

	var req struct {
		ArtistProviderID string `json:"artist_provider_id"`
		ArtistName       string `json:"artist_name"`
		ArtistImageURL   string `json:"artist_image_url"`
		Provider         string `json:"provider,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	primary := req.Provider
	if primary == "" {
		primary = s.providers.Primary()
	}

	artist, err := s.queries.FindArtistByProvider(primary, req.ArtistProviderID)
	if errors.Is(err, sql.ErrNoRows) || artist == nil {
		artist = &models.Artist{
			Name:       req.ArtistName,
			Provider:   primary,
			ProviderID: req.ArtistProviderID,
			ImageURL:   strPtrOrNil(req.ArtistImageURL),
			Status:     models.ArtistStatusPartial,
		}
		if err := s.queries.CreateArtist(artist); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save artist")
			return
		}
	}

	existingAlbum, _ := s.queries.FindAlbumByProvider(primary, providerID)
	if existingAlbum != nil {
		writeJSON(w, http.StatusOK, existingAlbum)
		return
	}

	albumDetail, err := s.providers.GetAlbum(r.Context(), primary, providerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch album")
		return
	}

	year := intPtrOrNil(int(albumDetail.Year))
	cover := albumDetail.CoverUrl
	album := &models.Album{
		ArtistID:   artist.ID,
		Title:      albumDetail.Title,
		Year:       year,
		Provider:   primary,
		ProviderID: albumDetail.Id,
		CoverURL:   strPtrOrNil(cover),
		RecordType: "album",
		Status:     models.AlbumStatusWatched,
	}
	if err := s.queries.CreateAlbum(album); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save album")
		return
	}

	for _, pt := range albumDetail.Tracks {
		s.queries.CreateTrack(&models.Track{
			AlbumID:     album.ID,
			Title:       pt.Title,
			TrackNumber: int(pt.TrackNumber),
			DiscNumber:  int(pt.DiscNumber),
			DurationMs:  int(pt.DurationMs),
			Provider:    primary,
			ProviderID:  pt.Id,
			Status:      models.TrackStatusWanted,
		})
	}

	writeJSON(w, http.StatusCreated, album)
}

func (s *Server) handleWatchTrack(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "id")

	var req struct {
		ArtistProviderID string `json:"artist_provider_id"`
		ArtistName       string `json:"artist_name"`
		ArtistImageURL   string `json:"artist_image_url"`
		AlbumProviderID  string `json:"album_provider_id"`
		AlbumTitle       string `json:"album_title"`
		AlbumCoverURL    string `json:"album_cover_url"`
		AlbumYear        *int   `json:"album_year"`
		Title            string `json:"title"`
		TrackNumber      int    `json:"track_number"`
		DiscNumber       int    `json:"disc_number"`
		DurationMs       int    `json:"duration_ms"`
		Provider         string `json:"provider,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	primary := req.Provider
	if primary == "" {
		primary = s.providers.Primary()
	}

	artist, err := s.queries.FindArtistByProvider(primary, req.ArtistProviderID)
	if errors.Is(err, sql.ErrNoRows) || artist == nil {
		artist = &models.Artist{
			Name:       req.ArtistName,
			Provider:   primary,
			ProviderID: req.ArtistProviderID,
			ImageURL:   strPtrOrNil(req.ArtistImageURL),
			Status:     models.ArtistStatusPartial,
		}
		if err := s.queries.CreateArtist(artist); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save artist")
			return
		}
	}

	album, err := s.queries.FindAlbumByProvider(primary, req.AlbumProviderID)
	if errors.Is(err, sql.ErrNoRows) || album == nil {
		album = &models.Album{
			ArtistID:   artist.ID,
			Title:      req.AlbumTitle,
			Year:       req.AlbumYear,
			Provider:   primary,
			ProviderID: req.AlbumProviderID,
			CoverURL:   strPtrOrNil(req.AlbumCoverURL),
			RecordType: "album",
			Status:     models.AlbumStatusWatched,
		}
		if err := s.queries.CreateAlbum(album); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save album")
			return
		}
	}

	track := &models.Track{
		AlbumID:     album.ID,
		Title:       req.Title,
		TrackNumber: req.TrackNumber,
		DiscNumber:  req.DiscNumber,
		DurationMs:  req.DurationMs,
		Provider:    primary,
		ProviderID:  providerID,
		Status:      models.TrackStatusWanted,
	}
	if err := s.queries.CreateTrack(track); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save track")
		return
	}

	writeJSON(w, http.StatusCreated, track)
}

// Library

func (s *Server) handleListArtists(w http.ResponseWriter, r *http.Request) {
	artists, err := s.queries.ListArtists()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list artists")
		return
	}
	if artists == nil {
		writeJSON(w, http.StatusOK, []struct{}{})
		return
	}

	healthCache := make(map[string]bool)
	for i := range artists {
		p := artists[i].Provider
		if _, ok := healthCache[p]; !ok {
			healthCache[p] = s.providers.IsHealthy(p)
		}
		artists[i].Orphaned = !healthCache[p]
	}

	writeJSON(w, http.StatusOK, artists)
}

func (s *Server) handleGetArtist(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	artist, err := s.queries.GetArtist(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "artist not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get artist")
		return
	}

	albums, _ := s.queries.ListAlbumsByArtist(id)
	for i := range albums {
		tracks, _ := s.queries.ListTracksByAlbum(albums[i].ID)
		albums[i].Tracks = tracks
	}
	artist.Albums = albums
	artist.Orphaned = !s.providers.IsHealthy(artist.Provider)

	writeJSON(w, http.StatusOK, artist)
}

func (s *Server) handleGetAlbum(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	album, err := s.queries.GetAlbum(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "album not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get album")
		return
	}

	tracks, _ := s.queries.ListTracksByAlbum(id)
	album.Tracks = tracks

	writeJSON(w, http.StatusOK, album)
}

// Unwatch

func (s *Server) handleToggleNewReleases(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.queries.SetArtistWatchNewReleases(id, req.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update artist")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"watch_new_releases": req.Enabled})
}

func (s *Server) handleUnwatchArtist(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.queries.DeleteArtist(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete artist")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUnwatchAlbum(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.queries.DeleteAlbum(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete album")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleQueueTrack(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.queries.EnqueueDownload(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to queue track")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}

func (s *Server) handleQueueArtistTracks(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	tracks, err := s.queries.ListWantedTracksByArtist(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list wanted tracks")
		return
	}
	ids := make([]int64, len(tracks))
	for i, t := range tracks {
		ids[i] = t.ID
	}
	queued, err := s.queries.EnqueueDownloadBatch(ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to queue tracks")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"queued": queued})
}

func (s *Server) handleQueueAlbumTracks(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	tracks, err := s.queries.ListWantedTracksByAlbum(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list wanted tracks")
		return
	}
	ids := make([]int64, len(tracks))
	for i, t := range tracks {
		ids[i] = t.ID
	}
	queued, err := s.queries.EnqueueDownloadBatch(ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to queue tracks")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"queued": queued})
}

func (s *Server) handleManualSearch(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	results, err := s.downloader.ManualSearch(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed: "+err.Error())
		return
	}
	if results == nil {
		results = []downloader.ManualSearchResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleManualDownload(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Username string `json:"username"`
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
		BitRate  int    `json:"bit_rate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.downloader.ManualDownload(r.Context(), id, req.Username, req.Filename, req.Size, req.BitRate); err != nil {
		writeError(w, http.StatusInternalServerError, "download failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "downloading"})
}

func (s *Server) handleUnwatchTrack(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.queries.DeleteTrack(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete track")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Downloads

func (s *Server) handleDownloadProgress(w http.ResponseWriter, r *http.Request) {
	progress, err := s.downloader.GetProgress(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get progress")
		return
	}
	writeJSON(w, http.StatusOK, progress)
}

func (s *Server) handleListDownloads(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	downloads, err := s.queries.ListDownloadsWithTrack(status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list downloads")
		return
	}
	if downloads == nil {
		downloads = []models.DownloadQueueItem{}
	}
	writeJSON(w, http.StatusOK, downloads)
}

func (s *Server) handleQueueDownloads(w http.ResponseWriter, r *http.Request) {
	tracks, err := s.queries.ListWantedTracks()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list wanted tracks")
		return
	}
	ids := make([]int64, len(tracks))
	for i, t := range tracks {
		ids[i] = t.ID
	}
	queued, err := s.queries.EnqueueDownloadBatch(ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to queue tracks")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"queued": queued})
}

func (s *Server) handleDeleteDownload(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.queries.DeleteDownload(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete download")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleClearDownloadsByStatus(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		writeError(w, http.StatusBadRequest, "status parameter required")
		return
	}
	allowed := map[string]bool{"failed": true, "complete": true, "pending": true}
	if !allowed[status] {
		writeError(w, http.StatusBadRequest, "can only clear failed, complete, or pending downloads")
		return
	}
	deleted, err := s.queries.DeleteDownloadsByStatus(status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear downloads")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": deleted})
}

func (s *Server) handleRetryDownload(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.queries.UpdateDownloadStatus(id, models.DownloadStatusPending, nil, nil); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retry download")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "pending"})
}

// Providers

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	providers := s.providers.ListProviders()
	writeJSON(w, http.StatusOK, providers)
}

func (s *Server) handleListActivity(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	var offset int
	if o := r.URL.Query().Get("offset"); o != "" {
		fmt.Sscanf(o, "%d", &offset)
	}
	items, err := s.activityLog.List(limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activity")
		return
	}
	if items == nil {
		items = []models.ActivityLog{}
	}
	total, _ := s.activityLog.Count()
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": total,
	})
}

func (s *Server) handleClearCache(w http.ResponseWriter, r *http.Request) {
	s.cache.Clear()
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

func (s *Server) handleRelinkEntity(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req struct {
		ProviderID string `json:"provider_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	primary := s.providers.Primary()

	path := r.URL.Path
	var err2 error
	switch {
	case strings.Contains(path, "/relink/artist/"):
		err2 = s.queries.RelinkArtist(id, primary, req.ProviderID)
	case strings.Contains(path, "/relink/album/"):
		err2 = s.queries.RelinkAlbum(id, primary, req.ProviderID)
	case strings.Contains(path, "/relink/track/"):
		err2 = s.queries.RelinkTrack(id, primary, req.ProviderID)
	default:
		writeError(w, http.StatusBadRequest, "invalid relink target")
		return
	}

	if err2 != nil {
		writeError(w, http.StatusInternalServerError, "failed to relink")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "relinked"})
}

// Settings

var sensitiveSettings = map[string]bool{
	"navidrome_password": true,
	"slskd_api_key":     true,
}

const redactedPlaceholder = "••••••••"

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.queries.AllSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get settings")
		return
	}
	for k := range settings {
		if sensitiveSettings[k] {
			settings[k] = redactedPlaceholder
		}
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var settings map[string]string
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	for k, v := range settings {
		if sensitiveSettings[k] && v == redactedPlaceholder {
			continue
		}
		if err := s.queries.SetSetting(k, v); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save settings")
			return
		}
	}
	writeJSON(w, http.StatusOK, settings)
}

// Helpers

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func intPtrOrNil(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}
