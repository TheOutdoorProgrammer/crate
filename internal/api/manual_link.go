package api

import (
	"encoding/json"
	"net/http"

	"github.com/TheOutdoorProgrammer/crate/internal/models"
	"github.com/TheOutdoorProgrammer/crate/internal/provider"
)

// Manual link — the escape hatch when the automatic reconcile can't match an
// imported album/track to the provider (title drift, deluxe editions, tagging
// quirks). See docs/adr/0007-reconcile-local-import.md.
//
// After reconcile the provider's whole discography is already present, so
// "linking" a leftover local entity means *merging* it into the existing
// provider row — not re-stamping a provider id, which would collide on the
// unique (provider, provider_id) index. Owned files are carried over; the empty
// local shell is removed. Nothing else is touched.

// handleLinkAlbum merges a local album into another album by the same artist
// that's already linked to a provider: each owned local track claims the
// matching wanted track (by title, disc/track number as tiebreak); a track the
// provider doesn't list is moved over as-is (owned + local extra); the emptied
// local album is deleted.
func (s *Server) handleLinkAlbum(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		TargetAlbumID int64 `json:"target_album_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	src, err := s.queries.GetAlbum(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "album not found")
		return
	}
	if src.Provider != provider.LocalProvider {
		writeError(w, http.StatusBadRequest, "only a local album can be linked")
		return
	}
	dst, err := s.queries.GetAlbum(req.TargetAlbumID)
	if err != nil {
		writeError(w, http.StatusNotFound, "target album not found")
		return
	}
	if dst.ID == src.ID || dst.ArtistID != src.ArtistID {
		writeError(w, http.StatusBadRequest, "target must be another album by the same artist")
		return
	}
	if dst.Provider == provider.LocalProvider {
		writeError(w, http.StatusBadRequest, "target album is not linked to a provider")
		return
	}

	if err := s.mergeAlbum(src, dst); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link album")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "linked", "merged_into": dst.ID})
}

func (s *Server) mergeAlbum(src, dst *models.Album) error {
	srcTracks, err := s.queries.ListTracksByAlbum(src.ID)
	if err != nil {
		return err
	}
	dstTracks, err := s.queries.ListTracksByAlbum(dst.ID)
	if err != nil {
		return err
	}

	used := make(map[int64]bool)
	for i := range srcTracks {
		t := &srcTracks[i]
		if match := findWantedMatch(dstTracks, used, t); match != nil && t.FilePath != nil {
			used[match.ID] = true
			if err := s.queries.ClaimTrackFile(match.ID, *t.FilePath, strVal(t.DownloadFormat), intVal(t.DownloadBitrate), t.DurationMs, t.MBRecordingID); err != nil {
				return err
			}
			if err := s.queries.DeleteTrack(t.ID); err != nil {
				return err
			}
			continue
		}
		// No provider match (or no file) — keep the track, move it under dst.
		if err := s.queries.UpdateTrackAlbum(t.ID, dst.ID); err != nil {
			return err
		}
	}
	return s.queries.DeleteAlbum(src.ID)
}

// handleLinkTrack claims a wanted provider track using an owned local track's
// file — "this local file IS that track" — then removes the local duplicate.
func (s *Server) handleLinkTrack(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		TargetTrackID int64 `json:"target_track_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	src, err := s.queries.GetTrack(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "track not found")
		return
	}
	if src.Provider != provider.LocalProvider || src.FilePath == nil {
		writeError(w, http.StatusBadRequest, "only an owned local track can be linked")
		return
	}
	dst, err := s.queries.GetTrack(req.TargetTrackID)
	if err != nil {
		writeError(w, http.StatusNotFound, "target track not found")
		return
	}
	if dst.AlbumID != src.AlbumID {
		writeError(w, http.StatusBadRequest, "target track must be in the same album")
		return
	}
	if dst.Status != models.TrackStatusWanted {
		writeError(w, http.StatusBadRequest, "target track already has a file")
		return
	}

	if err := s.queries.ClaimTrackFile(dst.ID, *src.FilePath, strVal(src.DownloadFormat), intVal(src.DownloadBitrate), src.DurationMs, src.MBRecordingID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link track")
		return
	}
	if err := s.queries.DeleteTrack(src.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove local duplicate")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "linked", "claimed": dst.ID})
}

// findWantedMatch returns the wanted dst track that best matches src: an exact
// title-fold match, preferring one whose disc/track number also aligns. Mirrors
// the reconcile track matcher, but over DB rows and scoped to wanted tracks.
func findWantedMatch(dst []models.Track, used map[int64]bool, src *models.Track) *models.Track {
	var titleOnly *models.Track
	for i := range dst {
		d := &dst[i]
		if used[d.ID] || d.Status != models.TrackStatusWanted || !foldEqual(d.Title, src.Title) {
			continue
		}
		if d.TrackNumber == src.TrackNumber && d.DiscNumber == src.DiscNumber {
			return d
		}
		if titleOnly == nil {
			titleOnly = d
		}
	}
	return titleOnly
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func intVal(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}
