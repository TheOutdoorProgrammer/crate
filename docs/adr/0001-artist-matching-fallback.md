# ADR-0001: Require Artist and Title Match in Auto-Downloads

## Status

Accepted (2026-06-08), revised (2026-06-08)

## Context

Crate's downloader searches Soulseek (via slskd) for `"Artist Name Track Title"` and then scores the returned files to pick the best candidate. The original implementation only required the **track title** to appear in the filename, with the artist name contributing a small +20 score bonus. This meant a generic title like "Dreams" would match files from any artist, and a high-quality FLAC from the wrong artist would outscore a correct-artist MP3 because quality points (up to 100) dominated the 20-point artist bonus.

The artist match was intentionally kept as a bonus rather than a hard filter to accommodate "flat libraries" — Soulseek users who organize files without artist directory structures (e.g., `Music/01 - Dreams.flac` instead of `Music/Fleetwood Mac/Rumours/01 - Dreams.flac`).

### History

- `e67b33f`: Removed artist from the filter entirely (was previously `artist OR title`), leaving title-only.
- `36e31ce`: Re-added artist as a +30 score bonus with the rationale "without excluding flat libraries."
- `eb71e12`: Reduced bonus to +20 so "quality always dominates between tiers."

## Decision

**Auto-downloads** (`pickBestFile`) require both artist name and track title in the file path. No fallback. If no files match both, the download fails and the scheduler retries on the next cycle.

**Manual search** (`PollManualSearch`) only requires the track title. Users see all title-matching results and choose for themselves. Since manual search now supports editable queries (ADR-0002), users can refine the search if they need to.

The flat-library concern that originally motivated the bonus-only approach is no longer relevant for auto-downloads — downloading the wrong song is always worse than waiting. For manual search, the user is in control and can see what they're picking.

## Consequences

- Auto-downloads will never grab wrong-artist files, even for flat libraries. If a user's only source is a flat library, the auto-download will fail and they can use manual search to find and download it.
- Manual search is lenient — shows everything matching the title, scored with artist as a +20 bonus for ranking.
- The scheduler retries failed downloads on its next cycle, so transient unavailability of artist-matching sources resolves naturally.
