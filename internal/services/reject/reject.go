// Package reject performs track rejection — the shared logic behind the HTTP
// reject endpoints and the Music Assistant reject-playlist watcher: delete the
// bad file, blacklist its source so it isn't re-grabbed, reset the track to
// wanted, and re-enqueue a download. It deliberately does not trigger a library
// rescan; callers notify however they need to (the API fans out to notifiers,
// the MA watcher triggers music/sync).
package reject

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/TheOutdoorProgrammer/crate/internal/activity"
	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/library"
	"github.com/TheOutdoorProgrammer/crate/internal/models"
)

var (
	// ErrNotFound means no track has the given id.
	ErrNotFound = errors.New("track not found")
	// ErrNotOwned means the track exists but isn't owned — nothing to reject.
	ErrNotOwned = errors.New("track is not owned")
)

// Service rejects tracks. It is stateless over the database and safe for
// concurrent use.
type Service struct {
	queries    *db.Queries
	libraryDir string
	activity   *activity.Log
}

// NewService builds a reject service. activity may be nil.
func NewService(queries *db.Queries, libraryDir string, act *activity.Log) *Service {
	return &Service{queries: queries, libraryDir: libraryDir, activity: act}
}

// Reject rejects the track with the given id. Returns ErrNotFound or
// ErrNotOwned for those cases.
func (s *Service) Reject(id int64) error {
	track, err := s.queries.GetTrackWithMeta(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("get track: %w", err)
	}
	return s.RejectTrack(track)
}

// RejectTrack rejects an already-loaded track, for callers that found it by
// name or path and want to skip the id lookup.
func (s *Service) RejectTrack(track *models.Track) error {
	if track.Status != models.TrackStatusOwned {
		return ErrNotOwned
	}
	if track.FilePath != nil {
		library.DeleteFile(s.libraryDir, *track.FilePath)
	}
	if track.DownloadedFrom != nil && track.DownloadedFilename != nil {
		_ = s.queries.BlacklistFile(*track.DownloadedFrom, *track.DownloadedFilename, "rejected by user")
		slog.Info("reject: blacklisted source", "username", *track.DownloadedFrom, "filename", *track.DownloadedFilename)
	}
	if err := s.queries.RejectTrack(track.ID); err != nil {
		return fmt.Errorf("reset track to wanted: %w", err)
	}
	if err := s.queries.ReenqueueDownload(track.ID); err != nil {
		slog.Error("reject: failed to re-enqueue", "track_id", track.ID, "error", err)
	}
	if s.activity != nil {
		s.activity.Record("track_rejected", "track", track.ID,
			fmt.Sprintf("Rejected: %s - %s", track.ArtistName, track.Title))
	}
	return nil
}
