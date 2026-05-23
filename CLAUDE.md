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
  models/                       Shared structs and status enums
  provider/
    manager.go                  Provider registry, search+enrichment, caching, health checks
    process.go                  Child process management for built-in providers
  services/
    slskd/                      slskd API client (Soulseek daemon)
    downloader/                 Background download queue processor (tick every 10s)
    scheduler/                  Background periodic jobs (new release detection, auto-queue, quality upgrades)
    organizer/                  Moves downloaded files into library, renames by convention
    tagger/                     ID3/FLAC metadata tagging + cover art embedding
    navidrome/                  Optional Navidrome integration (triggers library scan after download)
web/                            React frontend (Vite, Tailwind, React Query, React Router)
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
5. Downloader processes queue: search slskd → pick best file → download → organize → tag
6. Track status: `wanted` → `downloading` → `owned`

## Download Retry & Blacklist

- **No results**: immediate fail, no retry. Track stays "wanted" for the scheduler's next cycle.
- **Transfer failures** (rejected, errored, cancelled): the (username, filename) pair is blacklisted in `slskd_blacklist` table. Future searches skip that source.
- **Retry backoff**: 5m → 15m → 30m → 1h. After 4 attempts (~2h cumulative), permanently fails. Track reverts to "wanted".
- **Blacklist is per-file-per-user**: a user can be blacklisted for one file but not others. `pickBestFile` checks the blacklist before scoring candidates.

## Pagination

- Search API: `GET /api/search?q=...&limit=25&offset=0` returns `{artists: [...], total: N}`
- Activity API: `GET /api/activity?limit=50&offset=0` returns `{items: [...], total: N}`
- Frontend uses "Load More" buttons for both

## Quality Upgrades

- Priority-ordered quality tiers stored as JSON in settings (`quality_tiers`), default: FLAC > MP3 320 > MP3 256
- `download_format` and `download_bitrate` recorded on tracks at search time (when the slskd result is picked)
- Scheduler scans one artist per day (round-robin via `upgrade_last_artist_id` setting), re-queues owned tracks that can be upgraded to a higher-priority tier
- `QualityTierRank()` and `IsUpgradeable()` in `internal/services/downloader/` handle scoring

## Navidrome Integration

- Optional: configure `navidrome_url`, `navidrome_user`, `navidrome_password` in settings
- After each successful download+organize, triggers `startScan` via the Subsonic API
- Auth uses token+salt scheme: `token = md5(password + random_salt)`, both sent as query params
- Implemented as a `PostDownloadNotifier` interface — extensible for other integrations
- Does nothing if settings are not configured (all three fields required)

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
- `internal/api/handlers_test.go` — API integration tests (50+ tests)
- `internal/activity/activity_test.go` — Activity log unit tests
- `internal/services/downloader/service_test.go` — Retry delay, pickBestFile/blacklist, quality tier scoring tests
- `internal/services/navidrome/client_test.go` — Navidrome scan trigger and auth tests

The `testEnv` helper in handlers_test.go wires up real in-memory SQLite, a fake gRPC provider, fake slskd, and an in-memory activity log. Use `newTestEnv(t)` and call `env.do(method, path, body)`. The fake provider returns canned data for artist "1000" with two albums and three tracks.

## Lessons learned

- **Always add tests for new features.** Run `go test ./...` before pushing. CI gates on this.
- **The `.dockerignore` matters.** If something works locally but fails in Docker, check `.dockerignore` first.
- **FLAC vorbis comment `Add()` appends, not replaces.** Always create a fresh comment block.
- **Frontend should not pass info the backend can resolve.** The backend resolves providers from settings/entity data.
- **Present designs for reaction, don't ask multiple-choice during architecture.** Show one approach and let user redirect.
- **Deezer API needs rate limiting.** 10 req/s. MusicBrainz needs 1 req/s.
- **Cross-device file moves fail with `os.Rename`.** Organizer falls back to copy+delete.
- **CI multiarch builds need QEMU.** Only set multiarch platforms when QEMU is configured (tag builds).
