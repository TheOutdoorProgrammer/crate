---
dusk: v1alpha1
namespace: stout
kind: repository
name: crate
title: Crate
attributes:
  language: go
  public: true
  url: https://runcrate.dev
---

Self-hosted music manager, roughly a Lidarr alternative: search a metadata provider for an artist, watch their discography, and let a background downloader find, score, fetch, organize and tag the files through slskd.
Go backend, React SPA in `web/`, SQLite for everything.

`cmd/crate/` is the entry point and `cmd/provider-*/` is one standalone binary per metadata provider.
Providers are separate processes speaking gRPC, supervised by the main binary, which is the layout worth understanding first: adding a provider is a new binary plus a `CRATE_PROVIDERS` entry, not a change to Crate.
Each stored entity remembers which provider and provider ID it came from, and the frontend is deliberately provider-unaware, so the backend resolves the provider from settings or from the entity itself rather than being told.
The background work lives under `internal/services/`, one package per job (downloader, scheduler, importer, organizer, tagger, and the optional Navidrome and Music Assistant integrations).

`CLAUDE.md` is the deep reference and is far more detailed than the README: scoring invariants, the blacklist and shadow-ban rules, the naming template, and the import and reconcile behaviour.
`docs/adr/` holds the decisions with real trade-offs behind them, and they are worth reading before changing scoring, manual search, or the importer.

## Not in the observed half of the catalog, on purpose

Crate runs as a container alongside slskd, on the NAS that holds the music library, rather than in the Kubernetes cluster.
Its Kubernetes namespace contains only ExternalName services: one so DNS resolves, one so ingress can terminate TLS and proxy to the NAS.

There is no workload for the cluster plugin to observe, so an entry in the observed half of the catalog is not expected and its absence is not drift.

## Gotchas

**Crate's downloads volume must be the exact host directory slskd writes completed downloads to.** They are two containers agreeing on a path by convention only, and if they disagree Crate simply never finds anything it downloaded, with no error that points at the cause.

**The tagger is non-destructive on purpose.** It overwrites only Crate's own fields and preserves everything else, because Music Assistant writes ReplayGain values and a MusicBrainz recording ID back into the same files. The older "write a fresh tag block" approach silently wiped them, and there are regression tests specifically holding that line.
