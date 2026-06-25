# Analyzer Claim Ordering — Most Recently Added Tracks First

## Problem

The analyzer was expected to index the most recently added tracks first, but in
practice it does not. Newly added tracks are not consistently picked up before
older ones.

## Current Behavior

The analyzer worker loop (`cmd/tui/main.go:777`) claims tracks via
`db.ClaimNextTrackBatch` (`internal/db/queue.go:525`). The claim query ordered by:

```sql
WHERE t.status = 'pending' AND a.prechecked = 1
ORDER BY t.created_at DESC, t.album_id, t.track_number
LIMIT ?
```

Two compounding defects defeat the intended "newest first" ordering:

1. **Second-only timestamp resolution.** `created_at TEXT NOT NULL DEFAULT
   (datetime('now'))` (`internal/db/db.go:115`) yields `YYYY-MM-DD HH:MM:SS` with
   no sub-second precision. `InsertTracks` (`internal/db/queue.go:494`) writes a
   whole album's tracks in one tight transaction and never sets `created_at`, so
   every track in an album — and every album resolved within the same wall-clock
   second — collapses to one identical timestamp string.

2. **Tie-break discards recency.** When `created_at` ties (almost always, per #1)
   ordering falls through to `t.album_id` (alphabetical) then `t.track_number`.
   The result is effectively alphabetical-by-album, not newest-first. The one
   perfectly monotonic insertion-order signal — `id INTEGER PRIMARY KEY
   AUTOINCREMENT` — was never used.

The unit test `TestClaimNextTrackBatchOrdering` passed only because it manually
staged `created_at` values 30/60/90 seconds apart, which never happens in
production bulk inserts.

## Research Findings

- `id` is `INTEGER PRIMARY KEY AUTOINCREMENT`, strictly monotonic in insertion
  order; highest id = most recently inserted track. It has no granularity issue.
- `ClaimNextTrackBatch` has one production caller (`cmd/tui/main.go:777`) and
  ~13 test call sites. Only `db_test.go:219` asserts a specific multi-track order
  that changes under the new ordering; all other multi-track claim tests are
  count-based and order-independent.

## Design Proposal

Order the claim query purely by track recency:

```sql
ORDER BY t.id DESC
```

Chosen over album-grouped or timestamp-tiebreak alternatives by user decision:
"Pure track recency (id DESC)".

## System Changes

- `internal/db/queue.go` — `ClaimNextTrackBatch` ORDER BY changed to `t.id DESC`.
- `internal/db/db_test.go` — update the single order-dependent assertion and
  strengthen `TestClaimNextTrackBatchOrdering` to prove `id` ordering dominates
  `created_at`.

## Implementation Steps

1. Change ORDER BY in `ClaimNextTrackBatch`.
2. Update `db_test.go:219` expectation (Song One/Two -> Song Three/Two).
3. Rewrite `TestClaimNextTrackBatchOrdering` so insertion order (id) contradicts
   `created_at`, asserting id order wins.

## Testing Strategy

- `make build` (produces `bin/timbre`).
- `go vet ./...`.
- `go test -race -count=1 ./...` — all packages pass.

## Accepted Tradeoffs

- Tracks from different albums interleave within/across claim batches.
- Within one album, tracks are claimed in reverse-insertion order rather than
  `track_number` order.

## Out of Scope

- Precheck gating (`a.prechecked = 1`) can still delay a very recent album whose
  precheck has not run; the precheck queue already favors newest albums.
- Timestamp format inconsistency between Go-written `updated_at` (RFC3339) and the
  `datetime('now')` column default; irrelevant to id-based ordering.

## Open Questions

None.
