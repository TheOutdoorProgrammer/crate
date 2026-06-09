# ADR-0001: Require Artist Match in Download File Selection

## Status

Accepted (2026-06-08)

## Context

Crate's downloader searches Soulseek (via slskd) for `"Artist Name Track Title"` and then scores the returned files to pick the best candidate. The original implementation only required the **track title** to appear in the filename, with the artist name contributing a small +20 score bonus. This meant a generic title like "Dreams" would match files from any artist, and a high-quality FLAC from the wrong artist would outscore a correct-artist MP3 because quality points (up to 100) dominated the 20-point artist bonus.

The artist match was intentionally kept as a bonus rather than a hard filter to accommodate "flat libraries" — Soulseek users who organize files without artist directory structures (e.g., `Music/01 - Dreams.flac` instead of `Music/Fleetwood Mac/Rumours/01 - Dreams.flac`).

### History

- `e67b33f`: Removed artist from the filter entirely (was previously `artist OR title`), leaving title-only.
- `36e31ce`: Re-added artist as a +30 score bonus with the rationale "without excluding flat libraries."
- `eb71e12`: Reduced bonus to +20 so "quality always dominates between tiers."

## Decision

Require the artist name in the file path for auto-downloads, with a conditional fallback:

1. **First pass**: `scoreCandidates` runs with `requireArtist: true` — files must contain both the artist name and track title in the path.
2. **If no candidates pass**: scan all raw results to check whether *any* file mentions the artist name anywhere in its path.
   - **Artist files exist but none qualified** (locked, blacklisted, wrong format, etc.): return no result. The correct source exists but isn't available right now — downloading from a wrong artist is worse than waiting.
   - **No files mention the artist at all** (flat library): fall back to title-only matching. This preserves the original flat-library support.

Manual search (`ManualSearch`) is unchanged — it still returns all title-matching results so users can make their own choice.

## Consequences

- Auto-downloads will no longer grab wrong-artist files for common song titles.
- Flat libraries remain supported via the fallback path, though they are only reached when no artist-matching files exist at all.
- If a correct-artist file exists but is locked/blacklisted/filtered, the download will fail rather than grab a wrong-artist file. The scheduler will retry on its next cycle when the source may be available.
- The artist bonus (+20) still exists within `scoreCandidates` for tie-breaking among title-only fallback results.
