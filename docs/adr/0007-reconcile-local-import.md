# ADR-0007: Reconcile an Imported Local Library Against a Provider

## Status

Accepted (2026-07-23) — implemented: reconcile-on-link in `internal/api/reconcile.go`, manual merge/claim in `internal/api/manual_link.go`, UI in `web/src/pages/{ArtistDetail,AlbumDetail}.tsx`.

## Context

Library import (`internal/services/importer`) records only the files on disk, all as `status = owned`. Files carrying consistent MusicBrainz tags import under the `musicbrainz` provider with real IDs; everything else gets the reserved `local` provider (`provider.LocalProvider`) with stable tag-derived hash IDs. Import never fetches a *reference* discography, so on its own it produces no gap view — there's nothing to diff "what you have" against.

"What you're missing" is modeled as the `wanted` track status, and `wanted` rows are created **only by watching an artist** — that's the step that pulls the full discography from a provider (`handleWatchArtist` → `saveAlbumsFromProvider` / `syncAlbumTracks`). Two outcomes today:

- **MB-tagged imports watch cleanly.** The imported artist/album/track already live under `musicbrainz` with the same IDs the provider browses (release-group for albums, release-track for tracks), so watch matches by `(provider, provider_id)`, skips what you own, and marks only the true gaps `wanted`.
- **Untagged (`local`) imports duplicate.** Watch matches only by `(provider, provider_id)` and has no name fallback (unlike the importer, which folds on name/title). It misses every `local` row and creates a **parallel watched artist** with the entire discography `wanted` — including tracks you already own on disk. The two artist rows never reconcile.

`Relink*` today is a dumb single-row re-stamp (`UPDATE … SET provider=?, provider_id=?`) — it doesn't fetch a discography, create `wanted` rows, or cascade to children. So it can re-point a `local` artist's identity but can't fill the gap or absorb the import.

The failure risk in *any* reconcile is asymmetric across the three joins:

- **Artist join is dangerous.** Same-name collisions are common in music (two "Nirvana", the film-composer vs classical-guitarist "John Williams"). Local tags carry no disambiguation, so an automated name match can bind a whole wrong discography — large blast radius.
- **Album join is safe-ish.** Within one artist's discography the namespace is tiny, and exact title+year folding errs toward *under*-merge (leftover to prune) rather than false-merge.
- **Track join / "song on the wrong album" is structurally low-risk.** All track matching is album-scoped, so a track can never move between albums — wrong placement can only come from pre-existing bad source tags, which is true with or without this feature.

## Decision

**Promote a `local` import to a real provider through a user-anchored artist link that cascades a conservative, local-scoped fuzzy reconcile to its albums and tracks.** The one dangerous join (artist) is resolved by the human; the safe joins (album/track) are automated and always album-scoped.

1. **Artist identity is chosen by the user, never fuzzed.** They search the provider and pick the exact artist via the relink panel already on `ArtistDetail` (always visible, not gated on `orphaned`). This deletes the same-name failure class outright — no name-fold matching, no corroboration heuristic needed.

2. **Linking a `local` artist to a real provider runs a reconcile cascade, not a bare re-stamp:**
   - Fetch the linked provider artist's discography.
   - Match each local album to a provider release-group by **case-folded title + year**. Relink matches in place; leave unmatched local albums as owned + `local`; create provider albums with no local match as new `wanted` albums (genuine gaps).
   - Within each matched album, match local tracks to provider release-tracks with the importer's exact ladder — **recording id → release-track → title-fold, album-scoped** (`FindTrackByAlbumRecordingID` → title). Relink matches; leave unmatched local tracks as owned + `local`; create provider tracks with no local match as `wanted`.
   - Set the artist to `watched` (with an opt-out) so the scheduler actually fills the new `wanted` rows.

3. **The reconcile trigger is "local children remain," not "old provider == `local`."** At the album level, run the track reconcile whenever the album still contains any `local`-provider track. This single predicate covers **promotion** (all children local), **correction** of a wrong fuzzy album match (the mismatched tracks are still local), and naturally no-ops once everything is matched. Gating on the album's *former* provider would leave a wrongly-matched album (now `musicbrainz`) impossible to re-reconcile.

