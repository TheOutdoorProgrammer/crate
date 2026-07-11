# Crate

Self-hosted music manager (Lidarr alternative). Search for artists via pluggable gRPC providers (MusicBrainz, Deezer, or custom), watch their discographies, and automatically download tracks via slskd (Soulseek).

## Architecture

```
cmd/crate/main.go              Entry point. Wires services, starts HTTP server + provider processes.
cmd/provider-musicbrainz/       Standalone gRPC server for MusicBrainz API
cmd/provider-deezer/            Standalone gRPC server for Deezer API
proto/provider/                 Protobuf service definition + generated Go code
internal/
  api/                          HTTP handlers (chi router), SPA serving
  activity/                     Activity log (separate SQLite: activity.db)
  cache/                        SQLite-backed cache (cache.db) with TTL
  config/                       Env-based config (CRATE_* vars)
  db/                           SQLite via modernc.org/sqlite, goose migrations, raw SQL queries
  migrations/                   Embedded .sql migration files
  library/                      Shared track-path helpers (ResolvePath, Contains) — all file_path resolution goes through here
  models/                       Shared structs and status enums
  naming/                       Library path templates: parse/validate/render ({artist}/{album}/...)
  provider/
    manager.go                  Provider registry, search+enrichment, caching, health checks
    process.go                  Child process management for built-in providers
  services/
    slskd/                      slskd API client (Soulseek daemon)
    downloader/                 Background download queue processor (tick every 10s)
    scheduler/                  Background periodic jobs (new release detection, auto-queue, quality upgrades)
    importer/                   Library import: tag-based scan of existing files into the DB
    organizer/                  Moves downloaded files into library using the naming template
    tagger/                     Non-destructive ID3/FLAC/WAV tagging + cover art (preserves foreign tags)
    navidrome/                  Optional Navidrome integration (triggers library scan after download)
    musicassistant/             Optional Music Assistant integration (ws client, sync notifier, reject-playlist watcher)
    reject/                     Shared track-reject logic (delete file, blacklist, re-queue) — used by the API and the MA watcher
web/                            React frontend (Vite, Tailwind, React Query, React Router)
docs/adr/                       Architecture Decision Records
```

## Provider Architecture

Crate uses gRPC to communicate with music metadata providers. The Docker image ships with MusicBrainz and Deezer providers as child processes managed by the main crate binary.

```
crate (main process)
  ├── Provider Manager (routes requests, caches, enriches)
  │   ├── gRPC → provider-musicbrainz (port 50051, 1 req/s rate limit)
  │   ├── gRPC → provider-deezer (port 50052, 10 req/s rate limit)
  │   └── gRPC → any external provider
  ├── Scheduler (new release detection via entity's stored provider)
  ├── Downloader (slskd integration)
  └── HTTP API (provider-unaware frontend)
```

Key concepts:
- **Default provider**: used for search and browse (configurable in settings, default: musicbrainz). Users can switch providers on the fly from the search UI.
- **Provider tracking**: each entity (artist/album/track) stores which provider+ID it came from
- **Orphan detection**: entities whose provider is unhealthy show as "orphaned" in the UI
- **Relink**: any entity can be relinked to a different provider at any time
- **Cache**: separate SQLite DB (cache.db) with configurable TTL, clearable from settings

Provider config format: `CRATE_PROVIDERS=name:binary:port,name2:binary2:port2`
For external providers: `CRATE_PROVIDERS=spotify:external:192.168.1.10:50053`

## Data flow

