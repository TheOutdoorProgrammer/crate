package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/models"
	"github.com/TheOutdoorProgrammer/crate/internal/provider"
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
		"version":           s.version,
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

	watchedAlbumIDs := []string{}
	existing, _ := s.queries.FindArtistByProvider(primary, id)
	if existing != nil {
		dbAlbums, _ := s.queries.ListAlbumsByArtist(existing.ID)
		for _, a := range dbAlbums {
			watchedAlbumIDs = append(watchedAlbumIDs, a.ProviderID)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":                artist.Id,
		"name":              artist.Name,
		"image_url":         artist.ImageUrl,
		"metadata":          artist.Metadata,
		"album_count":       len(albums.Albums),
		"albums":            albums.Albums,
		"watched_album_ids": watchedAlbumIDs,
		"artist_watched":    existing != nil && existing.Status == models.ArtistStatusWatched,
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

	existingAlbum, _ := s.queries.FindAlbumByProvider(primary, id)
	watchedTrackIDs := []string{}
	if existingAlbum != nil {
		tracks, _ := s.queries.ListTracksByAlbum(existingAlbum.ID)
		for _, t := range tracks {
			watchedTrackIDs = append(watchedTrackIDs, t.ProviderID)
		}
	}

	albumWatched := existingAlbum != nil && len(album.Tracks) > 0 && len(watchedTrackIDs) >= len(album.Tracks)

	var libraryID *int64
	if existingAlbum != nil {
		libraryID = &existingAlbum.ID
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":                album.Id,
		"title":             album.Title,
		"artist_name":       album.ArtistName,
		"year":              album.Year,
		"cover_url":         album.CoverUrl,
		"tracks":            album.Tracks,
		"metadata":          album.Metadata,
		"album_watched":     albumWatched,
		"watched_track_ids": watchedTrackIDs,
		"library_id":        libraryID,
	})
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

	existing, _ := s.queries.FindArtistByProvider(primary, providerID)

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
			if existingAlbum.Status != models.AlbumStatusIgnored {
				s.syncAlbumTracks(ctx, providerName, existingAlbum)
			}
			continue
		}
		s.saveAlbumFromProvider(ctx, providerName, artistID, pa)
	}
}

func (s *Server) syncAlbumTracks(ctx context.Context, providerName string, album *models.Album) {
	albumDetail, err := s.providers.GetAlbum(ctx, providerName, album.ProviderID)
	if err != nil {
		return
	}
	for _, pt := range albumDetail.Tracks {
		if _, err := s.queries.FindTrackByProvider(providerName, pt.Id); err == nil {
			continue
		}
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

func (s *Server) handleIgnoreAlbum(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.queries.UpdateAlbumStatus(id, models.AlbumStatusIgnored); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to ignore album")
		return
	}
	s.queries.UpdateTrackStatusByAlbum(id, models.TrackStatusWanted, models.TrackStatusIgnored)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
}

func (s *Server) handleUnignoreAlbum(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.queries.UpdateAlbumStatus(id, models.AlbumStatusWatched); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unignore album")
		return
	}
	s.queries.UpdateTrackStatusByAlbum(id, models.TrackStatusIgnored, models.TrackStatusWanted)
	writeJSON(w, http.StatusOK, map[string]string{"status": "watched"})
}

func (s *Server) handleIgnoreTrack(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.queries.UpdateTrackStatus(id, models.TrackStatusIgnored); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to ignore track")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
}

