# Crate Database Schema

Crate uses SQLite with WAL mode. The main database (`crate.db`) has five tables. This doc is for anyone writing a custom importer or migration script.

## Tables

### artists

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | Auto-increment |
| `name` | TEXT NOT NULL | Artist name |
| `provider` | TEXT NOT NULL | Provider name (e.g. `musicbrainz`, `deezer`) |
| `provider_id` | TEXT NOT NULL | Provider-specific ID (MBID, Deezer int as string, etc.) |
| `image_url` | TEXT | Artist image URL (nullable) |
| `status` | TEXT | `watched` (full discography), `partial` (some albums), `owned` |
| `watch_new_releases` | INTEGER | 0 or 1 |
| `watch_new_releases_since` | TEXT | RFC3339 timestamp, only tracks releases after this date |
| `created_at` | TEXT | RFC3339 |
| `updated_at` | TEXT | RFC3339 |

### albums

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | Auto-increment |
| `artist_id` | INTEGER FK | References `artists(id)` ON DELETE CASCADE |
| `title` | TEXT NOT NULL | Album title |
| `year` | INTEGER | Release year (nullable) |
| `provider` | TEXT NOT NULL | Same as artist's provider |
| `provider_id` | TEXT NOT NULL | Provider-specific album ID |
| `cover_url` | TEXT | Album cover URL (nullable) |
| `record_type` | TEXT | `album`, `single`, `ep`, `compilation` |
| `status` | TEXT | `watched`, `owned`, `ignored` |
| `created_at` | TEXT | RFC3339 |
| `updated_at` | TEXT | RFC3339 |

### tracks

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | Auto-increment |
| `album_id` | INTEGER FK | References `albums(id)` ON DELETE CASCADE |
| `title` | TEXT NOT NULL | Track title |
| `track_number` | INTEGER | Track number on disc |
| `disc_number` | INTEGER | Disc number |
| `duration_ms` | INTEGER | Duration in milliseconds |
| `provider` | TEXT NOT NULL | Same as album's provider |
| `provider_id` | TEXT NOT NULL | Provider-specific track ID |
| `status` | TEXT | `wanted`, `downloading`, `owned`, `ignored` |
| `file_path` | TEXT | Path to file on disk, set when owned. Crate stores paths relative to the library dir; absolute paths (e.g. from importers) also work. Crate never deletes files outside the library dir. |
| `downloaded_from` | TEXT | slskd username the file was downloaded from |
| `download_format` | TEXT | File format: `flac`, `mp3`, etc. (set at search time) |
| `download_bitrate` | INTEGER | Bitrate in kbps (set at search time, 0 for lossless) |
| `created_at` | TEXT | RFC3339 |
| `updated_at` | TEXT | RFC3339 |

### download_queue

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | Auto-increment |
| `track_id` | INTEGER FK | References `tracks(id)` ON DELETE CASCADE |
| `slskd_search_id` | TEXT | slskd search ID (or `username|transferID` during download) |
| `status` | TEXT | `pending`, `searching`, `downloading`, `organizing`, `complete`, `failed` |
| `attempts` | INTEGER | Number of attempts |
| `last_attempt` | TEXT | RFC3339 timestamp of last attempt |
| `error` | TEXT | Error message (nullable) |
| `next_retry_at` | TEXT | RFC3339 timestamp for automatic retry (nullable) |
| `created_at` | TEXT | RFC3339 |

### settings

| Column | Type | Notes |
|---|---|---|
| `key` | TEXT PK | Setting key |
| `value` | TEXT NOT NULL | Setting value |

Known keys: `provider_primary`, `slskd_url`, `slskd_api_key`, `library_path`, `download_format_preference`, `min_bitrate`, `scan_interval`, `cache_ttl_hours`, `quality_tiers` (JSON array), `upgrade_last_artist_id`, `navidrome_url`, `navidrome_user`, `navidrome_password`, `activity_retention_days`, `naming_template` (library folder/file layout, empty = default `{artist}/{album} ({year})/{track:2} - {title}`).

