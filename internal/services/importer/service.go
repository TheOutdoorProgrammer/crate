// Package importer scans an existing on-disk music library and records it in
// Crate's database, so Crate can manage a collection it didn't download.
//
// Import is tag-based and non-destructive: files are never moved, renamed, or
// rewritten, and folder structure is ignored entirely — only embedded
// metadata matters. Files carrying consistent MusicBrainz IDs (Picard or
// beets libraries) are imported under the musicbrainz provider with their
// real IDs; everything else gets the reserved "local" provider with stable
// tag-derived IDs and can be relinked to a real provider later.
package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TheOutdoorProgrammer/crate/internal/activity"
	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/library"
	"github.com/TheOutdoorProgrammer/crate/internal/models"
	"github.com/TheOutdoorProgrammer/crate/internal/provider"
)

// musicbrainzProvider is the conventional name of the MusicBrainz provider
// (the default in CRATE_PROVIDERS). MB-tagged files are imported under this
// name so they share a namespace with browsed/watched entities.
const musicbrainzProvider = "musicbrainz"

const maxSkippedSamples = 100

type Service struct {
	queries    *db.Queries
	libraryDir string
	activity   *activity.Log

	mu    sync.Mutex
	state State
}

// State is a snapshot of the importer, served to the UI while it polls.
type State struct {
	Status    string  `json:"status"` // idle | running | done | failed
	DryRun    bool    `json:"dry_run"`
	Path      string  `json:"path,omitempty"`
	StartedAt string  `json:"started_at,omitempty"`
	Processed int     `json:"processed"`
	Total     int     `json:"total"`
	Report    *Report `json:"report,omitempty"`
	Error     string  `json:"error,omitempty"`
}

// Report summarizes what an import did (or would do, for a dry run).
type Report struct {
	ArtistsAdded      int           `json:"artists_added"`
	AlbumsAdded       int           `json:"albums_added"`
	TracksAdded       int           `json:"tracks_added"`
	TracksClaimed     int           `json:"tracks_claimed"` // existing wanted/watched tracks that gained a file
	TracksKnown       int           `json:"tracks_known"`   // already owned, untouched
	MusicBrainzLinked int           `json:"musicbrainz_linked"`
	DuplicateFiles    int           `json:"duplicate_files"`
	FilesSkipped      int           `json:"files_skipped"`
	SkippedSamples    []SkippedFile `json:"skipped_samples,omitempty"`
}

type SkippedFile struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

func NewService(queries *db.Queries, libraryDir string, actLog *activity.Log) *Service {
	return &Service{
		queries:    queries,
		libraryDir: libraryDir,
		activity:   actLog,
		state:      State{Status: "idle"},
	}
}