func (s *Server) handleUnignoreTrack(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.queries.UpdateTrackStatus(id, models.TrackStatusWanted); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unignore track")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "wanted"})
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

func (s *Server) handleStartManualSearch(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Query string `json:"query"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	searchID, query, err := s.downloader.StartManualSearch(r.Context(), id, req.Query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"search_id": searchID, "track_id": id, "query": query})
}

func (s *Server) handlePollManualSearch(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	searchID := chi.URLParam(r, "searchId")
	if searchID == "" {
		writeError(w, http.StatusBadRequest, "missing search id")
		return
	}
	resp, err := s.downloader.PollManualSearch(r.Context(), id, searchID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "poll failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDeleteManualSearch(w http.ResponseWriter, r *http.Request) {
	searchID := chi.URLParam(r, "searchId")
	if searchID == "" {
		writeError(w, http.StatusBadRequest, "missing search id")
		return
	}
	s.downloader.CleanupSearch(r.Context(), searchID)
	w.WriteHeader(http.StatusNoContent)
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
	dl, err := s.queries.GetDownload(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "download not found")
		return
	}
	s.downloader.CancelTransfer(r.Context(), dl)
	if err := s.queries.DeleteDownload(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete download")
		return
	}
	if dl.Status == models.DownloadStatusDownloading || dl.Status == models.DownloadStatusSearching || dl.Status == models.DownloadStatusPending {
		_ = s.queries.UpdateTrackStatus(dl.TrackID, models.TrackStatusWanted)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleClearDownloadsByStatus(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		writeError(w, http.StatusBadRequest, "status parameter required")
		return
	}
	allowed := map[string]bool{"failed": true, "complete": true, "pending": true, "downloading": true, "searching": true}
	if !allowed[status] {
		writeError(w, http.StatusBadRequest, "can only clear failed, complete, pending, downloading, or searching downloads")
		return
	}
	resetTrack := status == "downloading" || status == "searching" || status == "pending"
	if resetTrack {
		s.queries.ResetTrackStatusForDownloads(status)
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
}

var hiddenSettings = map[string]bool{
	"slskd_url":                  true,
	"slskd_api_key":              true,
	"library_path":               true,
	"scan_interval":              true,
	"download_format_preference": true,
	"min_bitrate":                true,
}

const redactedPlaceholder = "••••••••"

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.queries.AllSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get settings")
		return
	}
	for k := range settings {
		if hiddenSettings[k] {
			delete(settings, k)
		} else if sensitiveSettings[k] {
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
		if hiddenSettings[k] {
			continue
		}
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

// Blacklist & Cooldowns

func (s *Server) handleListBlacklist(w http.ResponseWriter, r *http.Request) {
	entries, err := s.queries.ListBlacklist()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list blacklist")
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleDeleteBlacklistEntry(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.queries.DeleteBlacklistEntry(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete blacklist entry")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleClearBlacklist(w http.ResponseWriter, r *http.Request) {
	if err := s.queries.ClearBlacklist(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear blacklist")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListCooldowns(w http.ResponseWriter, r *http.Request) {
	cooldowns, err := s.queries.ListActiveCooldowns()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list cooldowns")
		return
	}
	writeJSON(w, http.StatusOK, cooldowns)
}

func (s *Server) handleDeleteCooldown(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.queries.DeleteCooldown(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete cooldown")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleClearCooldowns(w http.ResponseWriter, r *http.Request) {
	if err := s.queries.ClearCooldowns(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear cooldowns")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Reject

func (s *Server) handleRejectTrack(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	track, err := s.queries.GetTrackWithMeta(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "track not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get track")
		return
	}

	if track.Status != models.TrackStatusOwned {
		writeError(w, http.StatusBadRequest, "track is not owned")
		return
	}

	deleteTrackFile(s.libraryDir, track)

	if track.DownloadedFrom != nil && track.DownloadedFilename != nil {
		_ = s.queries.BlacklistFile(*track.DownloadedFrom, *track.DownloadedFilename, "rejected by user")
		slog.Info("reject: blacklisted source", "username", *track.DownloadedFrom, "filename", *track.DownloadedFilename)
	}

	if err := s.queries.RejectTrack(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reject track")
		return
	}

	if err := s.queries.ReenqueueDownload(id); err != nil {
		slog.Error("reject: failed to re-enqueue", "track_id", id, "error", err)
	}

	s.activityLog.Record("track_rejected", "track", id,
		fmt.Sprintf("Rejected: %s - %s", track.ArtistName, track.Title))

	s.bgWork.Add(1)
	go func() {
		defer s.bgWork.Done()
		for _, n := range s.downloader.Notifiers() {
			n.TriggerScan(context.Background())
		}
	}()

	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

func (s *Server) handleRejectTrackByName(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Artist string `json:"artist"`
		Title  string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Artist == "" || req.Title == "" {
		writeError(w, http.StatusBadRequest, "artist and title are required")
		return
	}

	track, err := s.queries.FindOwnedTrackByName(req.Artist, req.Title)
	if err != nil {
		writeError(w, http.StatusNotFound, "no owned track found matching artist/title")
		return
	}

	deleteTrackFile(s.libraryDir, track)

	if track.DownloadedFrom != nil && track.DownloadedFilename != nil {
		_ = s.queries.BlacklistFile(*track.DownloadedFrom, *track.DownloadedFilename, "rejected by user")
	}

	if err := s.queries.RejectTrack(track.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reject track")
		return
	}

	if err := s.queries.ReenqueueDownload(track.ID); err != nil {
		slog.Error("reject: failed to re-enqueue", "track_id", track.ID, "error", err)
	}

	s.activityLog.Record("track_rejected", "track", track.ID,
		fmt.Sprintf("Rejected: %s - %s", track.ArtistName, track.Title))

	s.bgWork.Add(1)
	go func() {
		defer s.bgWork.Done()
		for _, n := range s.downloader.Notifiers() {
			n.TriggerScan(context.Background())
		}
	}()

	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

// Helpers

func deleteTrackFile(libraryDir string, track *models.Track) {
	if track.FilePath == nil || libraryDir == "" {
		return
	}
	fullPath := filepath.Join(libraryDir, *track.FilePath)
	cleaned := filepath.Clean(fullPath)
	if !strings.HasPrefix(cleaned, filepath.Clean(libraryDir)+string(filepath.Separator)) {
		slog.Error("reject: path escapes library dir", "path", cleaned)
		return
	}
	if err := os.Remove(cleaned); err != nil && !os.IsNotExist(err) { // #nosec G703 -- path validated above
		slog.Error("reject: failed to delete file", "path", cleaned, "error", err)
	} else {
		slog.Info("reject: deleted file", "path", cleaned)
	}
}

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
