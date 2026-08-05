# v2 Plan Audit — Findings

Twelve independent reviewers, one per section of `V2-Plan.md`, each reading the actual source and
citing `file:line`. Several ran code to confirm. This is the output.

**Read this before `V2-Plan.md`.** The audit found bugs *in the plan* and bugs *in shipped v1 code*, and the second category is worth acting on regardless of whether v2 ever happens.

A thirteenth reviewer then read both documents and the source, and spot-checked ~25 of these against the code.
Most held.
The ones that didn't are marked ⚠️ inline and corrected in place — B12, B17, B18, B26, X4 — plus one bug the twelve missed entirely (B11b).
Where a claim here and a claim in `V2-Plan.md` disagree, this file is the corrected one.

**Line citations are against revision 6** — the version that was audited.
The plan has been revised twice since, so `V2-Plan.md:340` will not point at the text being quoted.
Use the surrounding description to find it; the prose identifies each claim well enough to locate it in any revision.

Sections:

- [Ship now — v1 bugs](#ship-now--v1-bugs) — real defects in shipped code, independent of v2
- [Corrections to the plan](#corrections-to-the-plan) — where V2-Plan.md is wrong
- [Contract gaps](#contract-gaps) — what the proto needs that nobody specced
- [Cross-section conflicts](#cross-section-conflicts) — where reviewers disagreed
- [Decisions needed](#decisions-needed) — Joey's calls
- [Convergent findings](#convergent-findings) — hit independently by multiple reviewers

---

## Ship now — v1 bugs

None of these need v2. Several are cheaper to fix *before* the refactor, because the code moves.

### Data loss / correctness

| # | Bug | Where |
| --- | --- | --- |
| B1 | **`copyFile` leaves a truncated destination on failure** — no close, no remove on write error; also leaks the fd. This is the **cross-device** path, i.e. the NAS path, i.e. the common one. Disk fills → a partial audio file sits at the library path → a later scan adopts it as real. | `organizer/service.go:185-216` |
| B2 | **`tagWAV` truncates the file in place** — `os.ReadFile` the whole file (a 700MB WAV is 1.4GB RSS), then `os.WriteFile` to the same path. Crash mid-write destroys the audio. | `tagger/service.go:163-200` |
| B3 | **`Place`/organize silently clobbers unknown files** — `os.Rename(src, dest)` overwrites whatever is at the destination. `removeReplacedFile` only handles the DB-known case. An imported library whose layout already matches the naming template loses files. | `organizer/service.go:89` |
| B4 | **`mergeAlbum` destroys `mb_recording_id`** — sources load via `ListTracksByAlbum`, whose SELECT omits the column, so `t.MBRecordingID` is always nil and `COALESCE` preserves the target's NULL. Merging an album loses the fingerprint-verified identity ADR-0005 exists to capture. | `manual_link.go:71,85` + `queries.go:386-388` |
| B5 | **No transactions anywhere.** `EnqueueDownloadBatch` is the *only* transactional operation in 1124 lines of `queries.go`; there is no `WithTx` helper. `mergeAlbum` crashing mid-loop leaves **two rows owning the same `file_path`**. Today that's a display oddity — under v2 it becomes a deleted file. | `queries.go:800-832`, `manual_link.go:80-98` |
| B6 | **Reject marks a track `wanted` even when the delete was refused.** `library.DeleteFile` returns nothing; for a path outside the library it logs "refusing to delete" and returns. `RejectTrack` proceeds anyway. The original file stays **and** crate downloads a replacement. | `reject.go:60-71`, `library/paths.go:42-45` |
| B7 | **The "Delete track" button never deletes the file.** `handleUnwatchTrack` only deletes when `?delete=true`; the frontend sends a bare DELETE. Worse: `handleUnwatchArtist`/`handleUnwatchAlbum` cascade-delete with **no file handling at all**. | `handlers.go:846-869` vs `client.ts:78-79` |

### Stuck / runaway states

| # | Bug | Where |
| --- | --- | --- |
| B8 | **`retryOrganize` has never worked, in either branch.** On a first download `file_path` is NULL → returns nil, row stuck in `organizing` **forever**, reprocessed every 10s, no attempt counter, no failure transition. On an upgrade it holds the *library* path, whose basename `findFile` searches for in the *downloads* dir → never found → same loop. Only escape is `DELETE /api/downloads/{id}`. | `downloader/service.go:441-467` |
| B9 | **`ManualDownload` can corrupt an unrelated queue row.** `EnqueueDownloadReturningID` is `INSERT … ON CONFLICT DO NOTHING` followed by `LastInsertId()` — on conflict nothing is inserted and it returns the connection's *previous* rowid. `UpdateDownloadStatus` then rewrites an arbitrary other row. Reachable any time a user manual-downloads a track with an active queue row. | `downloader/service.go:614-646`, `queries.go:723-735` |
| B10 | **Duplicate-file re-download loop.** "First file wins, never reassign" for duplicate `(album, title)`. Delete the winner, keep the other → integrity check flips the track to `wanted` and re-downloads a song sitting on disk. Permanently, because every scan re-reports the survivor as a duplicate. | `importer/service.go:355-363` |
| B11 | **`handleQueueTrack` has no status guard.** Any track id can be enqueued regardless of status. Neither does `downloader.process`. | `handlers.go:717-728` |
| B11b | **The upgrade window lies about possession, and a failed upgrade strands the track forever.** `scanForUpgrades` sets an owned track to `wanted` while its file is still on disk. For the whole window progress bars dip, Lidarr reports `hasFile: false`, and `checkFileIntegrity` skips the file. If the upgrade exhausts its retries, the track sits `wanted`-with-a-file permanently. **This is also the sharpest argument for splitting `status`** — one enum cannot hold "I have this" and "I'm getting a better one" simultaneously. *(Missed by the twelve-section audit; found by the thirteenth review.)* | `scheduler/service.go:236`, `downloader/service.go:472-473` |

### Silent wrongness

| # | Bug | Where |
| --- | --- | --- |
| B12 | **Unknown bitrate scores as top tier.** `QualityTierRank` guards on `bitrate > 0`, so a 96kbps MP3 with an unparseable header satisfies every tier's minimum. The *consequence* is config-dependent: it's marked not-upgradeable only when the matched tier is the top one (MP3-320-first users). With default FLAC-first tiers it stays upgradeable to FLAC, and the damage is that it ranks as a true 320 and is never re-fetched as a proper one. Also: CLAUDE.md's stated reason for `mp3info.go` is backwards — the field preventing a full-library re-download is `download_format`, not bitrate. | `downloader/service.go:733,744-746` |
| B13 | **Relinking to a non-primary provider permanently corrupts the row.** The handler hardcodes `providers.Primary()` and never reads the provider from the body — but the UI renders a provider selector and uses it for the *search*. Pick Deezer → row written as `provider='musicbrainz', provider_id='<deezer id>'` → marked `watched` → the scheduler retries the doomed fetch every interval forever. | `handlers.go:1021,1031-1040`, `ArtistDetail.tsx:276-284`, `client.ts:124` |
| B14 | **Saving settings rewinds the quality-upgrade scanner.** The UI loads every setting into `form` and PUTs `{...form}` back — round-tripping `upgrade_last_artist_id`, the scheduler's round-robin cursor. | `Settings.tsx:82-95,128-133` vs `scheduler/service.go:225` |
| B15 | **`CRATE_MB_USER_AGENT` does nothing.** Loaded into `cfg.MusicBrainzUserAgent`, which has zero consumers. `ProcessManager` injects only `PORT`; the provider reads unprefixed `MB_USER_AGENT`. **Every crate install hits MusicBrainz as `Crate/0.1.0`** — shared-fate ban risk on a limit keyed by User-Agent. Documented as working at `site/docs.html:576`. | `config.go:39`, `process.go:57`, `cmd/provider-musicbrainz/main.go:41` |
| B16 | **Ignored tracks are in the progress denominator, never the numerator.** Ignore three bonus tracks and that artist can never reach 100%. | `queries.go:55-56` |
| B17 | **Reading one Vorbis comment pulls the entire FLAC through RAM.** `flac.ParseFile` → `ReadAll`. `ParseMetadata` exists and reads only metadata blocks. ⚠️ **Correction:** an earlier version of this entry claimed the fix also closes issue #12. It does not — see B17b. | `importer/tags.go:98` |
| B17b | **Issue #12 has three failure classes and `ParseMetadata` fixes one of them.** ID3v2-prefixed FLAC is 9 of 16 skips and needs an ID3-skip shim before `go-flac`; ID3v2.2 MP3s aren't supported by `bogem/id3v2` at all; the "frames do not begin with sync code" error is 2 files and is the only class `ParseMetadata` addresses. Ship the FLAC change alone and close #12, and the reporter's actual complaint — 11 of 13 files — is still broken. | issue #12 |
| B18 | **`IsHealthy` holds `mu.RLock` across a 2s network call**, so a hung provider blocks re-registration. Separately, health checks run **serially** on every Settings/Search/ArtistDetail load — ~4s today with 2 providers, ~14s with v2's seven. ⚠️ **Correction:** an earlier version said `ListProviders` also holds the lock across its calls. It doesn't — it snapshots and releases first. The latency claim stands; the locking claim applies only to `IsHealthy`. | `manager.go:104,119-125` |
| B19 | **`WaitBackground()` is defined and never called**, and shutdown order is inverted — provider processes are killed *first*, while reconcile goroutines still hold their clients. Any `docker restart` mid-reconcile half-completes it. | `router.go:61-63`, `main.go:118-125` |
| B20 | **`ParseProviderConfig` silently drops malformed entries** (bare `continue`). A typo in `CRATE_PROVIDERS` yields a provider that simply isn't there, with no error anywhere. | `process.go:31-33` |
| B21 | **`StartProviders` logs-and-continues when a provider never becomes ready.** Crate boots "successfully" with no catalog provider and every search 500s. Providers also start strictly sequentially with a 10s timeout each — seven failing providers is 70s before the HTTP server even starts. | `process.go:74-78`, `main.go:75,104` |

### Build / release / repo

| # | Bug | Where |
| --- | --- | --- |
| B22 | **A fresh clone cannot build.** `go build ./cmd/crate/` → `pattern all:dist: no matching files found`. `docker build .` from a clean clone fails the same way, and `docker-compose.yml`'s `build: .` is therefore broken. CI papers over it with a `mkdir`+`touch`. | `.gitignore:22`, `cmd/crate/main.go:31`, `Dockerfile:6` |
| B23 | **There is no LICENSE file.** The plan says "all first-party, all MIT" while the flagship public repo is legally all-rights-reserved by default. | repo root |
| B24 | **`.dockerignore` gaps** — two stale 16.2 MB darwin provider binaries plus personal `cache.db` and `activity.db` go into every build context. CLAUDE.md itself says "the `.dockerignore` matters." | `.dockerignore:2` |
| B25 | **Unpinned supply chain in CI** — `gosec@latest` installed on every build, `golangci-lint-action` at `version: latest`. Non-reproducible security gate. | `ci.yml:26,52` |
| B26 | **`TestScoringBalance` never exercises `scoreCandidates`.** It calls the real `tierBasedScore` and `queueScore`, but re-implements the *assembly* — the `+20` artist and `+10` free-slot literals are hardcoded in the test. So a composition bug in `scoreCandidates` survives, and the test would pass through the v2 refactor unchanged while the production path silently inverted. (An earlier version overstated this as "re-implements the formula.") | `service_test.go:523-534` |
| B27 | **Six settings keys are documented as working and are read by nothing** (`slskd_url`, `slskd_api_key`, `library_path`, `scan_interval`, `download_format_preference`, `min_bitrate`), plus an orphan `default_provider` row in the live DB. Three docs claim they work, one claims a key is "masked in the UI" when it's hidden entirely. | `handlers.go:1076-1083`, `README.md:162`, `DATABASE.md:79`, `site/docs.html:655-714` |
| B28 | **Dead config fields** — `config.DownloadFormatPreference` and `config.MinBitrate` are set and never read. | `config.go:23-24,40-41` |

### Undocumented constraints that bite

| # | Issue | Where |
| --- | --- | --- |
| B29 | **The Music Assistant reject watcher requires MA's filesystem root to be byte-identical to crate's library root.** If they differ, every reject silently no-ops — at `slog.Debug`, invisible at the default level. This constraint appears in no doc, and `docker-compose.yml` doesn't even include MA. | `rejectwatcher.go:166-170,215-225` |
| B30 | **`findFile` matches by basename and takes the first hit**, at a default download concurrency of 10. Two concurrent `01 - Intro.mp3` downloads cross-wire silently. **slskd already knows the local path** — `DownloadFileCompleteEvent` carries `LocalFilename`, deliverable by webhook. Crate never asks. | `organizer/service.go:167-183` |
| B31 | **`PROVIDERS.md` is stale** — documents 5 RPCs; the proto has 6 (`SearchArtistTracks` missing). | `PROVIDERS.md:10-17` |
| B32 | **ADR-0007 documents a matching ladder `reconcile.go` doesn't implement.** The ADR claims recording-id → release-track → title-fold; the code is title-fold with a disc/track tiebreak only — and it *can't* do better, because `pb.TrackInfo` carries no recording id and `ListTracksByAlbum` doesn't select one. | `reconcile.go:177-191`, ADR-0007:33 |
| B33 | **Dry-run counts are not exact**, contradicting CLAUDE.md. Two independent causes: dry run can't see its own would-be writes (duplicate `(album,title)` files miscount), and a dry-run new artist gets `ID = -1`, which disables the album and track lookups entirely so everything counts as added. | `importer/service.go:232-233,260,306,312` |

---

## Corrections to the plan

Things `V2-Plan.md` states that are wrong. Ordered by consequence.

### C1 — Quality-upgrade ordering is inverted into a data-loss order

`V2-Plan.md:340` says `Remove(old_ref)` then `Place(new)`. **v1 does the opposite and v1 is right**: rename the new file into place, *then* delete the replaced one. Delete-first means a `Place` that fails after the `Remove` succeeded leaves the user with **neither file**, on an operation labeled "upgrade" — and on Soulseek the source peer may never come back.

Required everywhere: **`Place` → persist the new ref → `Remove(old)`.** Caught independently by two reviewers.

### C2 — Ref backfill would hand `sleeve` delete rights over files it never placed

`V2-Plan.md:546` says refs backfill from paths. But `file_path` is **relative when crate placed it, absolute when the importer adopted it** — absolute means a user-managed file outside the library, and there is a test that exists solely to protect those. Rule: **relative ⇒ backfill a ref; absolute ⇒ no ref, ever.**

Stronger recommendation from the state-machine review: **don't backfill in SQL at all.** Crate doesn't know which ingest provider the user will configure. Adopt refs on first boot from a provider-driven `Resync`/`Identify` pass, matched against the existing `file_path` cache.

### C3 — `MetadataProvider` does not exist

The service is **`MusicProvider`**. `MetadataProvider` appears **zero times** in the tree. `V2-Plan.md:52` states an entire section's premise against a symbol that isn't there.

Worse, and found independently by three reviewers: **the provider registry has no concept of provider *kind*.** Not in `ProviderConfig`, not in `CRATE_PROVIDERS` (`name:binary:port`), not in `InfoResponse`, not in the DB, not in the UI. It's a flat `map[string]*providerConn` holding a `pb.MusicProviderClient`. So the Settings "Default Provider" dropdown would happily offer `sleeve` as your catalog provider, and `handleUpdateSettings` validates nothing.

**Consequence: phase 2 is not "near-zero blast radius" — it is the forcing function for a registry refactor that appears nowhere in the plan.**

Free win: **take provider kind from `Info` rather than config**, and the `CRATE_PROVIDERS` breaking change at `V2-Plan.md:641` evaporates entirely.

### C4 — The inbound reject key is unsafe

`V2-Plan.md:483` claims `POST /api/tracks/reject` takes `{artist, title}` — "exactly what MA hands you." Both halves are wrong. MA's payload has **no artist field**, and that endpoint is `WHERE artist = ? AND title = ? LIMIT 1` with **no album scope**, on a **destructive** operation. "Creep" exists on *Pablo Honey*, reissues, and three compilations. Wrong match = wrong file deleted and a good source permanently blacklisted.

**Fix: send evidence, not a key.** A locator bundle (`path?`, `mbid?`, `artist?`, `title?`, `album?`, `duration_ms?`) resolved by crate's own matching ladder, refusing on ambiguity. That ladder already exists and is already tuned.

### C5 — The `release.yml` fix in the plan is broken, empirically

`V2-Plan.md:135-136` proposes `${{ contains(...) && '--prerelease' || '' }}` with a `\` continuation in a **plain** YAML scalar. YAML parses first, and the trailing backslash-space folds into a backslash-escaped space, which bash turns into a literal-space word, which `gh release create` treats as an asset path.

**Both branches fail** — prerelease and GA. Verified with PyYAML + bash argv inspection. It only works under a block scalar (`run: |`).

Two more release bugs nobody caught: `--generate-notes` uses the previous *release*, so v2.0.0 GA's notes would cover only `rc.N → GA` (needs `--notes-start-tag`), and **there is no `{{major}}` docker tag**, so nobody can pin `:1` or `:2` — while `README.md:33` tells every user to run `:latest`, straight into a one-way migration.

### C6 — Tagging `v2.0.0` permanently breaks `go get` for the contract

Go requires a `/v2` module path suffix for major version ≥ 2, and the `+incompatible` escape hatch does **not** apply to modules with a `go.mod` — crate has one. After the first v2 tag, `go get .../proto/provider@latest` **silently resolves to v1.20.1 forever**. A provider author writing against v2 compiles against the v1 contract.

**This does not require a repo split.** Make `proto/` a nested Go module inside the existing repo. Same clone, zero satellite ops, and the contract module stays at v1 regardless of crate's major version.

**It must land before the first v2 tag, RCs included.** The plan puts it at phase 8.

### C7 — BSR does not publish to public package registries

`V2-Plan.md:682-684` claims the Buf Schema Registry publishes SDKs "to Go modules, PyPI, npm, Maven, Cargo, NuGet, and Swift Package Manager." It publishes to **Buf-hosted** registries — Python users need `--extra-index-url https://buf.build/gen/python`, npm users need to repoint the `@buf` scope. That's *more* friction than running `protoc`, not less. Go is the only language where it's transparent, and Go authors already work today.

**But `buf lint` + `buf breaking` are genuinely valuable — and the existing proto fails 5 of 6 buf STANDARD rules** (package/directory mismatch, `SERVICE_SUFFIX`, request/response naming, shared request message). That's a full service + message rename, which is a contract break — and the only free moment to make it is *before* the v2 contract is public.

**Decision: publish stubs to GitHub Packages instead of BSR.** One honest caveat — GitHub Packages supports **npm, Maven/Gradle, NuGet, RubyGems, and the container registry. It does not support PyPI or Cargo**, and its Go registry was deprecated. So:

- **Go** needs nothing — a public repo already resolves through `proxy.golang.org`. This is the only language with a first-party provider today.
- **npm / Maven / NuGet** are covered by GitHub Packages.
- **Python and Rust are not covered by anything.** Those authors run `protoc` or `buf generate` against the committed `.proto`, which is the same thing they'd do with BSR minus the custom index URL.

That's a strictly better outcome than BSR for the same effort, as long as nobody promises Python/Rust packages in the docs.

### C8 — "Crate never re-acquires on an inference" is already violated, four ways

The invariant is good. It is not currently true: reject marks `wanted` after a refused delete (B6); reject-by-name is an identity inference on a destructive path (C4); `handleQueueTrack` has no status guard (B11); and **passthrough's find mode violates it by design** — `V2-Plan.md:436` admits a file retagged beyond recognition gets re-downloaded.

Honest restatement: **"Crate never re-acquires on an inference *it* made. A provider may declare a file gone, and that declaration is authoritative."** That relocates the risk into the contract, where it belongs with a MUST about not declaring loss cheaply.

And the missing sibling, right as the plan removes the global `library.Contains` gate: **"never delete on an inference."** That's the more expensive of the two to get wrong.

### C9 — `V2-Plan.md:393` describes something a provider cannot do

It says passthrough's find mode reconciles "by the same recording-ID → release-track → title ladder crate's importer already uses." **That ladder is three SQL queries against crate's tables.** A provider has no database, and the plan forbids it one. Find mode is a file-tags-to-file-tags match — a different algorithm with different failure modes, never specified.

### C10 — `staged` and `lost` cannot be set, because `Place` can't say what it did

Providers never write crate's DB, so crate must decide at `Place`-response time whether the file was placed or left alone. **`PlaceResponse` has no such field.** Crate can only distinguish sleeve from passthrough by branching on the provider's *name* — the exact coupling the architecture exists to prevent.

### C11 — Ledger mode's "zero special-casing" claim is false

`V2-Plan.md:423-428` says crate needs no special-casing because ledger's `Resync` just answers "everything's fine." Checked method by method: **1 of 5 holds.** `Search` returns empty while the UI offers recovery that can never succeed; `Found` must either read files (violating its own mode) or adopt blindly; `Remove` needs a distinct "unsupported by policy" result; and **quality upgrades are not a no-op at all** — `IsUpgradeable` runs off the track row with zero provider involvement, so the scheduler flips owned→wanted and re-enqueues on its own. Crate must **actively suppress** them.

### C12 — Adding statuses to `status` is a table rebuild with three traps

`V2-Plan.md:546` calls the migration "close to trivial." Adding `staged`/`lost` to the `CHECK` constraint requires SQLite's 12-step rebuild, and:

- goose runs migrations in a transaction, and **`PRAGMA foreign_keys` is a no-op inside a transaction**
- the DB opens with `foreign_keys(on)`, so `DROP TABLE tracks` cascades → **wipes the download queue**, orphaning in-flight transfers
- the `ALTER TABLE RENAME` variant is worse — SQLite 3.25+ rewrites FK references, so the rename re-points `download_queue` at `tracks_old` and the drop cascades anyway

**Alternative that dodges all of it: don't touch `status`.** Add `ingest_state` as a new unconstrained column — pure `ADD COLUMN`, matching every migration since 002. It also makes a v2→v1 rollback completely clean, because v1 selects by explicit column list and ignores unknown columns.

The state-machine reviewer argues this is the right design outright, not just the cheaper one: **the failure mode of new status values is invisibility.** Miss one of ~20 call sites and a track vanishes from progress, from Lidarr `hasFile`, from `FindTrackByPath`, from the artist page — and nobody files a bug about a track they can't see. Miss an `ingest_state` guard and you get a duplicate download: visible, annoying, harmless.

### C13 — v2 is one-way and nothing says so

`goose.Up` runs unconditionally inside `Open()`. Down migrations exist in all ten files and **nothing can invoke them** — there are zero `flag.`/`os.Args` references in `cmd/`. Rolling the NAS back to `1.20.1` against a v2 DB **boots successfully** and then runs v1 logic against a v2 schema.

There is no `HEALTHCHECK` and no pre-migration backup. `crate.db` is 256 KB. **Copying it immediately before `goose.Up` is the highest-value, lowest-cost change in the entire plan** and it isn't in it.

Also: migration happens at `main.go:39`, *before* providers start at `:75`. The irreversible step runs first. If a provider then fails hard, you `os.Exit(1)` with a migrated DB and a crash loop.

### C14 — `Identify`'s return shape cannot feed the matching ladder

`{path, title, artist, album, mbid, confidence}` is missing six scalars (`track`, `disc`, `year`, `duration_ms`, `format`, `bitrate`) and collapses **four distinct MusicBrainz namespaces** — artist, release-**group**, release-track, recording — into one ambiguous `mbid`. Those map to three tables plus one separate signal column.

Ship it as written and ADR-0005's top rung dies, MB-tagged libraries stop landing under `musicbrainz`, and every import becomes a `local` import requiring manual promotion. Needs a namespaced `external_ids` map.

Two semantics that must be contract-level, not implementation detail: **`artist` MUST be album-artist when tagged** (otherwise crate's grouping key shatters every compilation into one artist per track), and **`bitrate` needs a distinct UNKNOWN sentinel** separate from the lossless-is-0 convention (see B12).

Also: **`confidence` has no consumer.** Every rung of crate's ladder is exact equality. Define what crate does with it or drop it.

### C15 — `catalog_claim` is missing `cover_url`

The organizer reads it from `albums.cover_url` and hands it to the tagger. Ship the claim as specced at `V2-Plan.md:249-250` and **sleeve silently stops embedding cover art on day one of phase 3.** Also missing: `albumartist` (a distinct naming token), duration, and total-track-count.

### C16 — The capability enum gates nothing

All four values were audited and none changes crate behavior. `BATCH_PLACE` has no RPC behind it. `OFFLINE_IDENTIFY` has no stated consequence. `REQUIRES_RELEASE` exists for a provider the plan's own research disqualified. And `STABLE_REFS` **contradicts the plan**: `V2-Plan.md:429-432` says passthrough not declaring it is *why* a vanished file goes to `lost`, but `V2-Plan.md:239-241` already makes `lost` unconditional for any provider.

Recommendation: ship `enum Capability { CAPABILITY_UNSPECIFIED = 0; }` plus a reserved range. Values arrive with their consumers. Trade the saved surface for the `Place` outcome enum (C10).

### C17 — The push channel is broken three ways

- **`item_imported` does not fire for album imports.** beets' docs, verbatim. A hook wired as `V2-Plan.md:416` describes catches **zero** of a normal `beet import`. The right events are `item_moved`/`item_copied`, and there are **six** of them — one per import mode — so a correct recipe is five hook registrations.
- **The port isn't reachable.** Provider gRPC ports bind to localhost as child processes; the Dockerfile exposes only 6969. A provider-side endpoint needs a new `EXPOSE`, a compose mapping, and a k8s Service port.
- **Container paths don't survive.** Crate sees `/app/library`, beets sees `/mnt/music`.

Plus: **beets defaults to `copy: yes, move: no`**, so in the default config the staging file never moves at all — which quietly changes what every tracking mode does.

Two reviewers independently concluded: **cut it, or route it through crate's existing HTTP API** (`POST /api/ingest/relocated {old_path, new_path}` → crate resolves via `FindTrackByPath` → calls the provider's already-required `Found`). Zero new ports, zero new proto, and it works for sleeve users too — the plan scoped it to passthrough for no reason. This also matches the decision the plan already made for notification inbound events at `V2-Plan.md:478-486`, which the passthrough section contradicts.

### C18 — `strict` mode contradicts the plan's own doctrine

`V2-Plan.md:392` says strict "re-downloads forever." `V2-Plan.md:233-241` says `lost` is the safe default for any missing file from any provider and crate never re-acquires on an inference. Both cannot be true. Under the `lost` rule, strict produces an **unbounded manual-triage queue**, which is worse than what it was meant to prevent.

Both passthrough reviewers also argue **`find` should be the default**, not strict: the population that selects passthrough is by definition the population running an external tool.

### C19 — The plan contradicts itself on per-file vs per-release

`V2-Plan.md:596` rejects wrtag *because* the \*arr ecosystem gets complete releases by construction while Soulseek is per-file — "crate is genuinely the different case." `V2-Plan.md:510-512` then justifies a Soulseek-shaped download contract on "slskd stays first-class; other implementations adapt." A torrent provider delivers one artifact satisfying 14 rows, which the per-track state machine cannot express.

**Stop selling the download contract as pluggability. Sell it as extraction**, and ship a second implementer that actually validates it.

### C20 — Phase 0 is four bugs, not two

Beyond the two the plan names: `{{major}}` docker tag missing (C5), and the `\`-continuation fix being broken (C5). Plus everything in the build/release section of the v1 bug list.

Also worth knowing: the plan's claim that `{{major}}.{{minor}}` "already no-ops on prereleases" reaches the right conclusion by the wrong mechanism — metadata-action **falls back to the full version** for prereleases and the duplicate dedupes. `2.0` doesn't move, but the stated reason will mislead the next reader. And `flavor: latest=auto` does **not** suppress `latest` for prereleases — the explicit `enable=` guard is the only fix.

---

## Contract gaps

Things the proto needs that `V2-Plan.md` doesn't specify. Consolidated from all twelve reviews.

### IngestProvider

| Gap | Why |
| --- | --- |
| **`PlaceResponse.disposition`** (`PLACED` / `LEFT_IN_PLACE`) | Without it `staged` is unreachable without hardcoding provider names (C10) |
| **`Place` outcome enum** (`PLACED` / `HELD` / `REJECTED` / `FAILED`) | The two-claims model's whole payoff — "I fingerprinted this and it isn't what you said" — has no return path |
| **`Place` idempotency key** | A gRPC deadline that may or may not have succeeded is now the *normal* case, and today's destructive step and record-keeping step are ~10s apart in one process |
| **`CheckAccess(paths)` at registration** | The failure users will actually hit isn't remote hosts — it's two co-located containers mounting the same host dir at different container paths. A capability flag can't detect that; a token-file probe can, turning a silent per-track failure into a startup error |
| **`Remove` result taxonomy** | Must distinguish already-gone (success-equivalent) / retryable failure / refused-by-policy. Three different UI states, one error today |
| **Full `IdentifiedFile` schema** | See C14 |
| **`cover_url` + `albumartist` + duration on `catalog_claim`** | See C15 |
| **`contract_version` on `Info`**, distinct from the provider's own version | `Version` is display-only today, compared against nothing, and both first-party providers hardcode `"1.0.0"`. An unimplemented method surfaces as a runtime `Unimplemented` — mid-`Place` |
| **`kind` on `Info`** | Turns a `CRATE_PROVIDERS` typo into a startup error instead of a runtime `Unimplemented`, and removes a listed breaking change (C3) |
| **`sibling_paths` on `Place`** | Soulseek delivers folders — `cover.jpg`, `.cue`, `.log`. v1 moves exactly one audio file. Cheap now, contract-breaking later |
| **`supported_extensions` on `Info`** | Closes the plan's own known gap for free: crate can refuse to auto-download what no configured provider can tag |
| **Resync delta must carry the current path** | `file_path`-as-cache has no invalidation rule. Miss it and the MA reject watcher silently stops matching after any provider-side move |
| **Per-item `skip_reason` on the `Identify` stream** | Crate's skipped-files UI depends on it |
| **A stream header with the total** | `State.Total` is set from `len(files)` before any work; a stream has no length |
| **Concurrency semantics** | Crate may issue concurrent `Place` for distinct sources; providers MUST be safe for that and MUST serialize identical sources |
| **Per-RPC deadlines** | `Manager` never sets a deadline — it passes the caller's ctx straight through, so background calls are unbounded |
| **Ref constraints** | Max length, encoding (paths aren't guaranteed UTF-8), and a MUST that providers treat refs as untrusted input. With the `library.Contains` gate moved provider-side, a corrupted ref is an arbitrary-delete primitive |
| **A unique index on `(ingest_provider, ingest_ref)`** | Two rows can already share a path (B5). Under v2 that's two rows sharing a ref, and rejecting either destroys the other's file |
| **Resync deltas applied as compare-and-swap on the ref** | Mid-upgrade, a delta for the old ref generated before the swap marks a healthy track `lost`. Also skip tracks with an active queue row |

### Cross-cutting

- **`ConfigResult` needs per-field errors.** Validation lives with the provider, so it's a round trip; one error string makes a 12-field form unusable.
- **`ConfigField` needs `requires_restart`.** Eight settings hot-reload today; every one silently becomes restart-required unless push-on-save is a contract MUST.
- **`ConfigField` needs `PATH`** (4 users, highest blast radius, only type with real validation semantics) and a **repeated/list type** for `library_roots`. `BOOL` and `TEXT` have **zero** users.
- **Conditional field visibility.** passthrough's library dir is required in find mode and meaningless in ledger mode — not expressible today.
- **Ledger mode is a config value, not a capability**, so the frontend would have to hardcode one provider's config semantics to honor `V2-Plan.md:402`. Needs effective-behavior flags (`supports_upgrade` / `supports_reject`) computed after config is applied.
- **Three HTTP endpoints the UI cannot function without are specced nowhere**: provider config schema + values, provider capabilities, and lost-file search/found. The plan defines all three as gRPC between crate and the provider and never crosses the HTTP boundary the frontend actually consumes. Plus a job-cancel endpoint — a multi-hour operation with no cancel is unacceptable on a phone.
- **Provider persistent state is unaddressed.** Ledger mode requires a ledger; providers can't write crate's DB. So passthrough needs its own store → volume → backup → migrations. This is a packaging blocker, not a detail.

---

## Testing — the suite is aimed at the wrong half of the codebase

There are 238 tests. **Every package v2 rewrites, relocates, or hands to a third party has zero.**

| Package | Go LOC | v2 fate |
| --- | --- | --- |
| `internal/provider` | 437 | 2 child processes → 5+; the cited architectural precedent |
| `internal/services/scheduler` | 275 | `checkFileIntegrity` moves into providers |
| `internal/services/slskd` | 192 | becomes a satellite |
| `internal/services/reject` | 78 | `DeleteFile` → `Remove(ref)`; deletes can now fail |
| `internal/config` | 58 | `CRATE_PROVIDERS` format changes |
| `internal/library` | 51 | the `Contains` safety gate becomes the *provider's* job |
| `internal/migrations` | 10 files | the v1→v2 ref backfill |

**1,091 lines, zero tests between them.** Meanwhile 133 of the 238 tests cover HTTP handlers that barely change. The suite isn't weak — it's pointed at the stable half.

**The most dangerous untested function in the repo is `checkFileIntegrity`.** It stats every owned track and flips missing ones to `wanted`, which re-queues a download. The plan itself calls that behavior *"wrong and dangerous"* and inverts it to `lost`. With zero tests: nothing pins today's behavior, so the inversion is invisible in the diff; nothing catches a regression back; and after the move, that regression would arrive via a **provider update**, not a crate change. Failure mode: re-download the entire library.

### The real risk is that the suite stays green

| Bucket | Count |
| --- | --- |
| Relocate to satellites, ~intact | 35 |
| Split across the process boundary | 10 |
| Re-typed, semantics unchanged | 18 |
| Survive untouched | 42 |
| **Quietly stop meaning anything** | **~20** |

`newTestEnv(t)` is called 128 times but always through one helper, so a signature-preserving refactor breaks **zero test bodies**. You will not hit a red wall.

That's the problem. **~20 tests keep asserting `file_path` — which survives as a cache — while ownership moves to refs underneath them.** They pass, they mean less, nobody notices. Budget effort on new named tests, not on breakage triage.

### Two structural blockers

**`newTestEnvWithLibrary` is already a 48-line verbatim copy of `newTestEnv`**, differing in exactly one argument. One optional dimension forced a copy-paste fork; v2 adds four (ingest, download, notification, config schema). A variadic options signature fixes it and breaks nothing, because all 128 call sites pass no arguments.

**`api.NewServer` constructs `importer` and `reject` internally** from `libraryDir` — and those are precisely the two things that become provider calls. **There is no seam to inject a fake ingest provider.** You cannot write a v2 API-layer test without changing that constructor. Land it in **phase 2**, on the cheapest provider kind, or you'll be refactoring the harness while inverting ownership semantics simultaneously and won't know which change broke what.

### The ADR-0004 regression test can't catch the bug it exists for

It seeds `REPLAYGAIN_TRACK_GAIN = "-5.30 dB"` — **two decimal places**. The beets defect that disqualified it was truncation at *six* (`0.98765432` → `0.987654`). A writer with that exact bug passes cleanly. Also: neither FLAC fixture carries a genuinely foreign tag (both seeded keys are ones crate knows), and neither fixture is real audio — so byte-for-byte audio preservation is untested. For a binary other people will run, that matters more, not less.

### The conformance suite already half-exists

`cmd/provider-musicbrainz/main_test.go` registers the **real** server implementation on a real loopback listener and hands back a real client. The assertion bodies never know whether the server is in-process or remote. **Parameterize that constructor to take an address and the same tests become an external conformance runner.** Ship it as `crate-proto/conformance` so satellites can import it — that's a hard ordering constraint on the repo split, since satellites have nothing to test against otherwise.

**Use real spawned binaries over loopback, not bufconn.** Crate already does exactly this in production (`ProcessManager` execs providers with `PORT` in env), and an e2e test would be testing the real startup path, which is untested today. bufconn is an in-memory pipe: no real deadlines, no connection loss, no separate process to kill, and — decisively — **no distinct filesystem view**. Path mismatch is the new bug class this architecture introduces, and a bufconn harness structurally cannot produce it. A green bufconn suite would be actively misleading.

### The four artifacts that make phase 3 safe to ship

1. **`go test -race -timeout 10m ./...`** — one line, phase 0. Without `-timeout`, a hung provider handshake burns the CI job to GitHub's 6h ceiling.
2. **A characterization test pinning today's missing→`wanted`** in `checkFileIntegrity`, so the inversion to `lost` is a conscious, reviewable test edit. ~60 lines, works today.
3. **`newTestEnv(t, ...envOpt)` + `NewServer(deps)`** — the harness seam, phase 2.
4. **A committed v1 `crate.db` fixture + one migration test** — owned-count preserved, every owned track gets a resolvable ref, and **relative vs absolute paths handled differently**. Keep the weird rows on purpose; the synthetic-seed alternative is worse precisely because the bugs live in rows you wouldn't think to synthesize.

Add a fifth if phase 3 ships the upgrade path: the destroy-before-create test (C1). ~40 lines, and it's the difference between "a bug" and "the library is gone."

---

## Cross-section conflicts

Where reviewers disagreed, or where one section's design breaks another's.

| # | Conflict |
| --- | --- |
| X1 | **Reject is destroy-before-record, and v2 removes its safety net.** Today a crash between the file delete and the DB write self-heals — `checkFileIntegrity` reverts the track to `wanted`. Under v2, integrity checking moves into `Resync`, and passthrough in ledger mode "always answers everything's fine." A crash mid-reject then leaves a permanently `owned` row pointing at a deleted file with nothing to correct it. **Two sections that each look fine in isolation.** |
| X2 | **Who deletes the replaced file on an upgrade?** The plan says crate calls `Remove(old_ref)`. But sleeve *is* v1's organizer, which deletes the replaced file itself inside the move. Doing both is a double-delete; doing neither orphans. Exactly one owner, stated in the contract. |
| X3 | **`internal/library` ownership.** Two reviewers reached different conclusions — one says fork it into both providers (14 lines of pure `filepath` logic, sharing it would make every provider depend on a crate package), the other says publish it alongside `crate-proto` so the "MUST NOT delete outside the root" clause has a reference implementation. Both agree crate keeps a copy for the `file_path` cache. |
| X4 | **`external` providers per kind — resolved, and the first answer was wrong.** First-party providers ship in one image, but that's packaging convenience, not architecture: they're separate processes on an address, and a third-party provider runs in its own image via the `external:host:port` config that already exists. The real constraint is filesystem, and only for ingest — `Place` takes a path. But "separate container, same host, same volume at the same mount path" works fine and is exactly the third-party shape; only "separate host" genuinely breaks. **An earlier version of this entry said refuse external ingest categorically, which banned the working case to prevent the broken one.** `CheckAccess` distinguishes them: same-host-different-container passes, different-host fails loudly at registration. **External ingest providers are allowed; registration fails if the probe fails.** `PROVIDERS.md` still needs rewriting — it advertises `external:host:port` with no caveat, documents 5 RPCs when the proto has 6, and says nothing about locality. |
| X5 | **`file_path` as cache.** The plan justifies keeping it to save reject a round-trip — but reject uses the *ref*. The real consumer is `FindTrackByPath`, the MA reject watcher's only mapping, keyed by a path third parties know. So the cache must be updated from Resync deltas or MA reject silently dies. |
| X6 | **The download provider and passthrough independently need the same "provider exposes inbound HTTP alongside gRPC" pattern.** Design it once. Both reviewers, separately, proposed routing it through crate instead. |
| X7 | **Phase ordering disputes.** Provider-config says split phase 1 (transport now, schema after passthrough's field list is real, because the schema is currently being designed blind against navidrome's three string fields). Download says split phase 6 (Go interface early, gRPC last — 80% of the value at 10% of the risk). Passthrough says split phase 4 (refs with sleeve, statuses with passthrough, because passthrough is the only producer of `staged`). Ingest-contract says invert 3 and 5 (ledger-mode passthrough first as a conformance canary, before sleeve locks the contract in). **These are all the same argument: the plan's phases bundle a cheap, reversible half with an expensive, irreversible one.** |
| X8 | **The state machine must ship `lost` in phase 4, not phase 5.** The moment `checkFileIntegrity` is deleted, *something* must handle a missing file. Ship phase 4 without `lost` and there is no reaper at all — the DB drifts silently. |

---

## Decisions needed

Ordered by how much they block.

1. **Statuses or `ingest_state`?** (C12) — determines the migration's risk profile and whether rollback is clean.
2. **Does the repo split happen at all?** The release-infra review argues **no** — split the *module* (nested `proto/go.mod`), keep the providers in one repo indefinitely. Seven repos is 7× ops surface for one maintainer, fragments issue triage, breaks single-clone reproducibility, and the one thing it buys that a module split doesn't — letting someone else own a provider repo without commit access — is a governance need that doesn't exist yet.
3. **Adopt buf lint + breaking now?** (C7) — the renames are contract breaks and this is the only free window.
4. **Cut the push channel, or route it through crate?** (C17)
5. **Two tracking modes or three?** (C18) — and which is the default.
6. **Is `Search`/`Found` required in v2.0?** Two reviewers say defer to 2.1 — but only if `Place` gets an idempotency key, because otherwise `Search` *is* the recovery path for a lost `Place` ack.
7. **Does `lost` count toward progress?** Three consumers: the SQL aggregate, `/api/status`, and Lidarr `hasFile`. Dropping 400 tracks out of a progress bar because someone `mv`'d a folder is alarming for no reason — but it's a product judgment, and it should be a decision rather than a default.
8. **Who owns the library root?** Crate needs it for the Lidarr shim's root folder and `statfs`; sleeve needs it for placement. Two sources of truth for one path is how files get written outside the safety gate.
9. **Frontend tests?** There are none — no runner, no vitest, nothing. CLAUDE.md's "always add tests, CI gates on this" is true for Go and vacuous for React. ~1,200 lines of new status-branching, capability-gated UI changes that calculus.

---

## Convergent findings

Where independent reviewers, given different sections, hit the same thing. This is the strongest signal in the audit.

| Finding | Found by |
| --- | --- |
| **`retryOrganize` has never worked** | sleeve, ingest-contract, download |
| **Quality-upgrade ordering is inverted into data loss** | sleeve, state-machine |
| **`IdentifyResult` must be field-for-field `importer.fileMeta`** | ingest-contract, reconciliation |
| **The registry has no provider-kind concept** | catalog, notification, ingest-contract |
| **`IsHealthy` holds a read lock across a network call** | catalog, ingest-contract |
| **The inbound reject key is unsafe** | notification, state-machine |
| **Ingest providers must share crate's filesystem; `external` must be refused for that kind** | ingest-contract, catalog, download, passthrough, release-infra |
| **Hot-reload regresses unless `SetConfig` is pushed on save** | provider-config, notification, download, passthrough, state-machine |
| **Something needs to be a second implementation to keep each contract honest** — `crate-notify-webhook`, `crate-download-local`, ledger-mode passthrough | notification, download, ingest-contract |
| **The plan's phases bundle a cheap reversible half with an expensive irreversible one** | provider-config, download, passthrough, ingest-contract, reconciliation |

---

## Measurements worth taking

Cheap, and each one settles a question the audit couldn't:

- **`uname -m` on the Synology.** The DS923+ ships an AMD Ryzen R1600 — **x86_64**. If so, every second of QEMU arm64 in CI is spent for other users, not for Joey's deployment.
- **A dry-run import against the real NAS library**, reading `MusicBrainzLinked` against `TracksAdded`. `soleKey` is strict — one untagged file demotes an entire artist to `local`. If most albums land as `local`, ADR-0007's manual promotion becomes the *primary* path rather than the fallback. **One API call against running v1, and it would tell you more about v2's import UX than any further code reading.**
- **Production DB state** — goose version, status counts, and the relative-vs-absolute `file_path` split. Determines the migration's actual risk.
- **Point slskd's `DownloadFileComplete` webhook at a test endpoint and pull 10 files concurrently.** This is the load-bearing assumption under the best find in the audit (B30).
- **Time the importer against a synthetic 100k tree.** Determines whether the scheduled sweep needs to be incremental before it ships or after.
