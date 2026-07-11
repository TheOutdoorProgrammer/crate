# ADR-0004: Non-Destructive Tagging

## Status

Accepted (2026-07-10).

## Context

Crate's tagger writes title/artist/album/track/disc/year and cover art into each downloaded file. The original implementation rebuilt the tag block from scratch: for FLAC it created a fresh Vorbis comment block containing only Crate's fields and swapped out the existing one; for MP3 it opened the file with `id3v2` `Parse:false` and saved, which drops any frame not re-written. That silently **destroyed every tag Crate didn't write**.

This was harmless while Crate was the only thing tagging the files. It stopped being harmless with the Music Assistant integration: MA's analysis providers write back into the same files — `loudness_analysis` writes `REPLAYGAIN_TRACK_GAIN`, and `acoustid_lookup` writes a MusicBrainz recording id (`MUSICBRAINZ_TRACKID` / `UFID`), an AcoustID id, and ISRC. Any Crate re-tag (a quality upgrade, or the recording-id capture flow) would wipe them.

Separately, the tagger wrote a `crate:{track_id}` comment tag so the Haystack iOS app could map a file back to its Crate track by id. Haystack was retired, leaving the comment tag with no consumer.

## Decision

**The tagger preserves foreign tags and overwrites only Crate's own fields.**

- **FLAC**: read the existing Vorbis comment block, drop only Crate-owned keys (`TITLE`, `ARTIST`, `ALBUM`, `TRACKNUMBER`, `DISCNUMBER`, `DATE`), then re-add Crate's values. Everything else — ReplayGain, MusicBrainz/AcoustID/ISRC, etc. — is carried over verbatim.
- **MP3**: open with `Parse:true` so all frames are loaded. `SetTitle`/etc. replace single-value frames; for `TRCK`/`TPOS`/`APIC` we `DeleteFrames` then re-add, so pre-tagged Soulseek files don't accumulate duplicates. Unknown frames (`UFID`, `TXXX`, `TSRC`, …) are retained by the library and rewritten on save.
- **The `crate:{track_id}` comment tag is dropped** — it had no reader after Haystack, and the Music Assistant reject watcher maps files to tracks by path instead.

Regression tests (`internal/services/tagger/service_test.go`) seed a file with `REPLAYGAIN_TRACK_GAIN` + a MusicBrainz recording id (FLAC) and ISRC/AcoustID frames (MP3), run the tagger, and assert the foreign tags survive while Crate's fields are set and no `crate:` comment is written.

## Consequences

- Music Assistant's ReplayGain and fingerprint tags survive Crate re-tags; the two tools coexist on the same files.
- Crate is a well-behaved tagger generally — it won't clobber tags from Picard, beets, or any other tool.
- The `crate:` comment tag is gone. Anything that depended on it (only Haystack, retired) would break; nothing in the current system does.

## Alternatives considered

- **Keep rebuilding the block, but only when Crate is the first tagger.** Fragile — depends on ordering, and the recording-id capture flow re-tags after MA has written.
- **Keep the `crate:` comment tag as the reject-watcher mapping key.** Rejected: mapping by file path (which MA already exposes on filesystem tracks) is more robust and needs no bookkeeping.
