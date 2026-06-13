# ADR-0003: Manual Search Returns Every slskd Result

## Status

Accepted (2026-06-13). Supersedes the manual-search portion of [ADR-0001](0001-artist-matching-fallback.md).

## Context

Manual search lets a user search Soulseek (via slskd) for a track and pick a file to download by hand. [ADR-0001](0001-artist-matching-fallback.md) made manual search require the **track title** to appear in each filename (artist was a +20 ranking bonus). At the time the query was always `"<artist> <title>"`, so filtering by the title was a reasonable narrowing step.

[ADR-0002](0002-async-manual-search.md) then added an **editable query**: the user can replace the search text before re-running. But `PollManualSearch` kept filtering the returned files by the *original track's title*, which the editable query never updates. The result: any custom query that diverged from the track title silently dropped **every** file.

Concretely, opening manual search on a Bryan Andrews track, changing the query to `eminem`, and re-searching returned 100+ files from slskd and then filtered all of them out (none contain the original track title), so the UI showed "No results found" every time. The same title filter also hid locked files, unsupported formats, and anything that didn't look like the exact track — none of which the user asked for in a *manual*, human-in-the-loop flow.

## Decision

**Manual search applies no content filter.** `PollManualSearch` sets `scoringConfig.includeAll`, which disables the title, artist, negative-keyword, locked, and format/extension drops inside `scoreCandidates`. Every file slskd returns is surfaced.

Results are still **scored** (quality tier, artist +20, free slot, queue depth) so the most likely match sorts to the top, and still **annotated** so the UI can communicate problems without hiding them:

- `blacklisted` — previously failed from this (user, file); shown dimmed and not downloadable.
- `locked` — slskd reports the file is in a locked share; shown dimmed and not downloadable.
- `negative_match` — matches a configured negative keyword; shown dimmed but still downloadable.

The strict title+artist filter remains on the **auto-download** path (`pickBestFile`), which is unattended and must never grab the wrong file. Only manual search — where a human reviews every row — is unfiltered.

## Consequences

- Custom queries work as the user expects: searching "eminem" shows Eminem files regardless of which track the search was launched from.
- Manual search may list files that aren't the intended track. That's acceptable: the user reads the filename and chooses, and scoring keeps good matches near the top.
- Locked / blacklisted files are visible (dimmed, disabled) instead of silently missing, so "why isn't my file showing up?" has an answer on screen.
- The manual-search behavior in ADR-0001 ("only requires title") no longer holds; ADR-0001 is updated to point here.