4. **Manual escape hatch = merge, not re-stamp.** When fuzzy can't match (title drift, deluxe editions, tagging quirks), the user links the leftover by hand — but implementation surfaced a wrinkle the first draft missed. After reconcile the provider's whole discography is already present, so the "correct release-group" is usually already a **sibling album**; re-stamping the local album's `provider_id` onto it would collide on the unique `(provider, provider_id)` index. So linking **merges** instead of re-stamping:
   - Album (`POST /albums/{id}/link` with a `target_album_id`): each owned local file claims the matching wanted track in the target; a track the provider doesn't list moves over as an owned + `local` extra; the emptied local shell is deleted.
   - Track (`POST /tracks/{id}/link` with a `target_track_id`): the local file claims the chosen wanted track; the local duplicate is deleted.

   Pickers are scoped to what already exists — the artist's sibling releases, the album's wanted tracks — so no provider round-trip is needed. (The `POST /relink/album/{id}` re-stamp + reconcile path from rule 3 stays for the rarer case of a release-group the discography didn't include.)

5. **Surface the leftovers from state, not a report.** A `local`-provider entity *is* the leftover signal — no separate report needed. A `local` artist is flagged loudly: a "not linked" badge in the Library list and a "Not linked to a provider" banner (with the link action) on its page, so an imported artist obviously needs linking. Within a linked artist, a still-`local` album gets an "unmatched" badge and its page shows the same banner + merge picker; a still-`local` track carries a `local` badge + link action. Without this the escape hatch is invisible and nobody uses it.

6. **Invariants (non-negotiable):**
   - Only entities under the reserved `local` provider are ever fuzzed. Provider-anchored data is never touched.
   - Track matching is always album-scoped — a track never crosses an album boundary.
   - Never destructive on a link: unmatched local rows stay owned + `local`; nothing is deleted.
   - Reconcile is idempotent and safe to re-run.

## Consequences

- Untagged imports get a real gap view (progress bars, `wanted` rows, one-tap search) **without** duplicating the artist.
- The only automated fuzzy matching runs *inside a human-confirmed artist* and is album-scoped, so the worst realistic failure is benign — a leftover `local` row to link manually or a re-download of something you own — never a wrong-artist discography or a song on the wrong album.
- **Mixed-provider artists are expected and correct.** A bootleg or release the provider doesn't know about stays owned + `local` under an otherwise-`musicbrainz` artist. Gap math counts the reconciled entities; the local leftovers sit as owned.
- Ordering constraints follow from provider context: album manual-link requires the artist already linked; track manual-link requires the album already linked. The affordances are gated accordingly.
- `Relink` gains a second meaning. The "local children remain" trigger keeps **promote/correct** (has local children → reconcile) cleanly separate from a **plain provider swap** on an already-tracked artist (real → real, no local children → cheap re-stamp, as today).

## Alternatives considered

- **Auto-match artists by name-fold on watch (the original sketch).** Rejected — same-name collisions can bind a whole wrong discography; the blast radius is large and a corroboration heuristic is fragile. Anchoring the artist to a human choice removes the risk instead of mitigating it.
- **Gate the cascade solely on `old provider == local`.** Rejected as the *only* trigger — it can't re-reconcile after a wrong fuzzy album match, since that album is already non-`local`. "Local children remain" is the robust predicate.
- **Make plain `Relink` cascade for every provider.** Rejected — a real → real swap (e.g. an orphaned provider) should stay a cheap re-stamp, not refetch and re-fuzz a discography.
- **Fuzzier album matching (normalized-title clustering, fingerprint album ID).** Deferred — exact title + year folding is conservative and errs toward under-merge; the manual link covers the residue. Revisit if unmatched albums prove common in practice.
- **Auto-delete duplicate `local` rows after matching.** Rejected — a metadata link is never destructive, and unmatched `local` rows are legitimate content, not garbage.
