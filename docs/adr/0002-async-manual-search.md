# ADR-0002: Async Manual Search with Frontend Polling

## Status

Accepted (2026-06-08)

## Context

Manual search used a blocking HTTP request: the backend started an slskd search, polled internally for up to 30 seconds, then returned whatever results had arrived. Auto-downloads, by contrast, started the search on one tick and checked for results on subsequent ticks, effectively waiting until slskd reported the search as complete (up to 5 minutes).

This caused a real bug: manual search returned poor or no results for songs where the correct source was a slow-responding peer, while auto-download found the right file because it waited longer. The 30-second internal polling loop was a hack around the fundamental issue — Soulseek is P2P, results trickle in, and you can't know when you have "enough" results until the search completes.

## Decision

Replace the blocking manual search with three endpoints:

1. **`POST /api/tracks/{id}/search`** — starts the slskd search and returns the search ID immediately.
2. **`GET /api/tracks/{id}/search/{searchId}`** — returns the current scored results plus `is_complete` and `file_count`. Called repeatedly by the frontend.
3. **`DELETE /api/tracks/{id}/search/{searchId}`** — cleans up the slskd search when the user is done.

The frontend polls every 2 seconds and renders results as they arrive. A small spinner shows next to the result count while the search is in progress. When `is_complete` is true, the spinner disappears. Cleanup runs on unmount and when the user closes the search panel or starts a download.

The scoring and candidate formatting logic was extracted into a shared `formatCandidates` function used by the poll endpoint.

## Consequences

- Manual search now waits for the full Soulseek network response, same as auto-download. No more timing-dependent result quality.
- Results appear incrementally — users see matches arriving in real time rather than staring at a spinner for 30 seconds.
- Slightly more HTTP traffic (one request every 2s during search), but searches typically complete in 10-30 seconds so this is ~5-15 extra requests per search.
- The slskd search is cleaned up explicitly via DELETE rather than inline. If the frontend crashes without cleanup, the search persists in slskd until its own TTL expires (harmless).
- Backend is simpler: no internal polling loop, no goroutine blocking, no 30-second timeout constant.
