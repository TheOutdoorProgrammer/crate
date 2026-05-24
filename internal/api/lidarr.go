package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOutdoorProgrammer/crate/internal/models"
	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

func (s *Server) mountLidarrRoutes(r chi.Router) {
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(lidarrLogger)
		r.Use(lidarrAuth)

		r.Get("/system/status", s.lidarrSystemStatus)

		r.Get("/qualityprofile", s.lidarrQualityProfiles)
		r.Get("/metadataprofile", s.lidarrMetadataProfiles)
		r.Get("/rootfolder", s.lidarrRootFolders)
		r.Get("/tag", s.lidarrTags)
		r.Get("/languageprofile", s.lidarrLanguageProfiles)

		r.Get("/search", s.lidarrSearch)

		r.Get("/artist", s.lidarrListArtists)
		r.Get("/artist/lookup", s.lidarrArtistLookup)
		r.Get("/artist/{id}", s.lidarrGetArtist)
		r.Post("/artist", s.lidarrAddArtist)
		r.Put("/artist/{id}", s.lidarrUpdateArtist)
		r.Delete("/artist/{id}", s.lidarrDeleteArtist)

		r.Get("/album", s.lidarrListAlbums)
		r.Get("/album/{id}", s.lidarrGetAlbum)
		r.Delete("/album/{id}", s.lidarrDeleteAlbum)
		r.Put("/album/monitor", s.lidarrMonitorAlbum)

		r.Get("/track", s.lidarrListTracks)

		r.Get("/health", s.lidarrHealth)
		r.Get("/diskspace", s.lidarrDiskspace)
		r.Get("/customfilter", s.lidarrCustomFilter)

		r.Get("/queue", s.lidarrQueue)
		r.Get("/queue/details", s.lidarrQueue)

		r.Get("/wanted/missing", s.lidarrWantedMissing)
		r.Get("/wanted/cutoff", s.lidarrWantedCutoff)

		r.Get("/calendar", s.lidarrCalendar)
		r.Get("/history", s.lidarrHistory)

		r.Get("/release", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, []any{}) })
		r.Post("/release", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, []any{}) })

		r.Post("/command", s.lidarrCommand)

		r.NotFound(lidarrCatchAll)
	})
}

func lidarrAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

func lidarrLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(strings.NewReader(string(body)))

		slog.Info("[LIDARR SHIM] request",
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"headers", fmt.Sprintf("%v", r.Header),
			"body", string(body),
		)
		next.ServeHTTP(w, r)
	})
}

func lidarrCatchAll(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	slog.Warn("[LIDARR SHIM] unhandled endpoint",
		"method", r.Method,
		"path", r.URL.Path,
		"query", r.URL.RawQuery,
		"body", string(body),
	)
	writeJSON(w, http.StatusOK, []any{})
}

// System

func (s *Server) lidarrSystemStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"appName":                "Crate",
		"instanceName":           "Crate",
		"version":                "2.8.1.4731",
		"buildTime":              s.startTime.Format(time.RFC3339),
		"isDebug":                false,
		"isProduction":           true,
		"isAdmin":                false,
		"isUserInteractive":      false,
		"startupPath":            "/app",
		"appData":                "/config",
		"osName":                 runtime.GOOS,
		"osVersion":              runtime.GOARCH,
		"isNetCore":              true,
		"isLinux":                runtime.GOOS == "linux",
		"isOsx":                  runtime.GOOS == "darwin",
		"isWindows":              runtime.GOOS == "windows",
		"isDocker":               false,
		"mode":                   "console",
		"branch":                 "main",
		"databaseType":           "sqLite",
		"databaseVersion":        "3.0.0",
		"authentication":         "none",
		"migrationVersion":       0,
		"urlBase":                "",
		"runtimeVersion":         runtime.Version(),
		"runtimeName":            "go",
		"startTime":              s.startTime.Format(time.RFC3339),
		"packageVersion":         "2.8.1.4731",
		"packageAuthor":          "crate",
		"packageUpdateMechanism": "docker",
	})
}

// Profiles

