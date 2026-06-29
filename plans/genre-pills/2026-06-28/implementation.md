# Implementation Steps

## Phase 1 — pills table + seed
- Add `pills` CREATE TABLE to `migrate()` queries in `internal/db/db.go`.
- Add `internal/db/pills.json` (14 pills + keyword lists + sort_order).
- Add `internal/db/pills.go`: `Pill`, `PillInsert`, `ActivePill` types;
  `SeedPills`, `SeedPillsIfEmpty`, `GetPillCount`, `BulkInsertPills`, `GetPillByID`.
- Wire `SeedPillsIfEmpty` next to `SeedCollectionsIfEmpty` (runTUI, runHeadless);
  `--seed-collections` also seeds pills.

## Phase 2 — coverage gating
- `CountPillCoverage(db, keywords) (int, error)`: DISTINCT listenable albums whose
  subjects/track tags LIKE any keyword.
- `ListActivePills(db) ([]ActivePill, error)`: enabled pills with coverage >=
  min_library_count, sorted by sort_order, carrying live LibraryCount.

## Phase 3 — pill -> feed
- `cmd/tui`: `--pills` lists active pills; `--pill <id>` renders feed:
  read pill -> `clap.GetTextEmbedding(prompt)` -> `db.SearchByText(vec, keywords, 20)`.
- Add `Pills bool` and `PillID string` to config + flags; dispatch in `main()`.

## Phase 4 — prioritize musopen
- Add `priorityCollectionID = "musopen-free"` constant in `internal/db/queue.go`.
- Bias `ClaimUnresolvedAlbum` and `ClaimUnresolvedAlbumBatch` ORDER BY so albums in
  the priority collection resolve first (additive; prior ordering kept as tiebreaker).

## Verify
`make build && go vet ./... && go test -race -count=1 ./...`
