# Architecture

## Components
- `pills` table (new): catalog of selectable genre pills, tunable in DB.
- `pills.json` (new, embedded via go:embed): seed data, upserted INSERT OR IGNORE.
- `internal/db/pills.go` (new): seed, coverage counting, active-pill listing.
- `cmd/tui` flags `--pills` / `--pill <id>`: list active pills / render a pill feed
  by embedding the pill prompt with CLAP and calling existing `db.SearchByText`.
- Resolution prioritization: bias `ClaimUnresolvedAlbum*` toward `musopen-free`.

## Layering (no new import cycles)
- `internal/db` stays free of `internal/clap`. CLAP text-embedding orchestration
  lives in `cmd/tui` (same pattern as `runTextSearch`).
- Pill -> feed flow:
  `cmd/tui` reads pill (db) -> `clap.GetTextEmbedding(prompt)` ->
  `db.SearchByText(vec, keywords, limit)`.

## Coverage gating
A pill is "active" when `enabled=1 AND CountPillCoverage(keywords) >= min_library_count`.
Coverage = COUNT(DISTINCT album) over the *listenable* pool (completed + embedded,
not excluded) whose `subjects` or track `tags` match any keyword (LIKE).
This auto-reveals Classical/World as musopen indexing progresses.