func (s *Server) lidarrQualityProfiles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []map[string]any{
		{
			"id":             1,
			"name":           "Crate Quality",
			"upgradeAllowed": true,
			"cutoff":         1,
			"items": []map[string]any{
				{"quality": map[string]any{"id": 1, "name": "Crate Quality"}, "allowed": true},
			},
		},
	})
}

func (s *Server) lidarrMetadataProfiles(w http.ResponseWriter, r *http.Request) {
	primary := s.providers.Primary()
	displayName := primary
	for _, p := range s.providers.ListProviders() {
		if p.Name == primary {
			displayName = p.DisplayName
			break
		}
	}

	writeJSON(w, http.StatusOK, []map[string]any{
		{
			"id":   1,
			"name": displayName,
			"primaryAlbumTypes": []map[string]any{
				{"albumType": map[string]any{"id": 0, "name": "Album"}, "allowed": true},
			},
			"secondaryAlbumTypes": []map[string]any{},
			"releaseStatuses":     []map[string]any{},
		},
	})
}

func (s *Server) lidarrRootFolders(w http.ResponseWriter, r *http.Request) {
	path := s.libraryPath()
	free, total := diskStats(path)
	writeJSON(w, http.StatusOK, []map[string]any{
		{
			"id":              1,
			"path":            path,
			"name":            "music",
			"accessible":      true,
			"freeSpace":       free,
			"totalSpace":      total,
			"unmappedFolders": []any{},
		},
	})
}

func (s *Server) lidarrTags(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []any{})
}

func (s *Server) lidarrLanguageProfiles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []map[string]any{
		{"id": 1, "name": "English"},
	})
}

// Search — returns SearchResource[]: {id, foreignId, artist}

