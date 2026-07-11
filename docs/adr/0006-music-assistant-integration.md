# ADR-0006: Music Assistant Integration

## Status

Accepted (2026-07-10).

## Context

Crate had one "serving layer" hook: a post-download Navidrome scan trigger (a `PostDownloadNotifier`). Music Assistant became the primary library/player, and two things were wanted:

1. Sync MA after downloads (like the Navidrome trigger).
2. **Mark a track bad from the MA app** — the download quality control that previously lived in the (retired) Haystack player.

The constraint: MA has **no extension point to add a per-item action** to its app. Its provider types (music / player / metadata / audio-analysis / plugin) can't contribute a "reject this track" button, and its event bus has no favorite/rate/playlist-add event a plugin could hook. Verified against the MA server source.

## Decision

**Add Music Assistant as a second integration alongside Navidrome, over one reconnecting websocket connection, in `internal/services/musicassistant`.**

- **Keep Navidrome.** The `PostDownloadNotifier` is a slice; MA is added next to it, not in place of it. Crate is public with real users, and Subsonic/Navidrome is far more common in the wild than MA.
- **Sync notifier**: `TriggerScan` → the `music/sync` command.
- **Reject watcher**: the MA-native way to signal intent from the app is a **playlist**. A dedicated "reject" playlist (auto-created) is watched **event-driven** — MA pushes `media_item_updated` for the playlist, and the watcher reconciles: fetch the playlist's tracks, map each MA filesystem track to a Crate track by its shared library-relative path, run the shared reject, remove it from the playlist, and trigger a sync.
- **Reject logic is shared code**, extracted to `internal/services/reject` and used by both the HTTP API and the watcher — one implementation.
- **Nil when unconfigured.** `NewClient` returns nil without `music_assistant_url` + `music_assistant_token`, so nothing connects and no goroutine runs.

## Consequences

- "Mark bad from the app" works via a playlist gesture — clunkier than a real button, but it's the ceiling of what MA allows without forking its frontend.
- The reject flow is single-sourced; the API handlers were refactored onto `internal/services/reject`.
- **Enabling or changing the MA connection requires a restart** (a persistent connection can't hot-start without an idle settings-polling goroutine, which would violate "nothing runs when unconfigured"). Config *values* remain UI-editable.
- Only local library files are rejected; streaming tracks dropped into the playlist are left alone.
- The Go client hand-rolls MA's websocket protocol (command-RPC correlated by `message_id`, event dispatch, reconnect with backoff). One new dependency: `coder/websocket`.

## Alternatives considered

- **An MA provider/plugin.** Can't add a per-item action or hook a reject gesture; dominated by the playlist watcher.
- **A Python sidecar** (reusing existing favs-sync tooling). Faster to prototype, but splits Crate's logic across repos and isn't a first-class Crate feature.
- **Remove Navidrome.** Rejected — breaking change for existing users, and keeping both is free.
- **Poll the reject playlist** instead of event-driven. Rejected — the push event exists (every authenticated ws client receives all events), so polling would just burn cycles.
