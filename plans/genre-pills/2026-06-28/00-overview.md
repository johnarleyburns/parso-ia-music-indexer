# Genre Pills — Overview

## Problem
The Parso/Lorewave listener app needs a TikTok-style "click what I'm interested in"
genre **pill** set for cold-start discovery. Pills must be grounded in what is
actually indexed, return real similar music, and stay tunable as the library grows.

## Current Behavior
- Two active query collections (`internal/db/seed_collections.json`):
  - `musopen-free` (classical) — effectively unindexed (1 listenable album).
  - `netlabels-free` (electronic) — ~99% of the indexed library.
- Genre signal lives in `albums.subjects` (1124/1353 populated), NOT `albums.genres`
  (only 5/1353 populated).
- A pill-scoring engine already exists: `db.SearchByText` =
  `0.50*CLAPSimilarity + 0.50*ComputePillScore` (`internal/db/embeddings.go`,
  `internal/db/pill.go`). CLAP text embeddings come from the sidecar
  (`clap.GetTextEmbedding`). There is **no defined pill catalog**.

## Research Findings (115 listenable albums, album-level keyword coverage)
| Pill | netlabels | library |
|---|---|---|
| Experimental / Noise | 38.6% | 38.3% |
| IDM / Electronica | 36.8% | 36.5% |
| Ambient / Drone | 31.6% | 31.3% |
| Techno / House | 25.4% | 25.2% |
| Rock / Indie / Metal | 11.4% | 11.3% |
| Downtempo / Dub | 10.5% | 10.4% |
| Field / World | 8.8% | 8.7% |
| Trance / Psy | 7.0% | 7.0% |
| Hip-Hop / Beats | 7.0% | 7.0% |
| Breakbeat / Bass | 6.1% | 6.1% |
| Dance / Club | 5.3% | 5.2% |
| Jazz | 3.5% | 3.5% |
| Classical | 1.8% | 2.6% |
| Reggae / Dancehall | 0.9% | 0.9% |

~81% of listenable albums match >=1 pill. Cross-collection genre overlap is ~0
(classical vs electronic), and musopen is barely indexed.

## Design Decisions (confirmed with owner)
1. Coverage-gated dynamic pill set (~14 pills; surface only those with enough music).
2. Prioritize musopen indexing so classical/global pills can populate.
3. Pills stored in a DB table (`pills`), seeded from embedded JSON, tunable in-place.
4. `min_library_count` default = 10 listenable albums.

## Workflow
Research -> Plan -> Implement (4 phases) -> Verify -> Refine.
