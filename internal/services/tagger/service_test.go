package tagger

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	id3v2 "github.com/bogem/id3v2/v2"
	flac "github.com/go-flac/go-flac"
	"github.com/go-flac/flacvorbis"
)

func TestReadAllUnderLimit(t *testing.T) {
	data := []byte("hello world")
	got, err := readAll(bytes.NewReader(data), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello world" {
		t.Errorf("readAll = %q, want %q", got, "hello world")
	}
}

func TestReadAllOverLimit(t *testing.T) {
	data := strings.Repeat("x", 1000)
	_, err := readAll(strings.NewReader(data), 100)
	if err == nil {
		t.Error("expected error for data exceeding limit")
	}
}

func TestBuildFLACPicture(t *testing.T) {
	pic := &coverData{
		data:     []byte{0xFF, 0xD8, 0xFF, 0xE0},
		mimeType: "image/jpeg",
	}
	block := buildFLACPicture(pic)
	if block == nil {
		t.Fatal("expected non-nil block")
	}
	if block.Type != 6 {
		t.Errorf("block type = %d, want 6 (Picture)", block.Type)
	}
	if !bytes.Contains(block.Data, []byte("image/jpeg")) {
		t.Error("block data should contain mime type")
	}
	if !bytes.Contains(block.Data, pic.data) {
		t.Error("block data should contain image data")
	}
}

func TestTagUnsupportedFormat(t *testing.T) {
	err := Tag("/tmp/fake.ogg", TrackMeta{Title: "test"})
	if err != nil {
		t.Errorf("unsupported format should return nil, got %v", err)
	}
}

func makeMinimalWAV() []byte {
	// Minimal valid WAV: RIFF header + fmt chunk + data chunk (1 sample of silence)
	var buf []byte
	buf = append(buf, []byte("RIFF")...)
	buf = append(buf, 0, 0, 0, 0) // placeholder size
	buf = append(buf, []byte("WAVE")...)

	// fmt chunk: PCM, 1 channel, 44100 Hz, 16-bit
	buf = append(buf, []byte("fmt ")...)
	buf = append(buf, 16, 0, 0, 0)         // chunk size
	buf = append(buf, 1, 0)                // PCM
	buf = append(buf, 1, 0)                // 1 channel
	buf = append(buf, 0x44, 0xAC, 0, 0)    // 44100 Hz
	buf = append(buf, 0x88, 0x58, 0x01, 0) // byte rate
	buf = append(buf, 2, 0)                // block align
	buf = append(buf, 16, 0)               // bits per sample

	// data chunk: 2 bytes of silence
	buf = append(buf, []byte("data")...)
	buf = append(buf, 2, 0, 0, 0)
	buf = append(buf, 0, 0)

	// Update RIFF size
	riffSize := len(buf) - 8
	buf[4] = byte(riffSize)
	buf[5] = byte(riffSize >> 8)
	buf[6] = byte(riffSize >> 16)
	buf[7] = byte(riffSize >> 24)

	return buf
}

func TestTagWAV(t *testing.T) {
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "test.wav")
	if err := os.WriteFile(wavPath, makeMinimalWAV(), 0644); err != nil {
		t.Fatal(err)
	}

	meta := TrackMeta{
		Title:       "Airbag",
		Artist:      "Radiohead",
		Album:       "OK Computer",
		TrackNumber: 1,
		DiscNumber:  1,
		Year:        1997,
	}

	if err := tagWAV(wavPath, meta); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(wavPath)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Contains(data, []byte("LIST")) {
		t.Error("tagged WAV should contain LIST chunk")
	}
	if !bytes.Contains(data, []byte("INFO")) {
		t.Error("tagged WAV should contain INFO type")
	}
	for _, field := range []string{"Airbag", "Radiohead", "OK Computer", "1997"} {
		if !bytes.Contains(data, []byte(field)) {
			t.Errorf("tagged WAV should contain %q", field)
		}
	}

	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		t.Error("tagged WAV should still be valid RIFF/WAVE")
	}
	riffSize := int(data[4]) | int(data[5])<<8 | int(data[6])<<16 | int(data[7])<<24
	if riffSize != len(data)-8 {
		t.Errorf("RIFF size %d doesn't match file size %d - 8", riffSize, len(data))
	}
}

