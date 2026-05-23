package tagger

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	buf = append(buf, 16, 0, 0, 0) // chunk size
	buf = append(buf, 1, 0)         // PCM
	buf = append(buf, 1, 0)         // 1 channel
	buf = append(buf, 0x44, 0xAC, 0, 0) // 44100 Hz
	buf = append(buf, 0x88, 0x58, 0x01, 0) // byte rate
	buf = append(buf, 2, 0)         // block align
	buf = append(buf, 16, 0)        // bits per sample

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
