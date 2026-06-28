# Free Music Pivot — Overview

## Problem
The indexer shipped with a mix of 14 hand-curated Internet Archive collections plus
user-imported "patron list" playlists. The project is pivoting to a focused **Free
Music** indexer: only two source collections (Musopen, Netlabels), and only tracks
whose license is one we can actually use commercially.

## Current Behavior (before)
- `internal/db/seed_collections.json` seeded 14 per-collection queries on first launch.
- Patron-list / simplelist playlists could be imported via the TUI.
- The license worker classified each album's `licenseurl` after analysis and surfaced a
  "% Free" metric, but nothing *excluded* non-free albums.

## Research Findings
- Discovery is fully query-driven: `discoverCollection` (`cmd/tui/main.go`) feeds
  `collections.query` straight into IA's cursor-paginated scrape API. A synthetic
  collection is just a `collections` row — **no discovery code change needed**.
- **Wildcard `licenseurl:` filtering is rejected by IA's Elasticsearch backend.**
  Validated live: `*creativecommons.org/...` (leading), anchored/trailing wildcards, and
  bare-token matches all fail with `[BACKEND_ERROR] Invalid or no response from
  Elasticsearch` or return 0. Only exact full-URL match works, which is impractical given
  the variant explosion (http/https × versions × country ports × trailing slash).
- Therefore the license filter **cannot** live in the IA query. It must be applied
  in-app, reusing the existing `ia.ClassifyLicense` / `ia.IsCommerciallyUsable` policy
  (free = pd, cc0, cc-by, cc-by-sa).

## Design Proposal
1. Replace the seed with two query-backed collections scoped to the collection + audio:
   - `musopen-free`: `mediatype:audio AND collection:musopen AND -subject:"spoken word" AND -mediatype:audiobook`
   - `netlabels-free`: `mediatype:audio AND collection:netlabels AND -subject:"spoken word" AND -mediatype:audiobook`
2. Add an in-app **free-only gate** in the album resolver: after metadata fetch (which
   already includes `licenseurl`) and before track insertion, classify the license; if it
   is not commercially usable (NC/ND/other/unknown), store the license, mark the album
   `unavailable`, and return early — skipping all downstream download/embedding/scoring.
3. Gate is controlled by a new `--free-only` flag (default **true**, env `FREE_ONLY`) for
   reversibility.

## System Changes
- `internal/db/seed_collections.json` — replaced with the two collections.
- `internal/config/config.go` — `FreeOnly` config + `--free-only` flag + `envOrDefaultBool`.
- `cmd/tui/main.go` — thread `freeOnly` through `albumResolverLoop` → `resolveAlbum`; add
  the license gate after the `IsMusicContent` check.
- Tests: `internal/db/seed_test.go`, `internal/ia/license_test.go`.

## Why the gate lives in the resolver (not the license worker)
The existing license worker only claims albums that already have a **completed** track, so
classifying there would happen *after* the expensive download + CLAP embedding work. The
resolver already fetches `licenseurl` up front, so gating there avoids wasting work on
non-free albums entirely.

## Validation
Live IA `advancedsearch` counts for the final queries: musopen-free = 34,
netlabels-free = 76,477. Both query forms (including the `-subject`/`-mediatype`
negations) are accepted by the ES backend.