func (s *Server) lidarrSearch(w http.ResponseWriter, r *http.Request) {
	term := r.URL.Query().Get("term")
	if term == "" {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	primary := s.providers.Primary()
	result, err := s.providers.Search(r.Context(), term, 25, 0)
	if err != nil {
		slog.Error("[LIDARR SHIM] search failed", "error", err)
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	var items []map[string]any
	for i, a := range result.Artists {
		existing, _ := s.queries.FindArtistByProvider(primary, a.Id)
		items = append(items, map[string]any{
			"id":        i + 1,
			"foreignId": a.Id,
			"artist":    searchArtistToLidarr(a.Id, a.Name, a.ImageUrl, existing),
		})
	}

	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, items)
}

// Artist lookup — returns flat array of ArtistResource

func (s *Server) lidarrArtistLookup(w http.ResponseWriter, r *http.Request) {
	term := r.URL.Query().Get("term")
	if term == "" {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	primary := s.providers.Primary()
	result, err := s.providers.Search(r.Context(), term, 25, 0)
	if err != nil {
		slog.Error("[LIDARR SHIM] search failed", "error", err)
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	var artists []map[string]any
	for _, a := range result.Artists {
		existing, _ := s.queries.FindArtistByProvider(primary, a.Id)
		artists = append(artists, searchArtistToLidarr(a.Id, a.Name, a.ImageUrl, existing))
	}

	if artists == nil {
		artists = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, artists)
}

func searchArtistToLidarr(providerID, name, imageURL string, existing *models.Artist) map[string]any {
	obj := map[string]any{
		"status":            "continuing",
		"ended":             false,
		"artistName":        name,
		"foreignArtistId":   providerID,
		"tadbId":            0,
		"discogsId":         0,
		"overview":          "",
		"artistType":        "Artist",
		"disambiguation":    "",
		"links":             []any{},
		"images":            lidarrImages(imageURL),
		"remotePoster":      imageURL,
		"qualityProfileId":  1,
		"metadataProfileId": 1,
		"monitored":         existing != nil,
		"monitorNewItems":   lidarrMonitorNewItems(existing),
		"folder":            name,
		"genres":            []string{},
		"tags":              []int{},
		"added":             "0001-01-01T00:00:00Z",
		"ratings":           map[string]any{"votes": 0, "value": 0.0},
		"nextAlbum":         nil,
		"lastAlbum":         nil,
	}
	if existing != nil {
		obj["id"] = int(existing.ID)
		obj["added"] = existing.CreatedAt
	}
	return obj
}

// Artist CRUD

func (s *Server) lidarrListArtists(w http.ResponseWriter, r *http.Request) {
	artists, err := s.queries.ListArtists()
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	var result []map[string]any
	for _, a := range artists {
		result = append(result, s.artistToLidarr(&a))
	}
	if result == nil {
		result = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) lidarrGetArtist(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	artist, err := s.queries.GetArtist(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "artist not found")
		return
	}

	albums, _ := s.queries.ListAlbumsByArtist(id)
	for i := range albums {
		tracks, _ := s.queries.ListTracksByAlbum(albums[i].ID)
		albums[i].Tracks = tracks
	}
	artist.Albums = albums

	writeJSON(w, http.StatusOK, s.artistToLidarr(artist))
}

func (s *Server) lidarrAddArtist(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	slog.Info("[LIDARR SHIM] add artist request", "body", req)

	foreignID, _ := req["foreignArtistId"].(string)
	if foreignID == "" {
		writeError(w, http.StatusBadRequest, "foreignArtistId required")
		return
	}

	artistName, _ := req["artistName"].(string)

	monitor := "all"
	searchOnAdd := false
	if addOpts, ok := req["addOptions"].(map[string]any); ok {
		if m, ok := addOpts["monitor"].(string); ok {
			monitor = m
		}
		if s, ok := addOpts["searchForMissingAlbums"].(bool); ok {
			searchOnAdd = s
		}
	}

	primary := s.providers.Primary()
	existing, _ := s.queries.FindArtistByProvider(primary, foreignID)
	if existing != nil {
		writeJSON(w, http.StatusOK, s.artistToLidarr(existing))
		return
	}

	artistDetail, err := s.providers.GetArtist(r.Context(), primary, foreignID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch artist")
		return
	}
	if artistName == "" {
		artistName = artistDetail.Name
	}

	albumList, err := s.providers.GetArtistAlbums(r.Context(), primary, foreignID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch artist albums")
		return
	}

	imgURL := artistDetail.ImageUrl
	status := models.ArtistStatusWatched
	if monitor == "latest" {
		status = models.ArtistStatusPartial
	}
	artist := &models.Artist{
		Name:             artistName,
		Provider:         primary,
		ProviderID:       foreignID,
		ImageURL:         strPtrOrNil(imgURL),
		Status:           status,
		WatchNewReleases: true,
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	artist.WatchNewReleasesSince = &ts

	if err := s.queries.CreateArtist(artist); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save artist")
		return
	}

	albums := lidarrFilterAlbumsByMonitor(albumList.Albums, monitor)
	slog.Info("[LIDARR SHIM] monitoring strategy", "monitor", monitor, "total_albums", len(albumList.Albums), "saving", len(albums), "searchOnAdd", searchOnAdd)

	artistID := artist.ID
	s.bgWork.Add(1)
	go func() {
		defer s.bgWork.Done()
		s.saveAlbumsFromProvider(primary, artistID, albums)
		if searchOnAdd {
			s.lidarrQueueArtistDownloads(artistID)
		}
	}()

	writeJSON(w, http.StatusCreated, s.artistToLidarr(artist))
}

func lidarrFilterAlbumsByMonitor(albums []*pb.AlbumSummary, monitor string) []*pb.AlbumSummary {
	if monitor == "all" || len(albums) == 0 {
		return albums
	}

	if monitor == "latest" {
		sort.Slice(albums, func(i, j int) bool {
			return albums[i].Year > albums[j].Year
		})
		return albums[:1]
	}

	return albums
}

func (s *Server) lidarrQueueArtistDownloads(artistID int64) {
	tracks, err := s.queries.ListWantedTracksByArtist(artistID)
	if err != nil || len(tracks) == 0 {
		return
	}
	ids := make([]int64, len(tracks))
	for i, t := range tracks {
		ids[i] = t.ID
	}
	queued, _ := s.queries.EnqueueDownloadBatch(ids)
	slog.Info("[LIDARR SHIM] auto-queued tracks", "artistId", artistID, "queued", queued)
}

func (s *Server) lidarrUpdateArtist(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	artist, err := s.queries.GetArtist(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "artist not found")
		return
	}
	writeJSON(w, http.StatusAccepted, s.artistToLidarr(artist))
}

func (s *Server) lidarrDeleteArtist(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.queries.DeleteArtist(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete artist")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

// Albums

func (s *Server) lidarrListAlbums(w http.ResponseWriter, r *http.Request) {
	artistIDStr := r.URL.Query().Get("artistId")
	var albums []models.Album

	if artistIDStr != "" {
		artistID, err := strconv.ParseInt(artistIDStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		albums, _ = s.queries.ListAlbumsByArtist(artistID)
	}

	var result []map[string]any
	for _, a := range albums {
		tracks, _ := s.queries.ListTracksByAlbum(a.ID)
		a.Tracks = tracks
		result = append(result, albumToLidarr(&a))
	}
	if result == nil {
		result = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) lidarrGetAlbum(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	album, err := s.queries.GetAlbum(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "album not found")
		return
	}
	tracks, _ := s.queries.ListTracksByAlbum(id)
	album.Tracks = tracks

	writeJSON(w, http.StatusOK, albumToLidarr(album))
}

func (s *Server) lidarrDeleteAlbum(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.queries.DeleteAlbum(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete album")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) lidarrMonitorAlbum(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AlbumIDs  []int64 `json:"albumIds"`
		Monitored bool    `json:"monitored"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var updated []map[string]any
	for _, id := range req.AlbumIDs {
		if req.Monitored {
			s.queries.UpdateAlbumStatus(id, models.AlbumStatusWatched)
			s.queries.UpdateTrackStatusByAlbum(id, models.TrackStatusIgnored, models.TrackStatusWanted)
		} else {
			s.queries.UpdateAlbumStatus(id, models.AlbumStatusIgnored)
			s.queries.UpdateTrackStatusByAlbum(id, models.TrackStatusWanted, models.TrackStatusIgnored)
		}
		album, err := s.queries.GetAlbum(id)
		if err != nil {
			continue
		}
		tracks, _ := s.queries.ListTracksByAlbum(id)
		album.Tracks = tracks
		updated = append(updated, albumToLidarr(album))
	}
	if updated == nil {
		updated = []map[string]any{}
	}
	writeJSON(w, http.StatusAccepted, updated)
}

// Tracks

func (s *Server) lidarrListTracks(w http.ResponseWriter, r *http.Request) {
	albumIDStr := r.URL.Query().Get("albumId")
	if albumIDStr == "" {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	albumID, err := strconv.ParseInt(albumIDStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	tracks, err := s.queries.ListTracksByAlbum(albumID)
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	album, _ := s.queries.GetAlbum(albumID)

	var result []map[string]any
	for _, t := range tracks {
		artistName := t.ArtistName
		if artistName == "" && album != nil {
			artistName = album.ArtistName
		}
		result = append(result, map[string]any{
			"id":                    int(t.ID),
			"albumId":              int(t.AlbumID),
			"artistId":             0,
			"foreignTrackId":       t.ProviderID,
			"foreignRecordingId":   t.ProviderID,
			"trackNumber":         fmt.Sprintf("%d", t.TrackNumber),
			"absoluteTrackNumber": t.TrackNumber,
			"title":               t.Title,
			"duration":            t.DurationMs,
			"mediumNumber":        t.DiscNumber,
			"hasFile":             t.Status == models.TrackStatusOwned,
			"monitored":           t.Status != models.TrackStatusIgnored,
			"ratings":             map[string]any{"votes": 0, "value": 0.0},
			"artist": map[string]any{
				"artistName": artistName,
			},
		})
	}
	if result == nil {
		result = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, result)
}

// Health / Diskspace / CustomFilter

func (s *Server) lidarrHealth(w http.ResponseWriter, r *http.Request) {
	var issues []map[string]any
	for _, p := range s.providers.ListProviders() {
		if !p.Healthy {
			issues = append(issues, map[string]any{
				"source":  "ProviderCheck",
				"type":    "warning",
				"message": fmt.Sprintf("Provider %q is unreachable", p.DisplayName),
				"wikiUrl": "",
			})
		}
	}
	if issues == nil {
		issues = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, issues)
}

func (s *Server) lidarrDiskspace(w http.ResponseWriter, r *http.Request) {
	path := s.libraryPath()
	free, total := diskStats(path)
	writeJSON(w, http.StatusOK, []map[string]any{
		{
			"path":       path,
			"label":      "music",
			"freeSpace":  free,
			"totalSpace": total,
		},
	})
}

func (s *Server) lidarrCustomFilter(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []any{})
}

// Queue

func (s *Server) lidarrQueue(w http.ResponseWriter, r *http.Request) {
	downloads, _ := s.queries.ListDownloadsWithTrack("")
	var records []map[string]any
	for _, d := range downloads {
		if d.Status == models.DownloadStatusComplete || d.Status == models.DownloadStatusFailed {
			continue
		}

		qualityName := "Unknown"
		if d.Track != nil && d.Track.DownloadFormat != nil {
			qualityName = lidarrFormatLabel(*d.Track.DownloadFormat, d.Track.DownloadBitrate)
		}

		record := map[string]any{
			"id":                     int(d.ID),
			"artistId":              0,
			"albumId":               0,
			"title":                 "",
			"status":                lidarrQueueStatus(d.Status),
			"trackedDownloadStatus": 0,
			"trackedDownloadState":  lidarrDownloadState(d.Status),
			"statusMessages":        []any{},
			"downloadId":           fmt.Sprintf("crate-%d", d.ID),
			"protocol":             "peer",
			"downloadClient":       "slskd",
			"size":                 0.0,
			"sizeleft":             0.0,
			"timeleft":             "00:00:00",
			"quality": map[string]any{
				"quality":  map[string]any{"id": 1, "name": qualityName},
				"revision": map[string]any{"version": 1, "real": 0, "isRepack": false},
			},
			"customFormats":     []any{},
			"customFormatScore": 0,
			"indexer":           "",
			"outputPath":        "",
			"trackFileCount":    0,
			"trackHasFileCount": 0,
			"downloadForced":    false,
		}
		if d.Track != nil {
			record["title"] = fmt.Sprintf("%s - %s", d.Track.ArtistName, d.Track.Title)
			record["artistId"] = 0
			record["albumId"] = int(d.Track.AlbumID)
		}
		records = append(records, record)
	}
	if records == nil {
		records = []map[string]any{}
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"page":          page,
		"pageSize":      len(records),
		"sortKey":       "timeleft",
		"sortDirection": "ascending",
		"totalRecords":  len(records),
		"records":       records,
	})
}

// Wanted

func (s *Server) lidarrWantedMissing(w http.ResponseWriter, r *http.Request) {
	tracks, _ := s.queries.ListWantedTracks()

	var records []map[string]any
	for _, t := range tracks {
		records = append(records, map[string]any{
			"id":         int(t.ID),
			"artistId":   0,
			"albumId":    int(t.AlbumID),
			"title":      t.Title,
			"artistName": t.ArtistName,
			"albumTitle": t.AlbumTitle,
			"monitored":  true,
		})
	}
	if records == nil {
		records = []map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"page":         1,
		"pageSize":     len(records),
		"totalRecords": len(records),
		"records":      records,
	})
}

func (s *Server) lidarrWantedCutoff(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"page":         1,
		"pageSize":     0,
		"totalRecords": 0,
		"records":      []any{},
	})
}

// Calendar / History

func (s *Server) lidarrCalendar(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []any{})
}

func (s *Server) lidarrHistory(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"page":         1,
		"pageSize":     0,
		"totalRecords": 0,
		"records":      []any{},
	})
}

// Command

func (s *Server) lidarrCommand(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	json.NewDecoder(r.Body).Decode(&req)
	slog.Info("[LIDARR SHIM] command received", "body", req)

	name, _ := req["name"].(string)

	switch name {
	case "ArtistSearch":
		if id, ok := lidarrCommandInt(req, "artistId"); ok {
			s.bgWork.Add(1)
			go func() {
				defer s.bgWork.Done()
				s.lidarrQueueArtistDownloads(id)
			}()
		}
	case "AlbumSearch":
		if ids := lidarrCommandIntArray(req, "albumIds"); len(ids) > 0 {
			s.bgWork.Add(1)
			go func() {
				defer s.bgWork.Done()
				for _, id := range ids {
					s.lidarrQueueAlbumDownloads(id)
				}
			}()
		} else if id, ok := lidarrCommandInt(req, "albumId"); ok {
			s.bgWork.Add(1)
			go func() {
				defer s.bgWork.Done()
				s.lidarrQueueAlbumDownloads(id)
			}()
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":      1,
		"name":    name,
		"status":  "completed",
		"started": time.Now().UTC().Format(time.RFC3339),
		"ended":   time.Now().UTC().Format(time.RFC3339),
	})
}

func lidarrCommandInt(req map[string]any, key string) (int64, bool) {
	v, ok := req[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}

func lidarrCommandIntArray(req map[string]any, key string) []int64 {
	v, ok := req[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var ids []int64
	for _, item := range arr {
		if n, ok := item.(float64); ok {
			ids = append(ids, int64(n))
		}
	}
	return ids
}

func (s *Server) lidarrQueueAlbumDownloads(albumID int64) {
	tracks, err := s.queries.ListWantedTracksByAlbum(albumID)
	if err != nil || len(tracks) == 0 {
		return
	}
	ids := make([]int64, len(tracks))
	for i, t := range tracks {
		ids[i] = t.ID
	}
	queued, _ := s.queries.EnqueueDownloadBatch(ids)
	slog.Info("[LIDARR SHIM] auto-queued album tracks", "albumId", albumID, "queued", queued)
}

// Release (interactive search)

// Translation helpers

func (s *Server) artistToLidarr(a *models.Artist) map[string]any {
	var totalTracks, ownedTracks, albumCount int

	if len(a.Albums) > 0 {
		albumCount = len(a.Albums)
		for _, al := range a.Albums {
			totalTracks += len(al.Tracks)
			for _, t := range al.Tracks {
				if t.Status == models.TrackStatusOwned {
					ownedTracks++
				}
			}
		}
	} else if a.TotalTracks > 0 {
		totalTracks = a.TotalTracks
		ownedTracks = a.OwnedTracks
		albums, _ := s.queries.ListAlbumsByArtist(a.ID)
		albumCount = len(albums)
	} else {
		albums, _ := s.queries.ListAlbumsByArtist(a.ID)
		albumCount = len(albums)
		for _, al := range albums {
			tracks, _ := s.queries.ListTracksByAlbum(al.ID)
			totalTracks += len(tracks)
			for _, t := range tracks {
				if t.Status == models.TrackStatusOwned {
					ownedTracks++
				}
			}
		}
	}

	pct := float64(0)
	if totalTracks > 0 {
		pct = float64(ownedTracks) / float64(totalTracks) * 100
	}

	var imgURL string
	if a.ImageURL != nil {
		imgURL = *a.ImageURL
	}

	return map[string]any{
		"id":                int(a.ID),
		"status":            "continuing",
		"ended":             false,
		"artistName":        a.Name,
		"foreignArtistId":   a.ProviderID,
		"tadbId":            0,
		"discogsId":         0,
		"overview":          "",
		"artistType":        "Artist",
		"disambiguation":    "",
		"links":             []any{},
		"images":            lidarrImages(imgURL),
		"remotePoster":      imgURL,
		"path":              fmt.Sprintf("/music/%s", a.Name),
		"qualityProfileId":  1,
		"metadataProfileId": 1,
		"monitored":         true,
		"monitorNewItems":   lidarrMonitorNewItems(a),
		"rootFolderPath":    s.libraryPath(),
		"folder":            a.Name,
		"genres":            []string{},
		"cleanName":         strings.ToLower(a.Name),
		"sortName":          strings.ToLower(a.Name),
		"tags":              []int{},
		"added":             a.CreatedAt,
		"ratings":           map[string]any{"votes": 0, "value": 0.0},
		"nextAlbum":         nil,
		"lastAlbum":         nil,
		"statistics": map[string]any{
			"albumCount":      albumCount,
			"trackFileCount":  ownedTracks,
			"trackCount":      totalTracks,
			"totalTrackCount": totalTracks,
			"sizeOnDisk":      0,
			"percentOfTracks": pct,
		},
	}
}

func albumToLidarr(a *models.Album) map[string]any {
	totalTracks := len(a.Tracks)
	ownedTracks := 0
	for _, t := range a.Tracks {
		if t.Status == models.TrackStatusOwned {
			ownedTracks++
		}
	}

	pct := float64(0)
	if totalTracks > 0 {
		pct = float64(ownedTracks) / float64(totalTracks) * 100
	}

	year := 0
	if a.Year != nil {
		year = *a.Year
	}

	var coverURL string
	if a.CoverURL != nil {
		coverURL = *a.CoverURL
	}

	var releaseDate *string
	if year > 0 {
		rd := fmt.Sprintf("%d-01-01T00:00:00Z", year)
		releaseDate = &rd
	}

	return map[string]any{
		"id":              int(a.ID),
		"title":           a.Title,
		"disambiguation":  "",
		"overview":        "",
		"artistId":        int(a.ArtistID),
		"foreignAlbumId":  a.ProviderID,
		"monitored":       a.Status == models.AlbumStatusWatched,
		"anyReleaseOk":    true,
		"profileId":       1,
		"duration":        0,
		"albumType":       a.RecordType,
		"secondaryTypes":  []string{},
		"mediumCount":     1,
		"ratings":         map[string]any{"votes": 0, "value": 0.0},
		"releaseDate":     releaseDate,
		"releases":        []any{},
		"genres":          []string{},
		"media":           []any{},
		"images":          lidarrImages(coverURL),
		"links":           []any{},
		"added":           a.CreatedAt,
		"remoteCover":     coverURL,
		"grabbed":         false,
		"statistics": map[string]any{
			"trackFileCount":  ownedTracks,
			"trackCount":      totalTracks,
			"totalTrackCount": totalTracks,
			"sizeOnDisk":      0,
			"percentOfTracks": pct,
		},
	}
}

func lidarrImages(url string) []map[string]any {
	if url == "" {
		return []map[string]any{}
	}
	return []map[string]any{
		{"coverType": "poster", "url": url, "extension": ".jpg"},
		{"coverType": "banner", "url": url, "extension": ".jpg"},
		{"coverType": "fanart", "url": url, "extension": ".jpg"},
	}
}

func (s *Server) libraryPath() string {
	return s.libraryDir
}

func diskStats(path string) (free, total uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 100_000_000_000, 500_000_000_000
	}
	return stat.Bavail * uint64(stat.Bsize), stat.Blocks * uint64(stat.Bsize)
}

func lidarrMonitorNewItems(a *models.Artist) string {
	if a == nil {
		return "none"
	}
	switch a.Status {
	case models.ArtistStatusWatched:
		return "all"
	case models.ArtistStatusPartial:
		return "latest"
	default:
		return "none"
	}
}

func lidarrFormatLabel(format string, bitrate *int) string {
	switch strings.ToLower(format) {
	case "flac":
		return "FLAC"
	case "mp3":
		if bitrate != nil && *bitrate > 0 {
			return fmt.Sprintf("MP3-%d", *bitrate)
		}
		return "MP3"
	default:
		return strings.ToUpper(format)
	}
}

func lidarrQueueStatus(s models.DownloadStatus) string {
	switch s {
	case models.DownloadStatusPending:
		return "queued"
	case models.DownloadStatusSearching, models.DownloadStatusDownloading:
		return "downloading"
	case models.DownloadStatusOrganizing:
		return "downloading"
	default:
		return "unknown"
	}
}

func lidarrDownloadState(s models.DownloadStatus) int {
	switch s {
	case models.DownloadStatusPending:
		return 4
	case models.DownloadStatusSearching, models.DownloadStatusDownloading:
		return 0
	case models.DownloadStatusOrganizing:
		return 5
	default:
		return 0
	}
}
