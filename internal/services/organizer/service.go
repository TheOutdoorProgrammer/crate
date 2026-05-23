package organizer

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/models"
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

	albumDir := fmt.Sprintf("%s (%d)", album.Title, func() int {
		if album.Year != nil {
			return *album.Year
		}
		return 0
	}())
	if album.Year == nil {
		albumDir = album.Title
	}

	destDir := filepath.Join(s.libraryDir, sanitize(artist.Name), sanitize(albumDir))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}

	ext := filepath.Ext(src)
	filename := fmt.Sprintf("%02d - %s%s", track.TrackNumber, sanitize(track.Title), ext)
	dest := filepath.Join(destDir, filename)

	if err := os.Rename(src, dest); err != nil {
		// Cross-device: copy then remove
		if err2 := copyFile(src, dest); err2 != nil {
			return err2
		}
		os.Remove(src)
	}

	slog.Info("organizer: moved", "dest", dest)

	year := 0
	if album.Year != nil {
		year = *album.Year
	}
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

	return s.queries.UpdateTrackFilePath(track.ID, dest)
}

var unsafeChars = regexp.MustCompile(`[<>:"/\\|?*]`)

func sanitize(s string) string {
	return strings.TrimSpace(unsafeChars.ReplaceAllString(s, "_"))
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
