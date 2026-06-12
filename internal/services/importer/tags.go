package importer

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	id3v2 "github.com/bogem/id3v2/v2"
	"github.com/go-flac/flacvorbis"
	flac "github.com/go-flac/go-flac"
)

// fileMeta is everything import needs from one music file.
type fileMeta struct {
	Path       string
	Artist     string // album artist when tagged, falling back to track artist
	Album      string
	Title      string
	Track      int
	Disc       int
	Year       int
	DurationMs int
	Format     string // "mp3", "flac"
	Bitrate    int    // kbps; 0 for lossless or unknown

	// MusicBrainz IDs from tags (Picard/beets-managed libraries). Empty when
	// the file isn't MB-tagged.
	MBArtistID       string // album artist MBID
	MBReleaseGroupID string // matches Crate's MusicBrainz album namespace
	MBTrackID        string // release-track MBID, matches album browse tracks
}

// readTags extracts metadata from a music file. Only MP3 and FLAC are
// supported — the same formats Crate can tag.
func readTags(path string) (*fileMeta, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return readMP3(path)
	case ".flac":
		return readFLAC(path)
	}
	return nil, fmt.Errorf("unsupported format")
}

func readMP3(path string) (*fileMeta, error) {
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return nil, fmt.Errorf("read id3: %w", err)
	}
	defer tag.Close()

	m := &fileMeta{Path: path, Format: "mp3"}
	m.Title = strings.TrimSpace(tag.Title())
	m.Album = strings.TrimSpace(tag.Album())

	artist := strings.TrimSpace(tag.Artist())
	albumArtist := strings.TrimSpace(tag.GetTextFrame("TPE2").Text)
	m.Artist = firstNonEmpty(albumArtist, artist)

	m.Track, _ = parseSlashNumber(tag.GetTextFrame("TRCK").Text)
	m.Disc, _ = parseSlashNumber(tag.GetTextFrame("TPOS").Text)
	m.Year = parseYear(firstNonEmpty(tag.Year(), tag.GetTextFrame("TDRC").Text, tag.GetTextFrame("TYER").Text))

	for _, f := range tag.GetFrames("TXXX") {
		udf, ok := f.(id3v2.UserDefinedTextFrame)
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(udf.Description)) {
		case "musicbrainz album artist id":
			m.MBArtistID = strings.TrimSpace(udf.Value)
		case "musicbrainz release group id":
			m.MBReleaseGroupID = strings.TrimSpace(udf.Value)
		case "musicbrainz release track id":
			m.MBTrackID = strings.TrimSpace(udf.Value)
		}
	}

	if br, dur, err := mp3AudioInfo(path); err == nil {
		m.Bitrate = br
		m.DurationMs = dur
	}
	return m, nil
}

func readFLAC(path string) (*fileMeta, error) {
	f, err := flac.ParseFile(path)
	if err != nil {
		return nil, fmt.Errorf("read flac: %w", err)
	}

	m := &fileMeta{Path: path, Format: "flac"}

	var artist, albumArtist string
	for _, block := range f.Meta {
		if block.Type != flac.VorbisComment {
			continue
		}
		cmt, err := flacvorbis.ParseFromMetaDataBlock(*block)
		if err != nil {
			continue
		}
		// Vorbis field names are case-insensitive per spec; parse the raw
		// comments rather than trusting the library's lookup casing.
		for _, c := range cmt.Comments {
			key, val, ok := strings.Cut(c, "=")
			if !ok {
				continue
			}
			val = strings.TrimSpace(val)
			switch strings.ToUpper(strings.TrimSpace(key)) {
			case "TITLE":
				m.Title = val
			case "ALBUM":
				m.Album = val
			case "ARTIST":
				artist = val
			case "ALBUMARTIST", "ALBUM ARTIST":
				albumArtist = val
			case "TRACKNUMBER":
				m.Track, _ = parseSlashNumber(val)
			case "DISCNUMBER":
				m.Disc, _ = parseSlashNumber(val)
			case "DATE", "YEAR", "ORIGINALDATE":
				if m.Year == 0 {
					m.Year = parseYear(val)
				}
			case "MUSICBRAINZ_ALBUMARTISTID":
				m.MBArtistID = val
			case "MUSICBRAINZ_RELEASEGROUPID":
				m.MBReleaseGroupID = val
			case "MUSICBRAINZ_RELEASETRACKID":
				m.MBTrackID = val
			}
		}
	}
	m.Artist = firstNonEmpty(albumArtist, artist)

	if si, err := f.GetStreamInfo(); err == nil && si.SampleRate > 0 {
		m.DurationMs = int(int64(si.SampleCount) * 1000 / int64(si.SampleRate))
	}
	// Crate's convention: lossless formats record bitrate 0.
	m.Bitrate = 0
	return m, nil
}

// parseSlashNumber handles "6" and "6/12" style track/disc tags.
func parseSlashNumber(s string) (int, error) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	return strconv.Atoi(strings.TrimSpace(s))
}

// parseYear extracts the year from "1997", "1997-05-21", or similar.
func parseYear(s string) int {
	s = strings.TrimSpace(s)
	if len(s) > 4 {
		s = s[:4]
	}
	y, err := strconv.Atoi(s)
	if err != nil || y < 1000 || y > 9999 {
		return 0
	}
	return y
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
