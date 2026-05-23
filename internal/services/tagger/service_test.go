package tagger

import (
	"bytes"
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
