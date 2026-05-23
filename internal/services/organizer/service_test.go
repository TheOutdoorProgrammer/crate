package organizer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSanitize(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"OK Computer", "OK Computer"},
		{`AC/DC`, "AC_DC"},
		{`What: "Is" This?`, "What_ _Is_ This_"},
		{"  spaces  ", "spaces"},
		{"a<b>c|d*e", "a_b_c_d_e"},
		{`back\slash`, "back_slash"},
	}
	for _, tt := range tests {
		got := sanitize(tt.in)
		if got != tt.want {
			t.Errorf("sanitize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFindFile(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "complete", "user")
	os.MkdirAll(sub, 0755)
	target := filepath.Join(sub, "song.flac")
	os.WriteFile(target, []byte("data"), 0644)

	found, err := findFile(dir, "song.flac")
	if err != nil {
		t.Fatal(err)
	}
	if found != target {
		t.Errorf("findFile = %q, want %q", found, target)
	}
}

func TestFindFileNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := findFile(dir, "nonexistent.flac")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "dst.bin")

	data := make([]byte, 2<<20)
	for i := range data {
		data[i] = byte(i % 251)
	}
	os.WriteFile(src, data, 0644)

	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(data) {
		t.Fatalf("copied %d bytes, want %d", len(got), len(data))
	}
	for i := range data {
		if got[i] != data[i] {
			t.Fatalf("mismatch at byte %d", i)
			break
		}
	}
}

func TestCopyFileSrcMissing(t *testing.T) {
	dir := t.TempDir()
	err := copyFile(filepath.Join(dir, "nope"), filepath.Join(dir, "dst"))
	if err == nil {
		t.Error("expected error for missing source")
	}
}
