package musicassistant

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/TheOutdoorProgrammer/crate/internal/db"
)

// Rejecter rejects a Crate track by id (delete file, blacklist source, re-queue).
// Implemented by reject.Service.
type Rejecter interface {
	Reject(id int64) error
}

// RejectWatcher lets a track be marked bad from the Music Assistant app: the
// user drops it into a dedicated "reject" playlist, and this watches that
// playlist (event-driven, via MEDIA_ITEM_UPDATED) and rejects each track it can
// map back to a Crate library file, then removes it from the playlist.
type RejectWatcher struct {
	client       *Client
	queries      *db.Queries
	rejecter     Rejecter
	playlistName string

	mu          sync.Mutex
	playlistID  string // MA library playlist db id, resolved on ready
	playlistURI string // library://playlist/<id> — the event object_id to match

	trigger chan struct{}
}

// NewRejectWatcher wires the watcher's handlers onto the client. Call before the
// client's Run. The reject playlist name comes from settings (default
// DefaultRejectPlaylist).
func NewRejectWatcher(client *Client, queries *db.Queries, rejecter Rejecter) *RejectWatcher {
	name, _ := queries.GetSetting(SettingRejectPlaylist)
	if strings.TrimSpace(name) == "" {
		name = DefaultRejectPlaylist
	}
	w := &RejectWatcher{
		client:       client,
		queries:      queries,
		rejecter:     rejecter,
		playlistName: name,
		trigger:      make(chan struct{}, 1),
	}
	client.SetEventHandler(w.onEvent)
	client.SetReadyHandler(w.onReady)
	return w
}

// Run drives the reconcile loop until ctx is cancelled. Call in a goroutine.
func (w *RejectWatcher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.trigger:
			w.reconcile(ctx)
		}
	}
}

// onReady resolves (creating if needed) the reject playlist and kicks off a
// reconcile to catch anything added while disconnected. Runs after each
// (re)authentication.
func (w *RejectWatcher) onReady(ctx context.Context) {
	if err := w.resolvePlaylist(ctx); err != nil {
		slog.Warn("music-assistant: reject playlist unavailable", "name", w.playlistName, "error", err)
		return
	}
	w.signal()
}

// onEvent runs on the read loop, so it stays cheap: filter for our playlist and
// signal a reconcile. It must not block.
func (w *RejectWatcher) onEvent(e Event) {
	if e.Event != EventMediaItemUpdated {
		return
	}
	w.mu.Lock()
	uri := w.playlistURI
	w.mu.Unlock()
	if uri != "" && e.ObjectID == uri {
		w.signal()
	}
}

func (w *RejectWatcher) signal() {
	select {
	case w.trigger <- struct{}{}:
	default: // a reconcile is already queued
	}
}

func (w *RejectWatcher) resolvePlaylist(ctx context.Context) error {
	res, err := w.client.Command(ctx, "music/playlists/library_items", map[string]any{"limit": 500})
	if err != nil {
		return err
	}
	var playlists []maPlaylist
	if err := json.Unmarshal(res, &playlists); err != nil {
		return err
	}
	for _, p := range playlists {
		if strings.EqualFold(p.Name, w.playlistName) {
			w.setPlaylist(p.ItemID, p.URI)
			return nil
		}
	}
	// Not found — auto-create it (builtin provider, track playlist).
	created, err := w.client.Command(ctx, "music/playlists/create_playlist", map[string]any{"name": w.playlistName})
	if err != nil {
		return fmt.Errorf("create playlist: %w", err)
	}
	var p maPlaylist
	if err := json.Unmarshal(created, &p); err != nil {
		return err
	}
	slog.Info("music-assistant: created reject playlist", "name", w.playlistName, "uri", p.URI)
	w.setPlaylist(p.ItemID, p.URI)
	return nil
}

func (w *RejectWatcher) setPlaylist(id, uri string) {
	w.mu.Lock()
	w.playlistID, w.playlistURI = id, uri
	w.mu.Unlock()
}

func (w *RejectWatcher) reconcile(ctx context.Context) {
	w.mu.Lock()
	id := w.playlistID
	w.mu.Unlock()
	if id == "" {
		return
	}

	res, err := w.client.Command(ctx, "music/playlists/playlist_tracks", map[string]any{
		"item_id":                        id,
		"provider_instance_id_or_domain": "library",
	})
	if err != nil {
		slog.Warn("music-assistant: fetch reject playlist tracks failed", "error", err)
		return
	}
	var tracks []maTrack
	if err := json.Unmarshal(res, &tracks); err != nil {
		slog.Warn("music-assistant: decode reject tracks failed", "error", err)
		return
	}

	var removePositions []int
	rejected := 0
	for _, t := range tracks {
		path := t.filesystemPath()
		if path == "" {
			slog.Debug("music-assistant: reject track has no filesystem mapping, leaving it", "name", t.Name)
			continue
		}
		track, err := w.queries.FindTrackByPath(path)
		if err != nil {
			slog.Debug("music-assistant: reject track not owned by crate, leaving it", "path", path)
			continue
		}
		if err := w.rejecter.Reject(track.ID); err != nil {
			slog.Warn("music-assistant: reject failed", "path", path, "error", err)
			continue
		}
		slog.Info("music-assistant: rejected track via playlist", "path", path)
		rejected++
		if t.Position != nil {
			removePositions = append(removePositions, *t.Position)
		}
	}

	if len(removePositions) > 0 {
		if _, err := w.client.Command(ctx, "music/playlists/remove_playlist_tracks", map[string]any{
			"db_playlist_id":      id,
			"positions_to_remove": removePositions,
		}); err != nil {
			slog.Warn("music-assistant: remove-from-reject-playlist failed", "error", err)
		}
	}
	if rejected > 0 {
		// Re-sync so MA drops the files we just deleted.
		w.client.TriggerScan(ctx)
	}
}

// maPlaylist is the slice of a MA playlist object the watcher needs.
type maPlaylist struct {
	ItemID string `json:"item_id"`
	Name   string `json:"name"`
	URI    string `json:"uri"`
}

// maTrack is the slice of a MA playlist track the watcher needs.
type maTrack struct {
	Name             string              `json:"name"`
	Position         *int                `json:"position"`
	ProviderMappings []maProviderMapping `json:"provider_mappings"`
}

type maProviderMapping struct {
	ItemID         string `json:"item_id"`
	ProviderDomain string `json:"provider_domain"`
}

// filesystemPath returns the track's path relative to the shared library root
// (the same relative path Crate stores in tracks.file_path), taken from its
// filesystem provider mapping — or "" if the track isn't a local file.
func (t maTrack) filesystemPath() string {
	for _, m := range t.ProviderMappings {
		if strings.HasPrefix(m.ProviderDomain, "filesystem") {
			return m.ItemID
		}
	}
	return ""
}
