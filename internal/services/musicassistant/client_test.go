package musicassistant

import (
	"encoding/json"
	"testing"
)

func TestNormalizeWSURL(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{"https://music.example.com", "wss://music.example.com/ws", false},
		{"http://192.168.1.5:8095", "ws://192.168.1.5:8095/ws", false},
		{"wss://ma.example.com/ws", "wss://ma.example.com/ws", false},
		{"https://music.example.com/", "wss://music.example.com/ws", false},
		{"music.example.com", "", true}, // no scheme
		{"ftp://host", "", true},        // unsupported scheme
	}
	for _, c := range cases {
		got, err := normalizeWSURL(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("normalizeWSURL(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeWSURL(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("normalizeWSURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMATrackFilesystemPath(t *testing.T) {
	// A real filesystem track (as MA returns it): the library-relative path lives
	// in the filesystem provider mapping's item_id — the same path Crate stores.
	raw := `{
		"name": "Blue Strips",
		"position": 3,
		"provider_mappings": [
			{"item_id": "Jessie Murph/Blue Strips (2025)/01 - Blue Strips.flac", "provider_domain": "filesystem_local"}
		]
	}`
	var tr maTrack
	if err := json.Unmarshal([]byte(raw), &tr); err != nil {
		t.Fatal(err)
	}
	if got := tr.filesystemPath(); got != "Jessie Murph/Blue Strips (2025)/01 - Blue Strips.flac" {
		t.Errorf("filesystemPath() = %q, want the library-relative path", got)
	}
	if tr.Position == nil || *tr.Position != 3 {
		t.Errorf("Position = %v, want 3", tr.Position)
	}

	// A streaming (non-filesystem) track has no local file to map/reject.
	raw2 := `{"name":"X","provider_mappings":[{"item_id":"spotify--abc","provider_domain":"spotify"}]}`
	var tr2 maTrack
	if err := json.Unmarshal([]byte(raw2), &tr2); err != nil {
		t.Fatal(err)
	}
	if got := tr2.filesystemPath(); got != "" {
		t.Errorf("filesystemPath() = %q, want empty for non-filesystem track", got)
	}
}
