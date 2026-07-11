<p align="center">
  <img src="web/public/cratelogo.png" width="128" alt="Crate logo">
</p>

<h1 align="center">Crate</h1>

<p align="center">
  <em>Dig deeper.</em>
</p>

<p align="center">
  Self-hosted music manager. Search for artists via pluggable providers (MusicBrainz, Deezer, or custom gRPC), watch their discographies, and automatically download via <a href="https://github.com/slskd/slskd">slskd</a> (Soulseek). Optional <a href="https://www.navidrome.org/">Navidrome</a> and <a href="https://www.music-assistant.io/">Music Assistant</a> integrations trigger a library scan/sync so new music appears immediately — and with Music Assistant you can mark a track bad right from the app. Mobile-first UI.
</p>

<p align="center">
  <img src="screenshots/library.jpeg" width="180" alt="Library">
  &nbsp;
  <img src="screenshots/artist.jpeg" width="180" alt="Artist detail">
  &nbsp;
  <img src="screenshots/album.jpeg" width="180" alt="Album detail">
  &nbsp;
  <img src="screenshots/downloads.jpeg" width="180" alt="Downloads">
</p>

## Quick Start

Create a `docker-compose.yml` and run `docker compose up -d`:

```yaml
services:
  crate:
    image: ghcr.io/theoutdoorprogrammer/crate:latest
    ports:
      - "6969:6969"
    volumes:
      - ./crate:/app/data             # SQLite databases
      - ./slskd/downloads:/app/downloads  # Must match slskd's download dir
      - ./library:/app/library       # Organized music library
    environment:
      - CRATE_SLSKD_URL=http://slskd:5030
      - CRATE_SLSKD_API_KEY=your_api_key
    depends_on:
      - slskd
    restart: unless-stopped

  slskd:
    image: slskd/slskd:latest
    ports:
      - "5030:5030"
    volumes:
      - ./slskd:/app                  # slskd data (config, downloads)
      - ./library:/music             # Share your library on Soulseek
    environment:
      - SLSKD_REMOTE_CONFIGURATION=true
      - SLSKD_API_KEY=your_api_key
      - SLSKD_SOULSEEK_USERNAME=your_soulseek_username
      - SLSKD_SOULSEEK_PASSWORD=your_soulseek_password
    restart: unless-stopped
```

Replace `your_api_key` (must match in both services), `your_soulseek_username`, and `your_soulseek_password` with your own values.

> **Important:** Crate's downloads volume (`/app/downloads`) must point to the same host directory that slskd writes completed downloads to. With the config above, slskd stores downloads under `./slskd/downloads/` and crate reads from the same path. If these don't match, crate won't find downloaded files.

Open `http://localhost:6969`.

## Features

