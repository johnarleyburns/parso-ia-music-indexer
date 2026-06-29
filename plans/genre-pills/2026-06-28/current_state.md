# Current State

## Phases
- Phase 0 (planning docs): COMPLETE
- Phase 1 (pills table + seed): COMPLETE
- Phase 2 (coverage gating): COMPLETE
- Phase 3 (pill -> feed wiring): COMPLETE
- Phase 4 (musopen resolution priority): COMPLETE
- Verify (build + vet + race tests): COMPLETE — all packages pass

## Delivered
- `pills` table added in `internal/db/db.go` migrate().
- `internal/db/pills.json` (14 pills) + `internal/db/pills.go`:
  `SeedPills`, `SeedPillsIfEmpty`, `GetPillCount`, `BulkInsertPills`, `GetPillByID`,
  `ListAllPills`, `CountPillCoverage`, `ListActivePills`.
- Seeding wired into runTUI, runHeadless, and `--seed-collections`.
- CLI: `--pills` (list active pills) and `--pill <id>` (render similar-music feed)
  in `cmd/tui/main.go` (`runListPills`, `runPillFeed`); flags in `internal/config`.
- Resolution priority toward `musopen-free` in `ClaimUnresolvedAlbum` /
  `ClaimUnresolvedAlbumBatch` (`internal/db/queue.go`).
- Tests: `internal/db/pills_test.go`, `internal/db/queue_priority_test.go`.

## Plan vs implementation notes
- implementation.md referenced `FeedForPill`; implemented as `cmd/tui.runPillFeed`
  to keep the `db` package free of a `clap` dependency (per architecture.md). The
  pill->feed flow (read pill -> CLAP text embedding -> SearchByText) is unchanged.

## Live verification (real DB, 115 listenable albums)
`--pills` surfaced 9 active pills (>=10 coverage): experimental-noise (44),
idm-electronica (44), ambient-drone (37), techno-house (29), rock-indie-metal (15),
downtempo-dub (13), trance-psy (13), field-world (10), hiphop-beats (10).
Sub-threshold pills (breakbeat-bass, dance-club, jazz, classical, reggae-dancehall)
are correctly hidden and will appear automatically as coverage grows (Classical via
the Phase 4 musopen resolution bias).

## Phase 5 — TUI Pills tab (follow-up request)
COMPLETE.
- New "Pills" tab (`internal/tui/pills.go`, wired in `internal/tui/model.go` as
  tab index 5): lists every pill with its live listenable-album count and an
  active (✓) flag.
- `[enter]` on a pill drills into its matching tracks (lexical keyword match via
  `db.TracksForPill`, best-first by listenability/quality); `[enter]` on a track
  emits `SwitchToPlayerMsg` to play it; `[esc]` returns to the pill list.
- `[r]` recalculates coverage counts (`db.ListPillsWithCoverage`); the tab also
  refreshes on switch-in.
- New DB funcs: `ListPillsWithCoverage`, `TracksForPill` (+ shared
  `pillKeywordClause`). Tests: `TestListPillsWithCoverage`, `TestTracksForPill`.
- Note: the TUI tab uses the deterministic lexical match (no sidecar needed); the
  CLAP+lexical blended feed remains available via the `--pill <id>` CLI.

## Blockers
None.
