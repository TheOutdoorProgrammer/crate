<p align="center">
  <img src="web/public/favicon.svg" width="96" alt="Crate logo">
</p>

<h1 align="center">Crate</h1>

<p align="center">
  <em>Dig deeper.</em>
</p>

<p align="center">
  Self-hosted music manager. Search for artists via pluggable providers (MusicBrainz, Deezer, or custom gRPC), watch their discographies, and automatically download via <a href="https://github.com/slskd/slskd">slskd</a> (Soulseek). Mobile-first UI.
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
      - ./crate-data:/app/data
      - ./slskd/downloads:/app/downloads
      - ./library:/app/library
    environment:
      - CRATE_PORT=6969
      - CRATE_SLSKD_URL=http://slskd:5030
      - CRATE_SLSKD_API_KEY=your_api_key
      - CRATE_DOWNLOADS_DIR=/app/downloads
      - CRATE_LIBRARY_PATH=/app/library
      - CRATE_SCAN_INTERVAL=24h
    depends_on:
      - slskd
    restart: unless-stopped

  slskd:
    image: slskd/slskd:latest
    ports:
      - "5030:5030"
    volumes:
      - ./slskd:/app
      - ./library:/crate
    environment:
      - TZ=ETC/UTC
      - SLSKD_REMOTE_CONFIGURATION=true
      - SLSKD_API_KEY=your_api_key
      - SLSKD_SOULSEEK_USERNAME=your_soulseek_username
      - SLSKD_SOULSEEK_PASSWORD=your_soulseek_password
    restart: unless-stopped
```

Replace `your_api_key` (must match in both services), `your_soulseek_username`, and `your_soulseek_password` with your own values.

Open `http://localhost:6969`.

## Features

- **Pluggable providers** -- search via MusicBrainz, Deezer, or custom gRPC providers. Switch providers on the fly from the search UI.
- **Search** -- find artists, browse their full discography with metadata
- **Watch** -- save artists, albums, or individual tracks to your library
- **Download** -- searches slskd (Soulseek), picks the best FLAC or 320kbps MP3, downloads automatically
- **Retry with backoff** -- failed downloads retry with backoff (5m → 15m → 30m → 1h, gives up after ~2h). Failed sources are blacklisted per-user per-file so they're never retried.
- **Manual search** -- browse all slskd results for a track, see scores/format/queue info, and pick which one to download
- **Quality upgrades** -- configure priority-ordered quality tiers (e.g. FLAC > MP3 320 > MP3 256). Scheduler scans one artist per day and re-queues tracks that can be upgraded.
- **Navidrome integration** -- optionally trigger a Navidrome library scan after each download so new files appear immediately
- **Organize** -- moves completed files to `library/{Artist}/{Album (Year)}/{nn} - {Title}.ext`
- **Metadata tagging** -- writes ID3v2 (MP3) and Vorbis comments (FLAC) with artist, album, track, year, and cover art
- **New release detection** -- opt-in per artist, auto-adds albums released after the feature is enabled
- **File integrity** -- daily check that owned tracks still exist on disk; reverts to "wanted" if missing
- **Activity log** -- tracks all download activity (search, download, complete, fail) in a separate SQLite DB with configurable retention
- **Relink** -- reassign any artist to a different provider without losing your library data
- **Scheduled scans** -- re-queues wanted tracks and checks for new releases on a configurable interval
- **Duplicate guard** -- prevents duplicate artists, albums, and download queue entries
- **Settings UI** -- configure providers, slskd connection, Navidrome, quality tiers, library path, scan interval, and more from the browser
- **Mobile-first** -- responsive layout with bottom nav on mobile, sidebar on desktop

## Roadmap

- [ ] **Import existing library** -- see [DATABASE.md](DATABASE.md) for schema docs to write custom importers

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

Additional settings (default provider, slskd connection, Navidrome, quality tiers, library path, scan interval) can be configured from the Settings page in the UI.

## Auth

Crate does not include built-in authentication. If you need to restrict access, put a reverse proxy with auth in front of it (e.g. [Cloudflare Zero Trust](https://www.cloudflare.com/zero-trust/), Authelia, or nginx basic auth).

## Stack

- **Backend**: Go + Chi + SQLite (pure Go, no CGO)
- **Frontend**: React + TypeScript + Vite + Tailwind
- **Providers**: MusicBrainz + Deezer via gRPC (extensible)
- **Tagging**: ID3v2 (MP3), Vorbis comments (FLAC)
- **Downloads**: slskd (Soulseek client)
- **Deployment**: Docker (single multi-stage container)
