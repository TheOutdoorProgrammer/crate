package api

import (
	"context"
	"log/slog"
	"strings"

	"github.com/TheOutdoorProgrammer/crate/internal/models"
	"github.com/TheOutdoorProgrammer/crate/internal/provider"
	pb "github.com/TheOutdoorProgrammer/crate/proto/provider"
)

// Reconciling a promoted local import. See docs/adr/0007-reconcile-local-import.md.
//
// When an imported (local-provider) artist is relinked to a real provider, we
// fetch that provider's discography and fold the artist's existing local
// albums/tracks into it *in place* — matching conservatively, relinking matches
// so owned files are preserved, and creating only the genuine remainder as
// wanted gaps. Nothing is ever deleted; local children that match nothing are
// left owned + local for the user to link manually. Only local-provider
// entities are ever fuzzed — provider-anchored rows are never touched.

// reconcileLocalArtist reconciles one artist's local albums against the
// provider discography. Safe to re-run: already-linked albums are recognised by
// provider id and only gain newly-listed tracks, so it never duplicates.
func (s *Server) reconcileLocalArtist(providerName string, artistID int64, artistProviderID string) {
	ctx := context.Background()

	albumList, err := s.providers.GetArtistAlbums(ctx, providerName, artistProviderID)
	if err != nil {
		slog.Error("reconcile: fetch discography", "artist_id", artistID, "provider", providerName, "error", err)
		return
	}

	existing, err := s.queries.ListAlbumsByArtist(artistID)
	if err != nil {
		slog.Error("reconcile: list albums", "artist_id", artistID, "error", err)
		return
	}

	// Partition the artist's current albums: those already on the target
	// provider (recognised by id, skip-or-sync) and the local ones we may fold in.
	linked := make(map[string]*models.Album)
	var locals []*models.Album
	for i := range existing {
		a := &existing[i]
		switch a.Provider {
		case providerName:
			linked[a.ProviderID] = a
		case provider.LocalProvider:
			locals = append(locals, a)
		}
	}
	used := make(map[int64]bool)

	for _, pa := range albumList.Albums {
		if a, ok := linked[pa.Id]; ok {
			// Already ours on this provider — fill any tracks it's newly listing.
			s.reconcileAlbumTracks(ctx, providerName, a.ID, pa.Id)
			continue
		}
		if match := matchLocalAlbum(locals, used, pa); match != nil {
			used[match.ID] = true
			if err := s.queries.RelinkAlbum(match.ID, providerName, pa.Id); err != nil {
				slog.Error("reconcile: relink album", "album_id", match.ID, "error", err)
				continue
			}
			s.reconcileAlbumTracks(ctx, providerName, match.ID, pa.Id)
			continue
		}
		// A genuine gap: create the album with every track wanted.
		s.saveAlbumFromProvider(ctx, providerName, artistID, pa)
	}

	slog.Info("reconcile: artist done", "artist_id", artistID, "provider", providerName,
		"provider_albums", len(albumList.Albums), "local_albums", len(locals), "albums_matched", len(used))
}

// reconcileAlbumTracks reconciles one album's tracks against the provider's
// track list: local tracks matching a provider track by title (album-scoped,
// disc/track number as tiebreak) are relinked in place so the owned file is
// kept; provider tracks with no local match are created as wanted; local tracks
// matching nothing stay owned + local. With no local tracks present it
// degenerates to "create the missing wanted tracks", matching the watch path.
func (s *Server) reconcileAlbumTracks(ctx context.Context, providerName string, albumID int64, albumProviderID string) {
	detail, err := s.providers.GetAlbum(ctx, providerName, albumProviderID)
	if err != nil {
		slog.Error("reconcile: fetch album", "album_id", albumID, "provider_id", albumProviderID, "error", err)
		return
	}

	existing, err := s.queries.ListTracksByAlbum(albumID)
	if err != nil {
		slog.Error("reconcile: list tracks", "album_id", albumID, "error", err)
		return
	}

	linked := make(map[string]bool)
	var locals []*models.Track
	for i := range existing {
		t := &existing[i]
		switch t.Provider {
		case providerName:
			linked[t.ProviderID] = true
		case provider.LocalProvider:
			locals = append(locals, t)
		}
	}
	used := make(map[int64]bool)

	for _, pt := range detail.Tracks {
		if linked[pt.Id] {
			continue
		}
		if match := matchLocalTrack(locals, used, pt); match != nil {
			used[match.ID] = true
			if err := s.queries.RelinkTrack(match.ID, providerName, pt.Id); err != nil {
				slog.Error("reconcile: relink track", "track_id", match.ID, "error", err)
			}
			continue
		}
		if err := s.queries.CreateTrack(&models.Track{
			AlbumID:     albumID,
			Title:       pt.Title,
			TrackNumber: int(pt.TrackNumber),
			DiscNumber:  int(pt.DiscNumber),
			DurationMs:  int(pt.DurationMs),
			Provider:    providerName,
			ProviderID:  pt.Id,
			Status:      models.TrackStatusWanted,
		}); err != nil {
			slog.Error("reconcile: create wanted track", "album_id", albumID, "error", err)
		}
	}
}

// albumHasLocalTracks reports whether an album still contains any local-provider
// tracks. This is the trigger for reconciling on a manual album relink: it
// covers both promotion (a freshly-linked local album) and correction (an album
// fuzzy-matched to the wrong release-group, whose tracks never matched and so
// stayed local) — and naturally no-ops once everything is linked.
func (s *Server) albumHasLocalTracks(albumID int64) bool {
	tracks, err := s.queries.ListTracksByAlbum(albumID)
	if err != nil {
		return false
	}
	for _, t := range tracks {
		if t.Provider == provider.LocalProvider {
			return true
		}
	}
	return false
}

// matchLocalAlbum returns the first unused local album whose title folds equal
// to pa and whose year is compatible (equal, or unknown on either side). The
// year guard stops a same-titled but distinct release (a re-record, a reissue
// under the same name) from being merged.
func matchLocalAlbum(locals []*models.Album, used map[int64]bool, pa *pb.AlbumSummary) *models.Album {
	for _, a := range locals {
		if used[a.ID] || !foldEqual(a.Title, pa.Title) {
			continue
		}
		if a.Year != nil && pa.Year != 0 && *a.Year != int(pa.Year) {
			continue
		}
		return a
	}
	return nil
}

// matchLocalTrack returns the best unused local track for pt: an exact
// title-fold match, preferring one whose disc/track number also matches when
// several share a title. There is deliberately no number-only fallback — an
// unmatched local track is left owned + local for the user to link manually,
// never force-matched onto a title it doesn't share.
func matchLocalTrack(locals []*models.Track, used map[int64]bool, pt *pb.TrackInfo) *models.Track {
	var titleOnly *models.Track
	for _, t := range locals {
		if used[t.ID] || !foldEqual(t.Title, pt.Title) {
			continue
		}
		if t.TrackNumber == int(pt.TrackNumber) && t.DiscNumber == int(pt.DiscNumber) {
			return t
		}
		if titleOnly == nil {
			titleOnly = t
		}
	}
	return titleOnly
}

func foldEqual(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
