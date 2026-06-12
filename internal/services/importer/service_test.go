package importer

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	id3v2 "github.com/bogem/id3v2/v2"
	"github.com/go-flac/flacvorbis"
	flac "github.com/go-flac/go-flac"

	"github.com/TheOutdoorProgrammer/crate/internal/db"
	"github.com/TheOutdoorProgrammer/crate/internal/models"
)

// trackTags describes one fixture file to fabricate.
type trackTags struct {
	artist, albumArtist, album, title string
	track, disc, year                 int
	mbArtistID, mbRGID, mbTrackID     string
}

func writeMP3Fixture(t *testing.T, path string, tt trackTags) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	// Minimal MPEG1 Layer3 128kbps 44100Hz frame, same as the tagger tests.
	frame := make([]byte, 417)
	frame[0] = 0xFF
	frame[1] = 0xFB
	frame[2] = 0x90
	if err := os.WriteFile(path, frame, 0644); err != nil {
		t.Fatal(err)
	}

	tag, err := id3v2.Open(path, id3v2.Options{Parse: false})
	if err != nil {
		t.Fatal(err)
	}
	defer tag.Close()
	tag.SetDefaultEncoding(id3v2.EncodingUTF8)
	tag.SetTitle(tt.title)
	tag.SetArtist(tt.artist)
	tag.SetAlbum(tt.album)
	if tt.albumArtist != "" {
		tag.AddTextFrame("TPE2", id3v2.EncodingUTF8, tt.albumArtist)
	}
	if tt.track > 0 {
		tag.AddTextFrame("TRCK", id3v2.EncodingUTF8, fmt.Sprintf("%d", tt.track))
	}
	if tt.disc > 0 {
		tag.AddTextFrame("TPOS", id3v2.EncodingUTF8, fmt.Sprintf("%d", tt.disc))
	}
	if tt.year > 0 {
		tag.AddTextFrame("TYER", id3v2.EncodingUTF8, fmt.Sprintf("%d", tt.year))
	}
	for desc, val := range map[string]string{
		"MusicBrainz Album Artist Id":  tt.mbArtistID,
		"MusicBrainz Release Group Id": tt.mbRGID,
		"MusicBrainz Release Track Id": tt.mbTrackID,
	} {
		if val != "" {
			tag.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{
				Encoding:    id3v2.EncodingUTF8,
				Description: desc,
				Value:       val,
			})
		}
	}
	if err := tag.Save(); err != nil {
		t.Fatal(err)
	}
}

func writeFLACFixture(t *testing.T, path string, tt trackTags) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, minimalFLAC(), 0644); err != nil {
		t.Fatal(err)
	}
	f, err := flac.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cmt := flacvorbis.New()
	cmt.Add("TITLE", tt.title)
	cmt.Add("ARTIST", tt.artist)
	if tt.albumArtist != "" {
		cmt.Add("ALBUMARTIST", tt.albumArtist)
	}
	cmt.Add("ALBUM", tt.album)
	if tt.track > 0 {
		cmt.Add("TRACKNUMBER", fmt.Sprintf("%d", tt.track))
	}
	if tt.disc > 0 {
		cmt.Add("DISCNUMBER", fmt.Sprintf("%d", tt.disc))
	}
	if tt.year > 0 {
		cmt.Add("DATE", fmt.Sprintf("%d", tt.year))
	}
	if tt.mbArtistID != "" {
		cmt.Add("MUSICBRAINZ_ALBUMARTISTID", tt.mbArtistID)
	}
	if tt.mbRGID != "" {
		cmt.Add("MUSICBRAINZ_RELEASEGROUPID", tt.mbRGID)
	}
	if tt.mbTrackID != "" {
		cmt.Add("MUSICBRAINZ_RELEASETRACKID", tt.mbTrackID)
	}
	block := cmt.Marshal()
	f.Meta = append(f.Meta, &block)
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
}

