# ADR-0005: MusicBrainz Recording ID as a Separate Signal

## Status

Accepted (2026-07-10).

## Context

Music Assistant's `acoustid_lookup` provider fingerprints audio (Chromaprint → AcoustID) and writes the resulting **MusicBrainz recording id** into the file (`MUSICBRAINZ_TRACKID` in Vorbis, `UFID` owner `http://musicbrainz.org` in ID3). This is a strong, fingerprint-verified identity — far better than fuzzy artist+title matching. Crate wanted to trust it.

But there's a namespace mismatch:

- AcoustID resolves to a MusicBrainz **recording** — "the song", release-independent.
- Crate's `musicbrainz` provider models tracks as **release-tracks** — "the song as it appears on a specific release". The importer already reads `MUSICBRAINZ_RELEASETRACKID` into `provider_id` under the `musicbrainz` provider.

So the id MA writes is a different tag key *and* a different entity type than the one Crate keys tracks on. A recording id can't be used as a release-track `provider_id`, and a fingerprint can't know which release you're holding.

## Decision

**Store the recording id as a separate signal; do not resolve it to a release-track.**

- New nullable column `tracks.mb_recording_id` (migration 010).
- The importer reads `MUSICBRAINZ_TRACKID` (FLAC) / `UFID` (MP3) into it, persisted on create and claim.
- Matching uses it **album-scoped and highest-priority**: `FindTrackByAlbumRecordingID(albumID, id)` is tried before release-track and title-fold matching. Album-scoping is deliberate — a recording shared across releases must not let a file claim a track in the wrong album.
- It never overrides album/release structure. It is an identity/matching signal, not a replacement for the release-track namespace.

## Consequences

- Fingerprint-verified matching is available where both a file and an existing track carry the recording id (re-imports, and future MA-driven flows).
- Crate stays release-track-centric; nothing about the album model changes.
- The id lands in files only when MA's `acoustid_lookup` has `write_tags_back` enabled — documented as a prerequisite.

## Alternatives considered

- **Resolve recording → release-track via MusicBrainz.** A recording maps to many release-tracks; picking the right one is ambiguous and needs online lookups. Not worth it.
- **Global (non-album-scoped) recording matching in the importer.** Rejected — a shared recording could claim a file into the wrong album.
- **Read the id from MA's database via its API instead of file tags.** Rejected — keeps Crate tag-based and self-reliant (works for any Picard/beets library, not just MA-managed ones).
