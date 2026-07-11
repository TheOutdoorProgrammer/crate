package organizer

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/library"
	"github.com/TheOutdoorProgrammer/crate/internal/models"
	"github.com/TheOutdoorProgrammer/crate/internal/naming"
	"github.com/TheOutdoorProgrammer/crate/internal/services/tagger"
)

type Service struct {
	queries      *db.Queries
	downloadsDir string
	libraryDir   string
}

func NewService(queries *db.Queries, downloadsDir, libraryDir string) *Service {
	return &Service{queries: queries, downloadsDir: downloadsDir, libraryDir: libraryDir}
}

// Organize moves a single track's downloaded file into the library.
// track.FilePath must be set to the downloaded filename (basename).
func (s *Service) Organize(track *models.Track) error {
	if track.FilePath == nil {
		return fmt.Errorf("organizer: track %d has no file_path", track.ID)
	}
	srcBase := filepath.Base(*track.FilePath)
	src, err := findFile(s.downloadsDir, srcBase)
	if err != nil {
		return fmt.Errorf("organizer: %w", err)
	}

	album, err := s.queries.GetAlbum(track.AlbumID)
	if err != nil {
		return err
	}
	artist, err := s.queries.GetArtist(album.ArtistID)
	if err != nil {
		return err
	}

	year := 0
	if album.Year != nil {
		year = *album.Year
	}
	meta := naming.Meta{
		Artist: artist.Name,
		Album:  album.Title,
		Year:   year,
		Track:  track.TrackNumber,
		Disc:   track.DiscNumber,
		Title:  track.Title,
	}

	tmpl := naming.DefaultTemplate
	if v, err := s.queries.GetSetting(naming.SettingKey); err == nil && strings.TrimSpace(v) != "" {
		tmpl = v
	}
	rel, err := naming.Render(tmpl, meta)
	if err != nil && tmpl != naming.DefaultTemplate {
		// The API validates templates on save, so this only happens if the
		// stored value was edited by hand or the data hits an empty-segment
		// case. Fall back loudly rather than blocking downloads forever.
		slog.Error("organizer: naming template failed, using default layout", "template", tmpl, "error", err)
		rel, err = naming.Render(naming.DefaultTemplate, meta)
	}
	if err != nil {
		return fmt.Errorf("organizer: render path: %w", err)
	}

	dest := filepath.Join(s.libraryDir, rel) + filepath.Ext(src)
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	// The DB still holds the previous library path at this point (the
	// downloader only overwrites track.FilePath in memory), so capture it to
	// clean up the file being replaced — e.g. a quality upgrade landing at a
	// different path after the naming template changed.
	oldAbs := s.storedFilePath(track.ID)

	if err := os.Rename(src, dest); err != nil {
		// Cross-device: copy then remove
		if err2 := copyFile(src, dest); err2 != nil {
			return err2
		}
		os.Remove(src)
	}

	slog.Info("organizer: moved", "dest", dest)
	s.removeReplacedFile(oldAbs, dest)

	coverURL := ""
	if album.CoverURL != nil {
		coverURL = *album.CoverURL
	}
	if err := tagger.Tag(dest, tagger.TrackMeta{
		Title:       track.Title,
		Artist:      artist.Name,
		Album:       album.Title,
		TrackNumber: track.TrackNumber,
		DiscNumber:  track.DiscNumber,
		Year:        year,
		CoverURL:    coverURL,
	}); err != nil {
		slog.Warn("organizer: tagging failed", "dest", dest, "error", err)
	}

	relPath, err := filepath.Rel(s.libraryDir, dest)
	if err != nil {
		return fmt.Errorf("organizer: relative path: %w", err)
	}
	return s.queries.UpdateTrackFilePath(track.ID, relPath)
}

// storedFilePath returns the track's current file path from the DB resolved
// to an absolute path, or "" if the track has none (first download).
func (s *Service) storedFilePath(trackID int64) string {
	stored, err := s.queries.GetTrackWithMeta(trackID)
	if err != nil || stored.FilePath == nil || *stored.FilePath == "" {
		return ""
	}
	return library.ResolvePath(s.libraryDir, *stored.FilePath)
}

// removeReplacedFile deletes the file a new download replaced, when it lives
// inside the library at a different path (e.g. a quality upgrade after the
// naming template changed). Files outside the library — imported entries
// pointing at user-managed locations — are never touched.
func (s *Service) removeReplacedFile(oldAbs, dest string) {
	if oldAbs == "" || oldAbs == filepath.Clean(dest) {
		return
	}
	if !library.Contains(s.libraryDir, oldAbs) {
		slog.Warn("organizer: replaced file is outside the library, leaving it", "path", oldAbs)
		return
	}
	if err := os.Remove(oldAbs); err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("organizer: could not remove replaced file", "path", oldAbs, "error", err)
		}
		return
	}
	slog.Info("organizer: removed replaced file", "path", oldAbs)
	pruneEmptyDirs(filepath.Dir(oldAbs), s.libraryDir)
}

// pruneEmptyDirs removes now-empty directories from dir up to (but not
// including) root. os.Remove fails on non-empty directories, which ends the
// walk at the first level still in use.
func pruneEmptyDirs(dir, root string) {
	root = filepath.Clean(root)
	for dir = filepath.Clean(dir); dir != root && library.Contains(root, dir); dir = filepath.Dir(dir) {
		if err := os.Remove(dir); err != nil {
			return
		}
	}
}

func findFile(root, name string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if filepath.Base(path) == name {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("not found: %s", name)
	}
	return found, err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	buf := make([]byte, 1<<20)
	for {
		n, readErr := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			out.Close()
			return readErr
		}
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