// minimalFLAC mirrors the tagger test fixture: STREAMINFO + frame sync.
func minimalFLAC() []byte {
	var buf bytes.Buffer
	buf.WriteString("fLaC")
	buf.WriteByte(0x80)
	buf.Write([]byte{0x00, 0x00, 0x22})
	si := make([]byte, 34)
	binary.BigEndian.PutUint16(si[0:2], 4096)
	binary.BigEndian.PutUint16(si[2:4], 4096)
	si[10] = 0xAC
	si[11] = 0x44
	si[12] = byte((44100&0xF)<<4) | byte(1<<1) | byte(15>>4)
	si[13] = byte(15&0xF) << 4
	buf.Write(si)
	buf.Write([]byte{0xFF, 0xF8, 0x00, 0x00})
	return buf.Bytes()
}

type impEnv struct {
	svc     *Service
	queries *db.Queries
	library string
}

func newImpEnv(t *testing.T) *impEnv {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	queries := db.NewQueries(database)
	libraryDir := t.TempDir()
	return &impEnv{
		svc:     NewService(queries, libraryDir, nil),
		queries: queries,
		library: libraryDir,
	}
}

// runImport starts an import and waits for it to finish.
func (e *impEnv) runImport(t *testing.T, path string, dryRun bool) *Report {
	t.Helper()
	if err := e.svc.Start(path, dryRun); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := e.svc.Status()
		switch st.Status {
		case "done":
			return st.Report
		case "failed":
			t.Fatalf("import failed: %s", st.Error)
		}
		if time.Now().After(deadline) {
			t.Fatal("import did not finish in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestImportFreshLibrary(t *testing.T) {
	env := newImpEnv(t)
	dir := filepath.Join(env.library, "Radiohead", "OK Computer (1997)")
	writeMP3Fixture(t, filepath.Join(dir, "01 - Airbag.mp3"),
		trackTags{artist: "Radiohead", album: "OK Computer", title: "Airbag", track: 1, disc: 1, year: 1997})
	writeFLACFixture(t, filepath.Join(dir, "02 - Paranoid Android.flac"),
		trackTags{artist: "Radiohead", album: "OK Computer", title: "Paranoid Android", track: 2, disc: 1, year: 1997})

	report := env.runImport(t, "", false)

	if report.ArtistsAdded != 1 || report.AlbumsAdded != 1 || report.TracksAdded != 2 {
		t.Fatalf("report = %+v, want 1 artist / 1 album / 2 tracks", report)
	}

	artist, err := env.queries.FindArtistByNameFold("radiohead")
	if err != nil {
		t.Fatal(err)
	}
	if artist.Provider != "local" || artist.Status != models.ArtistStatusOwned {
		t.Errorf("artist = %s/%s status %s, want local provider, owned", artist.Provider, artist.ProviderID, artist.Status)
	}

	album, err := env.queries.FindAlbumByArtistTitleFold(artist.ID, "ok computer")
	if err != nil {
		t.Fatal(err)
	}
	if album.Year == nil || *album.Year != 1997 || album.Status != models.AlbumStatusOwned {
		t.Errorf("album year/status = %v/%s, want 1997/owned", album.Year, album.Status)
	}

	track, err := env.queries.FindTrackByAlbumTitleFold(album.ID, "airbag")
	if err != nil {
		t.Fatal(err)
	}
	if track.Status != models.TrackStatusOwned {
		t.Errorf("track status = %s, want owned", track.Status)
	}
	if track.FilePath == nil || *track.FilePath != filepath.Join("Radiohead", "OK Computer (1997)", "01 - Airbag.mp3") {
		t.Errorf("file_path = %v, want library-relative path", track.FilePath)
	}
	if track.DownloadFormat == nil || *track.DownloadFormat != "mp3" {
		t.Errorf("download_format = %v, want mp3", track.DownloadFormat)
	}
	if track.DownloadBitrate == nil || *track.DownloadBitrate != 128 {
		t.Errorf("download_bitrate = %v, want 128 (parsed from frame header)", track.DownloadBitrate)
	}

	flacTrack, err := env.queries.FindTrackByAlbumTitleFold(album.ID, "paranoid android")
	if err != nil {
		t.Fatal(err)
	}
	if flacTrack.DownloadFormat == nil || *flacTrack.DownloadFormat != "flac" {
		t.Errorf("flac download_format = %v, want flac", flacTrack.DownloadFormat)
	}
	if flacTrack.DownloadBitrate == nil || *flacTrack.DownloadBitrate != 0 {
		t.Errorf("flac download_bitrate = %v, want 0 (lossless)", flacTrack.DownloadBitrate)
	}

	// Re-running the import must change nothing.
	again := env.runImport(t, "", false)
	if again.ArtistsAdded != 0 || again.AlbumsAdded != 0 || again.TracksAdded != 0 || again.TracksKnown != 2 {
		t.Errorf("re-import = %+v, want everything known, nothing added", again)
	}
}

func TestImportDryRunWritesNothing(t *testing.T) {
	env := newImpEnv(t)
	writeMP3Fixture(t, filepath.Join(env.library, "a.mp3"),
		trackTags{artist: "X", album: "Y", title: "T", track: 1})

	report := env.runImport(t, "", true)
	if report.TracksAdded != 1 || report.ArtistsAdded != 1 {
		t.Fatalf("dry-run report = %+v, want counts as if importing", report)
	}
	if _, err := env.queries.FindArtistByNameFold("X"); err == nil {
		t.Error("dry run must not create artists")
	}
}

func TestImportClaimsWantedTracks(t *testing.T) {
	env := newImpEnv(t)

	artist := &models.Artist{Name: "Radiohead", Provider: "deezer", ProviderID: "d1", Status: models.ArtistStatusWatched}
	if err := env.queries.CreateArtist(artist); err != nil {
		t.Fatal(err)
	}
	year := 1997
	album := &models.Album{ArtistID: artist.ID, Title: "OK Computer", Year: &year, Provider: "deezer", ProviderID: "d2", RecordType: "album", Status: models.AlbumStatusWatched}
	if err := env.queries.CreateAlbum(album); err != nil {
		t.Fatal(err)
	}
	track := &models.Track{AlbumID: album.ID, Title: "Airbag", TrackNumber: 1, DiscNumber: 1, Provider: "deezer", ProviderID: "d3", Status: models.TrackStatusWanted}
	if err := env.queries.CreateTrack(track); err != nil {
		t.Fatal(err)
	}

	writeMP3Fixture(t, filepath.Join(env.library, "airbag.mp3"),
		trackTags{artist: "radiohead", album: "ok computer", title: "AIRBAG", track: 1, year: 1997})

	report := env.runImport(t, "", false)

	if report.ArtistsAdded != 0 || report.AlbumsAdded != 0 || report.TracksAdded != 0 {
		t.Errorf("report added = %+v, want nothing new (existing rows reused)", report)
	}
	if report.TracksClaimed != 1 {
		t.Fatalf("claimed = %d, want 1", report.TracksClaimed)
	}

	got, err := env.queries.GetTrackWithMeta(track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.TrackStatusOwned {
		t.Errorf("claimed track status = %s, want owned", got.Status)
	}
	if got.FilePath == nil || *got.FilePath != "airbag.mp3" {
		t.Errorf("claimed file_path = %v, want airbag.mp3", got.FilePath)
	}
	if got.Provider != "deezer" || got.ProviderID != "d3" {
		t.Errorf("claim must not rewrite provider identity, got %s/%s", got.Provider, got.ProviderID)
	}
}

func TestImportMusicBrainzTags(t *testing.T) {
	env := newImpEnv(t)
	tags := trackTags{
		artist: "Radiohead", album: "OK Computer", title: "Airbag", track: 1, year: 1997,
		mbArtistID: "a74b1b7f-71a5-4011-9441-d0b5e4122711",
		mbRGID:     "b1392450-e666-3926-a536-22c65f834433",
		mbTrackID:  "5e5605b0-cd70-3aae-9b00-4e5f54c95dc5",
	}
	writeFLACFixture(t, filepath.Join(env.library, "airbag.flac"), tags)

	report := env.runImport(t, "", false)
	if report.MusicBrainzLinked != 1 {
		t.Fatalf("musicbrainz_linked = %d, want 1", report.MusicBrainzLinked)
	}

	artist, err := env.queries.FindArtistByProvider("musicbrainz", tags.mbArtistID)
	if err != nil {
		t.Fatalf("artist not under musicbrainz provider: %v", err)
	}
	album, err := env.queries.FindAlbumByProvider("musicbrainz", tags.mbRGID)
	if err != nil {
		t.Fatalf("album not under release-group id: %v", err)
	}
	if album.ArtistID != artist.ID {
		t.Error("album not attached to imported artist")
	}
	if _, err := env.queries.FindTrackByProvider("musicbrainz", tags.mbTrackID); err != nil {
		t.Fatalf("track not under release-track id: %v", err)
	}
}

func TestImportMixedMBIDsFallBackToLocal(t *testing.T) {
	env := newImpEnv(t)
	writeMP3Fixture(t, filepath.Join(env.library, "one.mp3"),
		trackTags{artist: "X", album: "A", title: "One", track: 1, mbArtistID: "id-1"})
	writeMP3Fixture(t, filepath.Join(env.library, "two.mp3"),
		trackTags{artist: "X", album: "B", title: "Two", track: 1, mbArtistID: "id-2"})

	env.runImport(t, "", false)

	artist, err := env.queries.FindArtistByNameFold("X")
	if err != nil {
		t.Fatal(err)
	}
	if artist.Provider != "local" {
		t.Errorf("conflicting artist MBIDs must fall back to local, got %s", artist.Provider)
	}
}

func TestImportSkipsFilesWithMissingTags(t *testing.T) {
	env := newImpEnv(t)
	writeMP3Fixture(t, filepath.Join(env.library, "untagged.mp3"), trackTags{title: "Only Title"})
	writeMP3Fixture(t, filepath.Join(env.library, "good.mp3"),
		trackTags{artist: "X", album: "Y", title: "T", track: 1})

	report := env.runImport(t, "", false)
	if report.FilesSkipped != 1 {
		t.Fatalf("files_skipped = %d, want 1", report.FilesSkipped)
	}
	if len(report.SkippedSamples) != 1 || report.SkippedSamples[0].Reason == "" {
		t.Errorf("skipped sample should carry a reason, got %+v", report.SkippedSamples)
	}
	if report.TracksAdded != 1 {
		t.Errorf("tracks_added = %d, want 1", report.TracksAdded)
	}
}

func TestImportOutsideLibraryStoresAbsolutePaths(t *testing.T) {
	env := newImpEnv(t)
	outside := t.TempDir()
	abs := filepath.Join(outside, "song.mp3")
	writeMP3Fixture(t, abs, trackTags{artist: "X", album: "Y", title: "T", track: 1})

	env.runImport(t, outside, false)

	artist, _ := env.queries.FindArtistByNameFold("X")
	album, _ := env.queries.FindAlbumByArtistTitleFold(artist.ID, "Y")
	track, err := env.queries.FindTrackByAlbumTitleFold(album.ID, "T")
	if err != nil {
		t.Fatal(err)
	}
	if track.FilePath == nil || *track.FilePath != abs {
		t.Errorf("file_path = %v, want absolute %s", track.FilePath, abs)
	}
}

func TestImportDuplicateFormatsFirstWins(t *testing.T) {
	env := newImpEnv(t)
	tags := trackTags{artist: "X", album: "Y", title: "T", track: 1}
	writeFLACFixture(t, filepath.Join(env.library, "t.flac"), tags)
	writeMP3Fixture(t, filepath.Join(env.library, "t.mp3"), tags)

	report := env.runImport(t, "", false)
	if report.TracksAdded != 1 || report.DuplicateFiles != 1 {
		t.Fatalf("report = %+v, want 1 added + 1 duplicate", report)
	}

	artist, _ := env.queries.FindArtistByNameFold("X")
	album, _ := env.queries.FindAlbumByArtistTitleFold(artist.ID, "Y")
	track, _ := env.queries.FindTrackByAlbumTitleFold(album.ID, "T")
	if track.DownloadFormat == nil || *track.DownloadFormat != "flac" {
		t.Errorf("first file (flac) should win, got %v", track.DownloadFormat)
	}
}

func TestImportRejectsBadPath(t *testing.T) {
	env := newImpEnv(t)
	if err := env.svc.Start(filepath.Join(env.library, "nope"), false); err == nil {
		t.Error("expected error for missing directory")
	}
}