func TestTagWAVIdempotent(t *testing.T) {
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "test.wav")
	if err := os.WriteFile(wavPath, makeMinimalWAV(), 0644); err != nil {
		t.Fatal(err)
	}

	meta := TrackMeta{Title: "Test", Artist: "Artist", Album: "Album", TrackNumber: 1, Year: 2020}

	if err := tagWAV(wavPath, meta); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(wavPath)

	if err := tagWAV(wavPath, meta); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(wavPath)

	if !bytes.Equal(first, second) {
		t.Errorf("tagging twice should produce identical output (got %d vs %d bytes)", len(first), len(second))
	}
}

func TestStripListInfo(t *testing.T) {
	wav := makeMinimalWAV()
	// Tag it to add a LIST INFO chunk
	dir := t.TempDir()
	p := filepath.Join(dir, "test.wav")
	os.WriteFile(p, wav, 0644)
	tagWAV(p, TrackMeta{Title: "X", Artist: "Y", Album: "Z", TrackNumber: 1, Year: 2000})
	tagged, _ := os.ReadFile(p)

	stripped := stripListInfo(tagged)
	if bytes.Contains(stripped, []byte("INFO")) {
		t.Error("stripListInfo should remove LIST INFO chunk")
	}
	if !bytes.Contains(stripped, []byte("WAVE")) {
		t.Error("stripped data should still contain WAVE header")
	}
}

// makeMinimalMP3 creates a minimal valid MP3 file that the id3v2 library can open.
func makeMinimalMP3() []byte {
	dir, _ := os.MkdirTemp("", "mp3test")
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "blank.mp3")
	// Write a valid MPEG audio frame sync (0xFF 0xFB) followed by enough padding
	// for a valid frame, then let id3v2 create a proper tag on top.
	frame := make([]byte, 417) // Minimum MPEG1 Layer3 128kbps frame
	frame[0] = 0xFF
	frame[1] = 0xFB // MPEG1 Layer3
	frame[2] = 0x90 // 128kbps, 44100Hz
	frame[3] = 0x00
	os.WriteFile(p, frame, 0644)

	tag, _ := id3v2.Open(p, id3v2.Options{Parse: false})
	tag.SetTitle("init")
	tag.Save()
	tag.Close()

	data, _ := os.ReadFile(p)
	return data
}

// makeMinimalFLAC creates a minimal valid FLAC file with a STREAMINFO block
// and a dummy frame sync so the go-flac parser doesn't panic on empty stream data.
func makeMinimalFLAC() []byte {
	var buf bytes.Buffer
	buf.WriteString("fLaC")

	// STREAMINFO block header: last-metadata-block=1, type=0, length=34
	buf.WriteByte(0x80)                 // last block flag + type 0 (STREAMINFO)
	buf.Write([]byte{0x00, 0x00, 0x22}) // length = 34

	// STREAMINFO data (34 bytes)
	si := make([]byte, 34)
	binary.BigEndian.PutUint16(si[0:2], 4096) // min block size
	binary.BigEndian.PutUint16(si[2:4], 4096) // max block size
	si[10] = 0xAC                             // sample rate high bits (44100)
	si[11] = 0x44
	si[12] = byte((44100&0xF)<<4) | byte(1<<1) | byte(15>>4) // rate low | channels-1 | bps-1 high
	si[13] = byte(15&0xF) << 4
	buf.Write(si)

	// Dummy frame data starting with sync code (0xFFF8) so readFLACStream doesn't panic
	buf.Write([]byte{0xFF, 0xF8, 0x00, 0x00})

	return buf.Bytes()
}