1. User searches for an artist (via selected provider's gRPC API, switchable on the fly)
2. User watches an artist (full discography), album, or individual track
3. Watched items saved to SQLite with provider + provider_id
4. Scheduler (configurable interval, default 6h) checks each watched artist's provider for new releases
5. Downloader processes queue: search slskd → pick best file → download → organize → tag (non-destructive: only Crate's own fields, foreign tags preserved) → notify (Navidrome / Music Assistant rescan)
6. Track status: `wanted` → `downloading` → `owned`

## Download Retry, Blacklist & Shadow Bans

- **No results**: immediate fail, no retry. Track stays "wanted" for the scheduler's next cycle.
- **Transfer failures** (rejected, errored, cancelled): the (username, filename) pair is blacklisted in `slskd_blacklist` table. Future searches skip that source.
- **Stale transfers**: state-aware timeouts detect stalled downloads. InProgress/Initializing = 5min, Queued = 30min, Requested = 10min. Active transfer stalls blacklist the file; queued/requested stalls trigger a shadow ban on the user.
- **Shadow bans (cooldowns)**: temporary per-user blocks stored in `user_cooldowns` table. Triggered by stale queued transfers or StartDownload failures (e.g. user offline). Duration is configurable via `shadow_ban_duration_minutes` setting (default 60min). Expired cooldowns are auto-purged on the scheduler's daily integrity tick. `scoreCandidates` skips cooled-down users entirely.
- **Retry backoff**: 5m → 15m → 30m → 1h. After 4 attempts (~2h cumulative), permanently fails. Track reverts to "wanted".
- **Blacklist is per-file-per-user**: a user can be blacklisted for one file but not others. Shadow bans are per-user (all files blocked during cooldown).
- **API management**: `GET/DELETE /api/blacklist/{id}`, `GET/DELETE /api/cooldowns/{id}` for viewing and removing entries. Also exposed in the Settings UI under "Blocked Sources".
- **Track rejection**: the shared logic lives in `internal/services/reject` (`Reject(id)` / `RejectTrack(track)`) — deletes the file (via `library.DeleteFile`), blacklists the source if `downloaded_from` and `downloaded_filename` are both known, resets track to wanted, and re-enqueues for download. Callers trigger the rescan afterward. Three triggers: `POST /api/tracks/{id}/reject` (by Crate track ID), `POST /api/tracks/reject` with `{"artist":"...","title":"..."}` (by name), and the **Music Assistant reject-playlist watcher** (see Music Assistant Integration). Tracks downloaded before v1.11.0 won't have `downloaded_filename` — reject still works but skips the blacklist step.

## Manual Search (async)

Manual search is async — the frontend starts a search, then polls for results as peers respond. See [ADR-0002](docs/adr/0002-async-manual-search.md).

1. `POST /api/tracks/{id}/search` → `{search_id, track_id}` — starts slskd search, returns immediately
2. `GET /api/tracks/{id}/search/{searchId}` → `{results: [...], is_complete: bool, file_count: int}` — poll for scored results. Results are **unfiltered** (every slskd file — see [ADR-0003](docs/adr/0003-manual-search-no-filter.md)); each carries `score` plus `blacklisted`/`locked`/`negative_match` annotation flags.
3. `DELETE /api/tracks/{id}/search/{searchId}` — cleanup when done

Frontend polls every 2s, shows results as they arrive with a spinner until `is_complete`. Cleanup runs on unmount and when the user closes the search panel.

## Pagination

- Search API: `GET /api/search?q=...&limit=25&offset=0` returns `{artists: [...], total: N}`
- Activity API: `GET /api/activity?limit=50&offset=0` returns `{items: [...], total: N}`
- Frontend uses "Load More" buttons for both

## Scoring System

All file selection goes through `scoreCandidates()` in `internal/services/downloader/service.go`. Score components:

- **Quality (0-100)**: tier-based from user's priority list. Tier 0 = 100, Tier 1 = 75, Tier 2 = 50, etc. (min 25, gap of 25 per tier). If no tiers configured, uses fallback scoring. Fallback scores are capped below the lowest tier.
- **Artist matching**: auto-downloads require both artist name and title in the file path — no fallback. Manual search applies **no** content filter and returns every slskd result for the user to pick (artist is only a +20 ranking bonus). See [ADR-0001](docs/adr/0001-artist-matching-fallback.md) and [ADR-0003](docs/adr/0003-manual-search-no-filter.md).
- **Artist bonus (+20)**: in manual search results, artist name in filename adds +20. Kept below the tier gap (25) so quality always dominates between tiers.
- **Free slot bonus (+10)**: if user has a free upload slot (instant start).
- **Queue score (0-15)**: `15 / (1 + queueLength)`. Empty queue = 15, decays toward 0.

Design invariants (enforced by `TestScoringBalance`):
- Same availability → higher tier always wins
- Artist bonus alone cannot flip a tier (20 < 25 gap)
- All bonuses combined (max 45) can overcome one tier gap but not two (50)
- Fallback formats lose to configured tiers at equal availability

The `quality_fallback_enabled` setting (default true) controls whether files outside configured tiers are accepted at all.

**Negative keywords**: user-configurable list stored as JSON array in the `negative_keywords` setting (e.g. `["acapella", "instrumental"]`). Auto-downloads skip files whose filename matches any keyword. Manual search still returns them but marks `negative_match: true` so the UI can dim them. Case-insensitive matching against the lowercased filename.

## Quality Upgrades

- Priority-ordered quality tiers stored as JSON in settings (`quality_tiers`), default: FLAC > MP3 320 > MP3 256
- `download_format` and `download_bitrate` recorded on tracks at search time (when the slskd result is picked)
- Scheduler scans one artist per day (round-robin via `upgrade_last_artist_id` setting), re-queues owned tracks that can be upgraded to a higher-priority tier
- `QualityTierRank()` and `IsUpgradeable()` in `internal/services/downloader/` handle tier ranking

## Library Naming Template

File/folder layout is templated, not hardcoded (issue #1). All logic lives in `internal/naming` (pure, no I/O): `Validate(template)`, `Render(template, Meta)`, `DefaultTemplate`, and `SettingKey` (`naming_template` in the settings table; empty/missing = default).

- Tokens: `{artist}`/`{albumartist}` (identical — Crate only models album artists), `{album}`, `{year}`, `{track}`, `{disc}`, `{title}`. Numeric pad: `{track:2}`. Extension is appended by the organizer, never templated.
- Empty tokens (`{year}`/`{disc}` when unknown) use a `\x00` marker so cleanup strips *template* decoration (empty parens/brackets, dangling separators) without touching the same characters in real metadata (e.g. Sigur Rós's album "( )").
- Segments are sanitized after rendering (`<>:"/\|?*` → `_`), so unsafe chars in template literals are neutralized too. If no token in a segment rendered empty, whitespace is preserved byte-for-byte — the default template must produce byte-identical paths to the old hardcoded layout.
- Validation: relative paths only, no `.`/`..` segments, last segment must contain `{title}` or `{track}`, unknown tokens rejected. Enforced in `handleUpdateSettings` (validate-all-before-save) and defensively at render time (organizer falls back to `DefaultTemplate` with `slog.Error`).
- `GET /api/settings/naming-preview?template=...` renders sample metadata for the Settings UI live preview (debounced 350ms client-side).
- Template changes apply to new downloads only — existing files are never renamed. The organizer captures the DB-stored `file_path` before moving (the downloader only overwrites it in memory) and deletes the replaced file when it's inside the library at a different path (quality upgrade after a template change), pruning now-empty dirs. Files outside the library (imported) are never deleted.

## Library Import

`internal/services/importer` adopts an existing on-disk library (issue #1, second half). Tag-based and non-destructive: walks the library dir (or a given path), reads embedded tags (MP3 via id3v2, FLAC via go-flac/flacvorbis — the only formats Crate tags), groups artist→album→track, and records rows with `status = owned`. Files are never moved, renamed, or written to. Async single-flight job: `POST /api/library/import` (`{path?, dry_run}`, 409 when one is running), `GET /api/library/import` for progress/report; UI lives in Settings → Library.

Key invariants:

- **Provider identity**: files with consistent MusicBrainz tags import under `musicbrainz` with real IDs — artist MBID, **release-group** ID for albums, **release-track** ID for tracks (matching the MB provider's namespace in `cmd/provider-musicbrainz`: albums are release-groups, browse tracks are release-tracks). Everything else gets `provider.LocalProvider` ("local") with stable tag-derived hash IDs (`loc-…`, never path-derived). `local` is reserved: always healthy (no orphan badge), rejected by `RegisterProvider`, never routed. Relink is the promotion path to a real provider.
- **Reuse before create**: artists match by (provider, provider_id) then case-insensitive name; albums by provider then title under the artist; tracks by provider then title within the album. A wanted track matching an imported file is **claimed** (flips to owned with the file path) — its provider identity is never rewritten.
- **Quality data is real**: `download_format` from the file, `download_bitrate` parsed from MP3 frame headers (Xing/VBR aware, `internal/services/importer/mp3info.go`) or 0 for FLAC (lossless convention). Without this, `IsUpgradeable` would treat every imported track as upgradeable and re-download the entire library one artist per day.
- **file_path convention**: relative when inside the library dir, absolute otherwise. All resolution goes through `internal/library` (`ResolvePath`/`Contains`/`DeleteFile`) — used by the organizer, reject, the scheduler's integrity check, and the importer. Destructive operations require `Contains` (files outside the library are never deleted).
- **Recording id capture**: files carrying a MusicBrainz *recording* id (FLAC `MUSICBRAINZ_TRACKID` / MP3 `UFID`) store it in `tracks.mb_recording_id`; the importer prefers it (album-scoped) as the highest-confidence match. See "MusicBrainz recording id".
- Dry-run runs the identical code path with writes skipped, so its counts are exact. Re-runs are idempotent. Skipped files (missing artist/album/title) are reported with reasons, capped at 100 samples.

## Navidrome Integration

- Optional: configure `navidrome_url`, `navidrome_user`, `navidrome_password` in settings
- After each successful download+organize, triggers `startScan` via the Subsonic API
- Auth uses token+salt scheme: `token = md5(password + random_salt)`, both sent as query params
- Implemented as a `PostDownloadNotifier` interface — extensible for other integrations
- Does nothing if settings are not configured (all three fields required)
- **Music Assistant is a second `PostDownloadNotifier`** (see below). Both fire when configured; keeping Navidrome is free.

## Metadata Tagging

`internal/services/tagger` writes ID3v2 (MP3), Vorbis comments (FLAC), and RIFF INFO (WAV) — title, artist, album, track, disc, year, and cover art (MP3/FLAC).

**Non-destructive.** The tagger reads the file's existing tags and overwrites *only* Crate's own fields, preserving everything else — ReplayGain, MusicBrainz/AcoustID/ISRC identifiers, and any other foreign tags. This matters because Music Assistant's analysis providers write ReplayGain and a MusicBrainz recording id back into the same files; the old "rebuild a fresh tag block" behavior silently wiped them. FLAC seeds a comment block from the existing one minus Crate's keys; MP3 opens with `Parse:true` and `DeleteFrames` only Crate's own frames before re-adding (so pre-tagged Soulseek files don't accumulate dupes). Regression tests in `service_test.go` prove foreign tags survive a re-tag.

> Historical: Crate used to write a `crate:{track_id}` comment tag so the (retired) Haystack app could bridge back by ID. Dropped — the MA reject watcher maps by file path instead.

## MusicBrainz recording id

The importer captures the MusicBrainz **recording** id (FLAC `MUSICBRAINZ_TRACKID` / MP3 `UFID` owner `http://musicbrainz.org`) into `tracks.mb_recording_id` (migration 010). This is the fingerprint-verified identity Music Assistant's `acoustid_lookup` provider writes — distinct from the **release-track** id Crate's `musicbrainz` provider uses (a recording is release-independent). Stored as a separate signal and used, album-scoped, as the highest-confidence importer match (`FindTrackByAlbumRecordingID`, tried before release-track and title matching). It never overrides album/release structure — a recording id can't place a track on a specific release. (Needs MA's `acoustid_lookup` `write_tags_back` option enabled for the id to land in files.)

## Music Assistant Integration

Optional, in `internal/services/musicassistant`. Configured via settings `music_assistant_url` + `music_assistant_token` (both required; `music_assistant_reject_playlist` optional, default `Crate Reject`). When unconfigured, `NewClient` returns nil — nothing connects, no goroutine runs. **Enabling it needs a restart** (a persistent connection can't hot-start without an idle settings-polling goroutine, which would violate "nothing runs when unconfigured").

One reconnecting websocket connection (`client.go`, `coder/websocket`) to MA's `/ws` endpoint backs two features:

- **Sync notifier** (`notifier.go`): implements `PostDownloadNotifier.TriggerScan` → sends `music/sync` so MA picks up new downloads. Runs *alongside* the Navidrome notifier.
- **Reject watcher** (`rejectwatcher.go`): "mark a track bad from the MA app." MA can't add a per-item action, so the signal is a dedicated **reject playlist** — the user drops a track into it. Event-driven: MA pushes `media_item_updated` for the playlist (`object_id == library://playlist/<id>`); the watcher fetches the playlist's tracks, maps each MA filesystem track back to a Crate track by its shared library-relative path (`FindTrackByPath`), runs the shared reject, removes it from the playlist, and triggers a sync. Auto-creates the playlist if missing; reconciles on (re)connect to catch anything added while disconnected.

Protocol (verified against MA 2.9.x + `music-assistant-models`): connect → server pushes `server_info` → client sends `{"command":"auth","message_id":"…","args":{"token":…}}` → `{"message_id":"…","result":…}`; events arrive as `{"event":"…","object_id":"…","data":…}`. `client.go` handles command-RPC correlation by `message_id`, partial-result merging, event dispatch, and reconnect with backoff. The MA token is redacted in the settings API (`sensitiveSettings`).

## Key concepts

- **Artist status**: `watched` (full discography tracked), `partial` (only some albums/tracks), `owned`
- **Watch granularity**: can watch at artist, album, or track level
- **Partial-to-full upgrade**: watching all albums for a `partial` artist promotes to `watched`
- **Frontend is provider-unaware**: never passes provider names in API calls (except settings page). Backend resolves from settings/DB.
- **Providers return rank + metadata**: providers control display order and can return arbitrary key-value metadata

## Database

SQLite with WAL mode, single connection. Migrations via goose (embedded SQL files). Foreign keys with `ON DELETE CASCADE`. Entities use `provider` + `provider_id` columns (composite index) instead of provider-specific ID columns.

## Build & deploy

```bash
# Backend
go test ./...
go build ./cmd/crate/
go build ./cmd/provider-musicbrainz/
go build ./cmd/provider-deezer/

# Frontend
cd web && npm install && npm run build

# Proto (only if .proto changes)
PATH="$PATH:$HOME/go/bin" protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative proto/provider/provider.proto

# Run locally
CRATE_PROVIDERS=musicbrainz:./provider-musicbrainz:50051,deezer:./provider-deezer:50052 \
CRATE_SLSKD_URL=http://localhost:5030 CRATE_SLSKD_API_KEY=xxx ./crate
```

Docker: multi-stage build, builds all three binaries. Alpine runtime. CI: GitHub Actions builds frontend, runs `go test`, then Docker image.

## Testing

Tests live in:
- `internal/api/handlers_test.go` — API integration tests (100+ tests, includes blacklist/cooldown CRUD, track reject by ID and by name)
- `internal/activity/activity_test.go` — Activity log unit tests
- `internal/services/downloader/service_test.go` — Scoring system (tier-based, queue, cooldown filtering, balance invariants), retry delay, pickBestFile, blacklist, stale timeouts, inferExt
- `internal/services/navidrome/client_test.go` — Navidrome scan trigger and auth tests
- `internal/services/tagger/service_test.go` — tagging per format + non-destructive regression (foreign tags survive a re-tag)
- `internal/services/importer/service_test.go` — library import incl. MusicBrainz recording-id capture
- `internal/services/musicassistant/client_test.go` — URL normalization + MA-track→path mapping; `client_live_test.go` is an opt-in read-only live smoke test (gated on `MA_LIVE_URL`/`MA_LIVE_TOKEN`)

The `testEnv` helper in handlers_test.go wires up real in-memory SQLite, a fake gRPC provider, fake slskd, and an in-memory activity log. Use `newTestEnv(t)` and call `env.do(method, path, body)`. The fake provider returns canned data for artist "1000" with two albums and three tracks.

## Lidarr API Shim

The Lidarr v1 API compatibility shim lives entirely in `internal/api/lidarr.go` (+ `lidarr_test.go`). **Crate is never changed to accommodate Lidarr.** All translation between Lidarr concepts and Crate internals happens inside `lidarr.go`. If Lidarr needs something Crate doesn't expose, the shim adapts — we do not add fields, endpoints, or behaviors to Crate's core code to make Lidarr work. Lidarr compatibility is a convenience, not a requirement.

## Docs maintenance

When adding or changing user-facing features (new settings, API endpoints, scoring changes, download behavior), update all three:
1. `README.md` — features list and configuration table
2. `CLAUDE.md` — technical details for agents (this file)
3. `site/index.html` and `site/docs.html` — marketing site and documentation

The marketing docs at `site/docs.html` include a settings table, API reference, scoring system section, and download flow description that must stay in sync with the code.

## Architecture Decision Records

Design decisions with non-obvious trade-offs are documented as ADRs in `docs/adr/`. Read these before changing the relevant system — they explain *why* something works the way it does and what alternatives were considered.

| ADR | Area | Decision |
|-----|------|----------|
| [0001](docs/adr/0001-artist-matching-fallback.md) | Downloader | Auto-downloads require artist+title (manual-search filtering since removed — see 0003) |
| [0002](docs/adr/0002-async-manual-search.md) | API/Frontend | Async manual search with frontend polling instead of blocking 30s request |
| [0003](docs/adr/0003-manual-search-no-filter.md) | Downloader | Manual search returns every slskd result (scored + annotated, never filtered) |
| [0004](docs/adr/0004-non-destructive-tagging.md) | Tagger | Tagger preserves foreign tags; the `crate:` comment tag was dropped |
| [0005](docs/adr/0005-recording-id-signal.md) | Importer | MusicBrainz recording id stored as a separate signal (not resolved to release-track) |
| [0006](docs/adr/0006-music-assistant-integration.md) | Integrations | Music Assistant added alongside Navidrome; mark-bad-from-app via an event-driven reject playlist |

When making a decision that involves a meaningful trade-off (especially "we tried X but chose Y because Z"), add a new ADR.

## Lessons learned

- **Always add tests for new features.** Run `go test ./...` before pushing. CI gates on this.
- **The `.dockerignore` matters.** If something works locally but fails in Docker, check `.dockerignore` first.
- **The tagger is non-destructive.** It reads existing tags and overwrites only Crate's own fields, so foreign tags (ReplayGain, the MusicBrainz recording id, etc. written by Music Assistant) survive a re-tag. FLAC `Add()` appends, so it filters Crate's keys out of the existing block before re-adding; MP3 uses `Parse:true` + `DeleteFrames`.
- **Frontend should not pass info the backend can resolve.** The backend resolves providers from settings/entity data.
- **Present designs for reaction, don't ask multiple-choice during architecture.** Show one approach and let user redirect.
- **Deezer API needs rate limiting.** 10 req/s. MusicBrainz needs 1 req/s.
- **Cross-device file moves fail with `os.Rename`.** Organizer falls back to copy+delete.
- **CI multiarch builds need QEMU.** Only set multiarch platforms when QEMU is configured (tag builds).