### slskd_blacklist

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | Auto-increment |
| `username` | TEXT NOT NULL | slskd username |
| `filename` | TEXT NOT NULL | Full file path on the user's machine |
| `reason` | TEXT | Why it was blacklisted (e.g. "transfer Errored") |
| `created_at` | TEXT | RFC3339 |

Unique index on `(username, filename)`. Blacklisted entries are skipped by `pickBestFile` during auto and manual searches.

## Indexes

- `idx_albums_artist_id` on `albums(artist_id)`
- `idx_tracks_album_id` on `tracks(album_id)`
- `idx_tracks_status` on `tracks(status)`
- `idx_download_queue_status` on `download_queue(status)`
- `idx_download_queue_track_id` on `download_queue(track_id)`
- `idx_download_queue_track_active` unique partial index on `download_queue(track_id)` WHERE status IN ('pending', 'searching', 'downloading', 'organizing') — prevents duplicate active downloads
- `idx_artists_provider` unique on `artists(provider, provider_id)`
- `idx_albums_provider` unique on `albums(provider, provider_id)`
- `idx_tracks_provider` unique on `tracks(provider, provider_id)`

## Importing existing music

**Crate has a built-in importer** — Settings → Library → Import Existing Library (or `POST /api/library/import`). It reads embedded tags from MP3/FLAC files, never touches the files themselves, and supports a dry run. Entities it creates use the reserved provider name `local` with stable tag-derived IDs (`loc-…`), unless the files carry consistent MusicBrainz tags, in which case they import under `musicbrainz` with their real IDs. `local` entities can be relinked to a real provider at any time.

If the built-in importer doesn't fit (unsupported formats, exotic layouts), write directly to the database: insert rows into `artists`, `albums`, and `tracks`. Set `status = 'owned'` on tracks and provide the `file_path`. Example:

```sql
-- Insert an artist
INSERT INTO artists (name, provider, provider_id, status, created_at, updated_at)
VALUES ('Radiohead', 'musicbrainz', 'a74b1b7f-71a5-4011-9441-d0b5e4122711', 'partial',
        strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), strftime('%Y-%m-%dT%H:%M:%SZ', 'now'));

-- Insert an album (use last_insert_rowid() for artist_id)
INSERT INTO albums (artist_id, title, year, provider, provider_id, record_type, status, created_at, updated_at)
VALUES (last_insert_rowid(), 'OK Computer', 1997, 'musicbrainz', 'b1392450-e666-3926-a536-22c65f834433',
        'album', 'owned',
        strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), strftime('%Y-%m-%dT%H:%M:%SZ', 'now'));

-- Insert owned tracks
INSERT INTO tracks (album_id, title, track_number, disc_number, duration_ms, provider, provider_id,
                    status, file_path, created_at, updated_at)
VALUES (last_insert_rowid(), 'Airbag', 1, 1, 282000, 'musicbrainz', 'some-track-mbid',
        'owned', '/music/Radiohead/OK Computer (1997)/01 - Airbag.flac',
        strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), strftime('%Y-%m-%dT%H:%M:%SZ', 'now'));
```

Tips:
- If you don't have provider IDs, use the provider name `local` with any unique string as the ID (e.g. a hash of artist+album+track). `local` is reserved for exactly this: it's always treated as healthy (no orphan badge) and its entities can be relinked to a real provider later. IDs are only used for deduplication and live provider lookups.
- Set `status = 'wanted'` on tracks you want Crate to download for you.
- The `download_queue` table is managed by the downloader — don't insert into it directly.
- Foreign keys cascade on delete: deleting an artist removes all their albums and tracks.

## Activity log

The activity log lives in a separate SQLite file (`activity.db` by default, configurable via `CRATE_ACTIVITY_PATH`). It has a single `activity_log` table with columns: `id`, `action`, `entity_type`, `entity_id`, `details`, `created_at`. This DB can be deleted or truncated at any time without affecting the main database.