// Status returns a copy of the current importer state.
func (s *Service) Status() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Start kicks off an asynchronous import of path (the library dir when
// empty). It returns an error if an import is already running or the path is
// not a directory; progress and results are read via Status.
func (s *Service) Start(path string, dryRun bool) error {
	if strings.TrimSpace(path) == "" {
		path = s.libraryDir
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("not a directory: %s", path)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Status == "running" {
		return fmt.Errorf("an import is already running")
	}
	s.state = State{
		Status:    "running",
		DryRun:    dryRun,
		Path:      path,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}

	go s.run(path, dryRun)
	return nil
}

func (s *Service) run(path string, dryRun bool) {
	report, err := s.scan(path, dryRun)

	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.state.Status = "failed"
		s.state.Error = err.Error()
		slog.Error("importer: failed", "path", path, "error", err)
		return
	}
	s.state.Status = "done"
	s.state.Report = report

	if !dryRun && s.activity != nil {
		s.activity.Record("library_import", "library", 0, fmt.Sprintf(
			"Imported %d tracks (%d artists, %d albums), claimed %d, skipped %d files",
			report.TracksAdded, report.ArtistsAdded, report.AlbumsAdded, report.TracksClaimed, report.FilesSkipped))
	}
	slog.Info("importer: finished", "path", path, "dry_run", dryRun,
		"artists", report.ArtistsAdded, "albums", report.AlbumsAdded,
		"tracks", report.TracksAdded, "claimed", report.TracksClaimed, "skipped", report.FilesSkipped)
}

type albumGroup struct {
	title string
	years map[int]int
	rgIDs map[string]bool
	files []*fileMeta
}

type artistGroup struct {
	name   string
	mbIDs  map[string]bool
	albums map[string]*albumGroup
}

func (s *Service) scan(root string, dryRun bool) (*Report, error) {
	files, err := collectFiles(root)
	if err != nil {
		return nil, err
	}
	s.setTotal(len(files))

	report := &Report{}
	artists := make(map[string]*artistGroup)

	for _, path := range files {
		s.bumpProcessed()
		meta, err := readTags(path)
		if err != nil {
			report.skip(path, err.Error())
			continue
		}
		if meta.Artist == "" || meta.Album == "" || meta.Title == "" {
			report.skip(path, "missing artist, album, or title tags")
			continue
		}

		aKey := strings.ToLower(meta.Artist)
		ag, ok := artists[aKey]
		if !ok {
			ag = &artistGroup{name: meta.Artist, mbIDs: make(map[string]bool), albums: make(map[string]*albumGroup)}
			artists[aKey] = ag
		}
		ag.mbIDs[meta.MBArtistID] = true

		alKey := strings.ToLower(meta.Album)
		alg, ok := ag.albums[alKey]
		if !ok {
			alg = &albumGroup{title: meta.Album, years: make(map[int]int), rgIDs: make(map[string]bool)}
			ag.albums[alKey] = alg
		}
		if meta.Year > 0 {
			alg.years[meta.Year]++
		}
		alg.rgIDs[meta.MBReleaseGroupID] = true
		alg.files = append(alg.files, meta)
	}

	// Deterministic order keeps re-runs and tests stable.
	artistKeys := make([]string, 0, len(artists))
	for k := range artists {
		artistKeys = append(artistKeys, k)
	}
	sort.Strings(artistKeys)

	for _, k := range artistKeys {
		if err := s.persistArtist(artists[k], dryRun, report); err != nil {
			return nil, err
		}
	}
	return report, nil
}

func (s *Service) persistArtist(g *artistGroup, dryRun bool, report *Report) error {
	prov, pid := provider.LocalProvider, localID("artist", g.name)
	if mbid := soleKey(g.mbIDs); mbid != "" {
		prov, pid = musicbrainzProvider, mbid
	}

	// Reuse an existing artist row — by provider identity first, then by
	// name so imports attach to artists the user already watches.
	artist, _ := s.queries.FindArtistByProvider(prov, pid)
	if artist == nil {
		artist, _ = s.queries.FindArtistByNameFold(g.name)
	}
	if artist == nil {
		report.ArtistsAdded++
		artist = &models.Artist{Name: g.name, Provider: prov, ProviderID: pid, Status: models.ArtistStatusOwned}
		if dryRun {
			artist.ID = -1
		} else if err := s.queries.CreateArtist(artist); err != nil {
			return fmt.Errorf("create artist %q: %w", g.name, err)
		}
	}

	albumKeys := make([]string, 0, len(g.albums))
	for k := range g.albums {
		albumKeys = append(albumKeys, k)
	}
	sort.Strings(albumKeys)

	for _, k := range albumKeys {
		if err := s.persistAlbum(artist, g.albums[k], dryRun, report); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) persistAlbum(artist *models.Artist, g *albumGroup, dryRun bool, report *Report) error {
	prov, pid := provider.LocalProvider, localID("album", artist.Name, g.title)
	if rgID := soleKey(g.rgIDs); rgID != "" {
		prov, pid = musicbrainzProvider, rgID
	}

	var album *models.Album
	if !dryRun || artist.ID > 0 {
		album, _ = s.queries.FindAlbumByProvider(prov, pid)
		if album == nil && artist.ID > 0 {
			album, _ = s.queries.FindAlbumByArtistTitleFold(artist.ID, g.title)
		}
	}
	if album == nil {
		report.AlbumsAdded++
		album = &models.Album{
			ArtistID:   artist.ID,
			Title:      g.title,
			Year:       commonYear(g.years),
			Provider:   prov,
			ProviderID: pid,
			RecordType: "album",
			Status:     models.AlbumStatusOwned,
		}
		if dryRun {
			album.ID = -1
		} else if err := s.queries.CreateAlbum(album); err != nil {
			return fmt.Errorf("create album %q: %w", g.title, err)
		}
	}

	sort.Slice(g.files, func(i, j int) bool {
		if g.files[i].Disc != g.files[j].Disc {
			return g.files[i].Disc < g.files[j].Disc
		}
		if g.files[i].Track != g.files[j].Track {
			return g.files[i].Track < g.files[j].Track
		}
		return g.files[i].Path < g.files[j].Path
	})
	for _, f := range g.files {
		s.persistTrack(artist, album, f, dryRun, report)
	}
	return nil
}

func (s *Service) persistTrack(artist *models.Artist, album *models.Album, f *fileMeta, dryRun bool, report *Report) {
	mbLinked := album.Provider == musicbrainzProvider && f.MBTrackID != ""

	var track *models.Track
	// Fingerprint-verified MusicBrainz recording id (e.g. from Music Assistant's
	// AcoustID provider) is the highest-confidence signal — trust it first,
	// scoped to this album.
	if f.MBRecordingID != "" && album.ID > 0 {
		track, _ = s.queries.FindTrackByAlbumRecordingID(album.ID, f.MBRecordingID)
	}
	if track == nil && mbLinked {
		track, _ = s.queries.FindTrackByProvider(musicbrainzProvider, f.MBTrackID)
	}
	if track == nil && album.ID > 0 {
		track, _ = s.queries.FindTrackByAlbumTitleFold(album.ID, f.Title)
	}

	storedPath := f.Path
	if library.Contains(s.libraryDir, f.Path) {
		if rel, err := filepath.Rel(s.libraryDir, f.Path); err == nil {
			storedPath = rel
		}
	}

	switch {
	case track == nil:
		report.TracksAdded++
		if mbLinked {
			report.MusicBrainzLinked++
		}
		if dryRun {
			return
		}
		prov, pid := provider.LocalProvider, localID("track", artist.Name, album.Title, f.Title, fmt.Sprint(f.Disc), fmt.Sprint(f.Track))
		if mbLinked {
			prov, pid = musicbrainzProvider, f.MBTrackID
		}
		t := &models.Track{
			AlbumID:         album.ID,
			Title:           f.Title,
			TrackNumber:     f.Track,
			DiscNumber:      f.Disc,
			DurationMs:      f.DurationMs,
			Provider:        prov,
			ProviderID:      pid,
			Status:          models.TrackStatusOwned,
			FilePath:        &storedPath,
			DownloadFormat:  &f.Format,
			DownloadBitrate: &f.Bitrate,
			MBRecordingID:   optStr(f.MBRecordingID),
		}
		if err := s.queries.CreateImportedTrack(t); err != nil {
			report.skip(f.Path, "db insert failed: "+err.Error())
			report.TracksAdded--
		}

	case track.Status == models.TrackStatusOwned && track.FilePath != nil && *track.FilePath != "":
		if *track.FilePath == storedPath {
			report.TracksKnown++
		} else {
			// Same track resolved from a second file (e.g. the album exists
			// in both FLAC and MP3). First one wins; never reassign.
			report.DuplicateFiles++
		}

	default:
		// The track exists from watching/browsing but has no file — claim it.
		report.TracksClaimed++
		if dryRun {
			return
		}
		if err := s.queries.ClaimTrackFile(track.ID, storedPath, f.Format, f.Bitrate, f.DurationMs, optStr(f.MBRecordingID)); err != nil {
			report.skip(f.Path, "db claim failed: "+err.Error())
			report.TracksClaimed--
		}
	}
}

func collectFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		name := d.Name()
		if d.IsDir() {
			// Hidden dirs and NAS metadata dirs have no music in them.
			if path != root && (strings.HasPrefix(name, ".") || name == "@eaDir") {
				return filepath.SkipDir
			}
			return nil
		}
		// AppleDouble sidecar files carry bogus copies of real tags.
		if strings.HasPrefix(name, "._") {
			return nil
		}
		switch strings.ToLower(filepath.Ext(name)) {
		case ".mp3", ".flac":
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// localID builds a stable, tag-derived provider ID for the local provider.
// It survives file moves and renames because it never includes paths.
func localID(kind string, parts ...string) string {
	h := sha256.New()
	h.Write([]byte(kind))
	for _, p := range parts {
		h.Write([]byte{0x1f})
		h.Write([]byte(strings.ToLower(strings.TrimSpace(p))))
	}
	return "loc-" + hex.EncodeToString(h.Sum(nil))[:16]
}

// optStr returns a pointer to s, or nil when s is empty, so blank tag values
// persist as SQL NULL rather than "".
func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// soleKey returns the only non-empty key of m, or "" when the set is empty,
// mixed, or contains any blank entry — i.e. the files don't agree.
func soleKey(m map[string]bool) string {
	if len(m) != 1 {
		return ""
	}
	for k := range m {
		return k
	}
	return ""
}

func commonYear(years map[int]int) *int {
	best, bestN := 0, 0
	for y, n := range years {
		if n > bestN || (n == bestN && y < best) {
			best, bestN = y, n
		}
	}
	if best == 0 {
		return nil
	}
	return &best
}

func (s *Service) setTotal(n int) {
	s.mu.Lock()
	s.state.Total = n
	s.mu.Unlock()
}

func (s *Service) bumpProcessed() {
	s.mu.Lock()
	s.state.Processed++
	s.mu.Unlock()
}

func (r *Report) skip(path, reason string) {
	r.FilesSkipped++
	if len(r.SkippedSamples) < maxSkippedSamples {
		r.SkippedSamples = append(r.SkippedSamples, SkippedFile{Path: path, Reason: reason})
	}
}