func TestTagMP3_AllFields(t *testing.T) {
	dir := t.TempDir()
	mp3Path := filepath.Join(dir, "test.mp3")
	if err := os.WriteFile(mp3Path, makeMinimalMP3(), 0644); err != nil {
		t.Fatal(err)
	}

	meta := TrackMeta{
		Title:       "Airbag",
		Artist:      "Radiohead",
		Album:       "OK Computer",
		TrackNumber: 1,
		DiscNumber:  1,
		Year:        1997,
	}
	if err := tagMP3(mp3Path, meta); err != nil {
		t.Fatal(err)
	}

	tag, err := id3v2.Open(mp3Path, id3v2.Options{Parse: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tag.Close()

	if tag.Title() != "Airbag" {
		t.Errorf("title = %q, want %q", tag.Title(), "Airbag")
	}
	if tag.Artist() != "Radiohead" {
		t.Errorf("artist = %q, want %q", tag.Artist(), "Radiohead")
	}
	if tag.Album() != "OK Computer" {
		t.Errorf("album = %q, want %q", tag.Album(), "OK Computer")
	}
	if tag.Year() != "1997" {
		t.Errorf("year = %q, want %q", tag.Year(), "1997")
	}
}

// TestTagMP3_PreservesForeignFrames proves the non-destructive re-tag: frames
// written by other tools (Music Assistant / Picard — ISRC, AcoustID, and by the
// same mechanism the MusicBrainz recording-id UFID) survive, while Crate's own
// fields are replaced rather than duplicated, and no crate: comment is written.
func TestTagMP3_PreservesForeignFrames(t *testing.T) {
	dir := t.TempDir()
	mp3Path := filepath.Join(dir, "test.mp3")
	if err := os.WriteFile(mp3Path, makeMinimalMP3(), 0644); err != nil {
		t.Fatal(err)
	}

	seed, err := id3v2.Open(mp3Path, id3v2.Options{Parse: true})
	if err != nil {
		t.Fatal(err)
	}
	seed.SetTitle("Stale Title")
	seed.AddTextFrame("TSRC", id3v2.EncodingUTF8, "USRC17607839")
	seed.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{
		Encoding:    id3v2.EncodingUTF8,
		Description: "Acoustid Id",
		Value:       "acoustid-uuid-1234",
	})
	if err := seed.Save(); err != nil {
		t.Fatal(err)
	}
	seed.Close()

	meta := TrackMeta{Title: "New Title", Artist: "Artist", Album: "Album", TrackNumber: 1, Year: 2020}
	if err := tagMP3(mp3Path, meta); err != nil {
		t.Fatal(err)
	}

	tag, err := id3v2.Open(mp3Path, id3v2.Options{Parse: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tag.Close()

	if got := tag.GetTextFrame("TSRC").Text; got != "USRC17607839" {
		t.Errorf("TSRC (ISRC) = %q, want it preserved", got)
	}
	if txxx := tag.GetFrames(tag.CommonID("User defined text information frame")); len(txxx) == 0 {
		t.Error("TXXX (Acoustid Id) frame did not survive re-tag")
	}
	if tag.Title() != "New Title" {
		t.Errorf("title = %q, want New Title (stale title should be replaced, not duplicated)", tag.Title())
	}
	if c := tag.GetFrames(tag.CommonID("Comments")); len(c) != 0 {
		t.Errorf("expected no COMM frame (crate: tag dropped), got %d", len(c))
	}
}

func TestTagFLAC_AllFields(t *testing.T) {
	dir := t.TempDir()
	flacPath := filepath.Join(dir, "test.flac")
	if err := os.WriteFile(flacPath, makeMinimalFLAC(), 0644); err != nil {
		t.Fatal(err)
	}

	meta := TrackMeta{
		Title:       "Airbag",
		Artist:      "Radiohead",
		Album:       "OK Computer",
		TrackNumber: 1,
		DiscNumber:  1,
		Year:        1997,
	}
	if err := tagFLAC(flacPath, meta); err != nil {
		t.Fatal(err)
	}

	f, err := flac.ParseFile(flacPath)
	if err != nil {
		t.Fatal(err)
	}

	for _, block := range f.Meta {
		if block.Type != flac.VorbisComment {
			continue
		}
		cmt, err := flacvorbis.ParseFromMetaDataBlock(*block)
		if err != nil {
			t.Fatal(err)
		}

		check := func(field, want string) {
			vals, _ := cmt.Get(field)
			if len(vals) == 0 || vals[0] != want {
				t.Errorf("%s = %v, want %q", field, vals, want)
			}
		}
		check("TITLE", "Airbag")
		check("ARTIST", "Radiohead")
		check("ALBUM", "OK Computer")
		check("TRACKNUMBER", "1")
		check("DISCNUMBER", "1")
		check("DATE", "1997")
	}
}

// TestTagFLAC_PreservesForeignTags is the critical-path regression guard: the
// library is FLAC, and Music Assistant writes ReplayGain + the MusicBrainz
// recording id into Vorbis comments. Crate must overwrite only its own fields
// and leave everything else intact.
func TestTagFLAC_PreservesForeignTags(t *testing.T) {
	dir := t.TempDir()
	flacPath := filepath.Join(dir, "test.flac")
	if err := os.WriteFile(flacPath, makeMinimalFLAC(), 0644); err != nil {
		t.Fatal(err)
	}

	// Seed tags another tool (Music Assistant) would write, plus a stale title
	// Crate should replace without duplicating.
	f, err := flac.ParseFile(flacPath)
	if err != nil {
		t.Fatal(err)
	}
	seed := flacvorbis.New()
	seed.Add("TITLE", "Stale Title")
	seed.Add("REPLAYGAIN_TRACK_GAIN", "-5.30 dB")
	seed.Add("MUSICBRAINZ_TRACKID", "11111111-2222-3333-4444-555555555555")
	seedBlock := seed.Marshal()
	f.Meta = append(f.Meta, &seedBlock)
	if err := f.Save(flacPath); err != nil {
		t.Fatal(err)
	}

	meta := TrackMeta{Title: "New Title", Artist: "Artist", Album: "Album", TrackNumber: 1, Year: 2020}
	if err := tagFLAC(flacPath, meta); err != nil {
		t.Fatal(err)
	}

	f2, err := flac.ParseFile(flacPath)
	if err != nil {
		t.Fatal(err)
	}
	var cmt *flacvorbis.MetaDataBlockVorbisComment
	for _, block := range f2.Meta {
		if block.Type == flac.VorbisComment {
			cmt, err = flacvorbis.ParseFromMetaDataBlock(*block)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if cmt == nil {
		t.Fatal("no vorbis comment block after tagging")
	}
	get := func(field string) []string {
		v, _ := cmt.Get(field)
		return v
	}

	if rg := get("REPLAYGAIN_TRACK_GAIN"); len(rg) != 1 || rg[0] != "-5.30 dB" {
		t.Errorf("REPLAYGAIN_TRACK_GAIN = %v, want [-5.30 dB] preserved", rg)
	}
	if mb := get("MUSICBRAINZ_TRACKID"); len(mb) != 1 || mb[0] != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("MUSICBRAINZ_TRACKID = %v, want the seeded UUID preserved", mb)
	}
	if title := get("TITLE"); len(title) != 1 || title[0] != "New Title" {
		t.Errorf("TITLE = %v, want [New Title] (stale replaced, not duplicated)", title)
	}
	if c := get("COMMENT"); len(c) != 0 {
		t.Errorf("expected no COMMENT field (crate: tag dropped), got %v", c)
	}
}

func TestTagDispatch(t *testing.T) {
	dir := t.TempDir()
	meta := TrackMeta{Title: "DispatchTitle", Artist: "A", Album: "Al", TrackNumber: 1, Year: 2001}

	// WAV via Tag()
	wavPath := filepath.Join(dir, "test.wav")
	os.WriteFile(wavPath, makeMinimalWAV(), 0644)
	if err := Tag(wavPath, meta); err != nil {
		t.Fatalf("Tag(.wav) failed: %v", err)
	}
	data, _ := os.ReadFile(wavPath)
	if !bytes.Contains(data, []byte("DispatchTitle")) {
		t.Error("Tag(.wav) should write the title")
	}

	// MP3 via Tag()
	mp3Path := filepath.Join(dir, "test.mp3")
	os.WriteFile(mp3Path, makeMinimalMP3(), 0644)
	if err := Tag(mp3Path, meta); err != nil {
		t.Fatalf("Tag(.mp3) failed: %v", err)
	}
	tag, _ := id3v2.Open(mp3Path, id3v2.Options{Parse: true})
	defer tag.Close()
	if tag.Title() != "DispatchTitle" {
		t.Error("Tag(.mp3) should write the title frame")
	}

	// FLAC via Tag()
	flacPath := filepath.Join(dir, "test.flac")
	os.WriteFile(flacPath, makeMinimalFLAC(), 0644)
	if err := Tag(flacPath, meta); err != nil {
		t.Fatalf("Tag(.flac) failed: %v", err)
	}
	f, _ := flac.ParseFile(flacPath)
	var found bool
	for _, block := range f.Meta {
		if block.Type == flac.VorbisComment {
			cmt, _ := flacvorbis.ParseFromMetaDataBlock(*block)
			vals, _ := cmt.Get("TITLE")
			for _, v := range vals {
				if v == "DispatchTitle" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("Tag(.flac) should write the TITLE vorbis field")
	}
}
