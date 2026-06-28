# Decisions

## D1 — Collection shape
**Two collections, license-filtered** (`musopen-free`, `netlabels-free`), rather than a
single combined "Free Music" collection. Each is scoped to its IA collection.

## D2 — BY-ND handling
**Drop BY-ND.** The app's commercially-usable definition is pd / cc0 / cc-by / cc-by-sa
(`ia.IsCommerciallyUsable`). BY-ND is excluded, keeping the indexed set aligned with the
"% Free" metric.

## D3 — PD Mark
**Include PD Mark** as free. `ia.ClassifyLicense` already maps `publicdomain/mark` → `pd`,
which `IsCommerciallyUsable` treats as free. No code change required.

## D4 — Playlist import feature
**Keep the import UI** (`internal/tui/collections.go`, `internal/playlist`). Only the seed
was changed. Note: the free-only gate is global, so manually-imported albums are also
filtered to free licenses while `--free-only` is on.

## D5 — License filtering mechanism (revised after live validation)
The original plan filtered licenses inside the IA query via `licenseurl:*...*` wildcards.
**Live testing proved IA's Elasticsearch backend rejects all wildcard/token `licenseurl`
queries** (`[BACKEND_ERROR]`), so this is impossible. Chosen alternative: **in-app
filtering** — the album resolver classifies the license up front and marks non-free albums
`unavailable`, so the two collections become effectively free-only.

## D6 — Unknown licenses
Albums with **no/ambiguous license** (`unknown`/`other`) are treated as **not free** and
excluded. Conservative and correct for a "Free Music" indexer.

## D7 — Reversibility
Gated behind `--free-only` (default `true`, env `FREE_ONLY`). Set `--free-only=false` to
restore prior behavior (index everything, classify-only).

## D8 — Stale DBs
Ignored per direction. `SeedCollectionsIfEmpty` no-ops on non-empty DBs, so anyone with an
existing DB keeps their old collections until they reseed; the fresh-DB path seeds the two
new collections.
