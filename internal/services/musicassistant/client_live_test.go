package musicassistant

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestLiveSmoke is an opt-in, read-only check against a real Music Assistant
// server. It runs only when MA_LIVE_URL and MA_LIVE_TOKEN are set, so it never
// runs in CI. It authenticates and lists playlists — no writes, no sync, no
// creates.
func TestLiveSmoke(t *testing.T) {
	rawURL := os.Getenv("MA_LIVE_URL")
	token := os.Getenv("MA_LIVE_TOKEN")
	if rawURL == "" || token == "" {
		t.Skip("set MA_LIVE_URL and MA_LIVE_TOKEN to run the live smoke test")
	}
	wsURL, err := normalizeWSURL(rawURL)
	if err != nil {
		t.Fatalf("normalizeWSURL: %v", err)
	}
	c := &Client{wsURL: wsURL, token: token}

	ready := make(chan struct{}, 1)
	c.SetReadyHandler(func(context.Context) {
		select {
		case ready <- struct{}{}:
		default:
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	go c.Run(ctx)

	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("did not connect + authenticate in time")
	}

	res, err := c.Command(ctx, "music/playlists/library_items", map[string]any{"limit": 10})
	if err != nil {
		t.Fatalf("list playlists: %v", err)
	}
	var playlists []struct {
		Name       string `json:"name"`
		URI        string `json:"uri"`
		IsEditable bool   `json:"is_editable"`
	}
	if err := json.Unmarshal(res, &playlists); err != nil {
		t.Fatalf("decode playlists: %v", err)
	}
	if len(playlists) == 0 {
		t.Fatal("expected at least one playlist")
	}
	for _, p := range playlists {
		t.Logf("playlist %-34q editable=%v  %s", p.Name, p.IsEditable, p.URI)
	}
}