- **Pluggable providers** -- search via MusicBrainz, Deezer, or custom gRPC providers. Switch providers on the fly from the search UI.
- **Search** -- find artists, browse their full discography with metadata
- **Watch** -- save artists, albums, or individual tracks to your library
- **Download** -- searches slskd (Soulseek), scores all results using a unified scoring system, and downloads the best match automatically
- **Smart scoring** -- tier-based quality scoring from your configured priority list, artist name matching bonus, free upload slot bonus, and queue-length-aware scoring using inverse decay. Quality always dominates; availability tips close calls.
- **Retry with backoff** -- failed downloads retry with backoff (5m → 15m → 30m → 1h, gives up after ~2h). Failed sources are blacklisted per-user per-file so they're never retried.
- **Shadow banning** -- users who go offline mid-transfer or whose queued downloads stall are temporarily blocked (configurable duration, default 60min). Different from permanent file blacklists -- shadow bans expire automatically.
- **State-aware stale detection** -- detects stalled downloads with timeouts tuned to the transfer state: actively transferring (5min), queued/waiting for a slot (30min), or requested (10min). Queued stalls trigger shadow bans; active transfer stalls blacklist the specific file.
- **Blocked sources management** -- view and remove blacklisted files and shadow-banned users from the Settings UI
- **Manual search** -- browse every slskd result for a track, see scores/format/queue info (blacklisted and locked sources shown but dimmed), and pick which one to download
- **Quality tiers** -- configure priority-ordered quality tiers (e.g. FLAC > MP3 320 > MP3 256) with an optional fallback toggle to reject files outside your configured tiers. Scheduler scans one artist per day and re-queues tracks that can be upgraded.
- **Negative keywords** -- skip files matching configurable keywords (e.g. acapella, instrumental) during auto-download. Manual search still shows them so you can override when needed.
- **Import existing library** -- tag-based scan of your on-disk collection with dry-run preview; MusicBrainz-tagged files (Picard/beets) link to their real provider IDs automatically
- **Navidrome integration** -- optionally trigger a Navidrome library scan after each download so new files appear immediately
- **Music Assistant integration** -- optionally sync your [Music Assistant](https://www.music-assistant.io/) library after each download, and **mark a track bad right from the MA app** by dropping it into a reject playlist: Crate deletes the bad copy, blacklists the source, and re-downloads a better one
- **Organize** -- moves completed files into the library using a configurable naming template (default `{artist}/{album} ({year})/{track:2} - {title}`), so Crate can match an existing library convention
- **Metadata tagging** -- writes ID3v2 (MP3), Vorbis comments (FLAC), and RIFF INFO (WAV) with artist, album, track, year, and cover art (MP3/FLAC). Non-destructive: only Crate's own fields are touched, so tags written by other tools (ReplayGain, MusicBrainz/AcoustID IDs) are preserved
- **New release detection** -- opt-in per artist, auto-adds albums released after the feature is enabled
- **File integrity** -- daily check that owned tracks still exist on disk; reverts to "wanted" if missing
- **Activity log** -- tracks all download activity (search, download, complete, fail) in a separate SQLite DB with configurable retention
- **Relink** -- reassign any artist to a different provider without losing your library data
- **Scheduled scans** -- re-queues wanted tracks and checks for new releases on a configurable interval
- **Duplicate guard** -- prevents duplicate artists, albums, and download queue entries
- **Settings UI** -- configure providers, slskd connection, Navidrome, Music Assistant, quality tiers, quality fallback, shadow ban duration, naming template (with live preview), library path, scan interval, and more from the browser
- **Mobile-first** -- responsive layout with bottom nav on mobile, sidebar on desktop

## Lidarr API Compatibility

Crate ships a Lidarr v1 API shim at `/api/v1/` so iOS apps like [Helmarr](https://apps.apple.com/us/app/helmarr/id1638624921) can manage your library as if Crate were Lidarr. No extra configuration needed -- point the app at your Crate URL with any API key and it works.

### Concept mapping

Lidarr and Crate model things differently. The shim translates between them:

| Lidarr concept | Crate equivalent | Notes |
|---|---|---|
| Monitor "all" | Artist status `watched` | Full discography tracked |
| Monitor "latest" | Watch newest album + enable new releases | Most recent album by year, future albums auto-added |
| Quality profile | "Crate Quality" | Crate uses priority-ordered quality tiers instead of Lidarr-style profiles |
| Metadata profile | Active provider name | Shows whichever provider is set as primary (MusicBrainz, Deezer, etc.) |
| Root folder | `CRATE_LIBRARY_PATH` | Library directory with real disk stats |
| Monitored album | Album status `watched` | Tracks set to `wanted` |
| Unmonitored album | Album status `ignored` | Tracks cascade to `ignored` (preserves `owned`/`downloading`) |
| ArtistSearch command | Queue all wanted tracks | Same as "Search wanted tracks" in the UI |
| AlbumSearch command | Queue album's wanted tracks | Same as "Search wanted tracks" on the album page |
| "Search for missing albums" on add | Auto-queue after watch | Queues all wanted tracks immediately |

### Supported endpoints

System status, health, disk space, quality/metadata profiles, root folders, custom filters, calendar, history, wanted/missing, queue, search, artist CRUD, album CRUD (including monitor toggle), track listing, and commands (ArtistSearch, AlbumSearch). Interactive search is not supported -- use the Crate UI for manual file selection.

### Auth

The shim accepts any API key in the `X-Api-Key` header or `apikey` query parameter. Crate has no built-in auth -- use a reverse proxy if you need access control.

### Tested with

- [Helmarr](https://apps.apple.com/us/app/helmarr/id1638624921) (iOS) -- full artist/album management, search, monitoring

Contributions to expand Lidarr API coverage are welcome.

## Importing an Existing Library

Crate can adopt a library it didn't download. **Settings → Library → Import Existing Library** scans your library folder, reads embedded tags (MP3 and FLAC), and records everything as `owned` — run the dry-run scan first to preview the result.

How it works:

- **Tag-based, not path-based.** Folder structure is ignored entirely; only embedded metadata matters. If your library works in Navidrome, your tags are good enough.
- **Non-destructive.** Files are never moved, renamed, or modified. Changing the naming template later doesn't touch imported files either.
- **MusicBrainz-tagged libraries link automatically.** Files tagged by Picard or beets carry MusicBrainz IDs; those import under the `musicbrainz` provider with their real IDs (artist MBID, release-group ID, release-track ID) and behave exactly like browsed entities. Everything else imports under the reserved `local` provider with stable tag-derived IDs — relink an artist to a real provider whenever you want search, cover art, or new-release watching for it.
- **Wanted tracks get claimed.** If you already watch an album and import files matching its tracks (by title), those tracks flip to `owned` instead of being re-downloaded.
- **Idempotent.** Re-running an import skips everything it already knows.
- **Quality upgrades apply.** Imported tracks record their real format and bitrate (parsed from the files), so the upgrade scanner treats them like any other owned track — import MP3s with a FLAC-first tier list and Crate will gradually upgrade them. Files outside the library folder are never deleted, even when replaced by an upgrade.
- Files with missing artist/album/title tags are skipped and reported, with reasons.

Prefer a custom importer? The schema is documented in [DATABASE.md](DATABASE.md).

## Configuration

| Env var | Default | Description |
|---|---|---|
| `CRATE_PORT` | `6969` | HTTP port |
| `CRATE_DB_PATH` | `./crate.db` | SQLite database path |
| `CRATE_CACHE_PATH` | `./cache.db` | Provider cache database path |
| `CRATE_ACTIVITY_PATH` | `./activity.db` | Activity log database path |
| `CRATE_SLSKD_URL` | `http://localhost:5030` | slskd API base URL |
| `CRATE_SLSKD_API_KEY` | -- | slskd API key |
| `CRATE_DOWNLOADS_DIR` | `./downloads` | Where slskd puts completed files |
| `CRATE_LIBRARY_PATH` | `./library` | Where organized files are moved |
| `CRATE_SCAN_INTERVAL` | `6h` | How often to auto-queue wanted tracks and check for new releases |
| `CRATE_PROVIDERS` | `musicbrainz:./provider-musicbrainz:50051,deezer:./provider-deezer:50052` | Provider configuration |

Additional settings (default provider, slskd connection, Navidrome, Music Assistant, quality tiers, naming template, library path, scan interval) can be configured from the Settings page in the UI.

### Library naming template

The folder/file layout for downloads is a template you can change from **Settings → Library** (with a live preview). The default matches Crate's original layout:

```
{artist}/{album} ({year})/{track:2} - {title}
```

| Token | Meaning |
|---|---|
| `{artist}` / `{albumartist}` | Artist name (Crate tracks album artists, so these are identical) |
| `{album}` | Album title |
| `{year}` | Album release year (renders empty when unknown) |
| `{track}` | Track number; zero-pad with a width, e.g. `{track:2}` → `06` |
| `{disc}` | Disc number; supports padding; renders empty when unknown |
| `{title}` | Track title |

Notes:

- The file extension is appended automatically -- don't put it in the template.
- When an empty token (like `{year}`) leaves dangling decoration behind, Crate cleans it up: `{album} ({year})` renders as `Album Title` when the year is unknown, not `Album Title ()`.
- The template applies to **new downloads only**. Existing files are never renamed when you change it. Quality upgrades use the current template and remove the file they replace, even if it was organized under an older template.
- Filesystem-unsafe characters (`<>:"/\|?*`) in metadata are replaced with `_`.

## Auth

Crate does not include built-in authentication. If you need to restrict access, put a reverse proxy with auth in front of it (e.g. [Cloudflare Zero Trust](https://www.cloudflare.com/zero-trust/), Authelia, or nginx basic auth).

## Stack

- **Backend**: Go + Chi + SQLite (pure Go, no CGO)
- **Frontend**: React + TypeScript + Vite + Tailwind
- **Providers**: MusicBrainz + Deezer via gRPC (extensible)
- **Tagging**: ID3v2 (MP3), Vorbis comments (FLAC), RIFF INFO (WAV)
- **Downloads**: slskd (Soulseek client)
- **Deployment**: Docker (single multi-stage container)

## Supported Formats

| Format | Download | Metadata tagging | Cover art |
|--------|----------|-----------------|-----------|
| FLAC   | Yes      | Yes (Vorbis comments) | Yes |
| MP3    | Yes      | Yes (ID3v2) | Yes |
| WAV    | Yes      | Yes (RIFF INFO) | No |
| OGG    | Yes      | No | No |
| Opus   | Yes      | No | No |
| AAC    | Yes      | No | No |
| M4A    | Yes      | No | No |

All formats are downloaded, organized into the library folder, and tracked in the database. Formats without tagging support are still fully functional -- they just won't have embedded metadata written by Crate.

## Architecture Decision Records

Design decisions with meaningful trade-offs are documented as ADRs in [`docs/adr/`](docs/adr/).

| ADR | Decision |
|-----|----------|
| [0001](docs/adr/0001-artist-matching-fallback.md) | Auto-downloads require artist+title match (manual-search filtering since removed — see 0003) |
| [0002](docs/adr/0002-async-manual-search.md) | Async manual search with frontend polling instead of blocking 30s request |
| [0003](docs/adr/0003-manual-search-no-filter.md) | Manual search returns every slskd result (scored + annotated, never filtered) |
| [0004](docs/adr/0004-non-destructive-tagging.md) | Tagger preserves foreign tags; the `crate:` comment tag was dropped |
| [0005](docs/adr/0005-recording-id-signal.md) | MusicBrainz recording id stored as a separate signal, not resolved to release-track |
| [0006](docs/adr/0006-music-assistant-integration.md) | Music Assistant added alongside Navidrome; mark-bad-from-app via an event-driven reject playlist |
