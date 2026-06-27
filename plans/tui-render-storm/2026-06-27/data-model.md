# Data Model — Listenability Version Backlog & Leaked Locks

## Problem

Two DB-state issues feed and aggravate the render storm:

1. A 40,219-track listenability cleaner backlog generates a large, sustained event flood.
2. 44 tracks have leaked `listenability_locked_at` locks and will never be re-claimed.

## Current Behavior

### Cleaner claim query (`internal/db/listenability.go:159`)

```sql
SELECT ... FROM tracks t
INNER JOIN albums a ON t.album_id = a.ia_identifier
LEFT JOIN track_embeddings e ON t.id = e.track_id
WHERE t.status = 'completed'
  AND (t.listenability_version IS NULL OR t.listenability_version != ?)  -- ? = 'listenability-v2'
  AND (t.listenability_locked_at IS NULL)
ORDER BY t.id ASC
LIMIT ?
```

Per claimed track the cleaner runs `GetAlbumListenabilityEvidence` (3+ queries incl. a
multi-`LIKE` scan), `DecodeF16`, `computePromptEvidence`, and `ScoreTrack`, then
`UpdateTrackListenability` (sets `listenability_version = r.Version` and
`listenability_locked_at = NULL`).

### Why the backlog exists

`MigrateListenabilityV1ToV2` (`internal/db/listenability.go:359`) sets
`listenability_version = NULL` for v1 completed tracks **and** flips a large population of
`status='unavailable'` listenability-excluded tracks back to `status='completed'` with
`listenability_version = NULL`. Net result (measured): 40,263 completed tracks with NULL
version vs. 720 at v2.

### Why 44 locks leaked

`ClaimListenabilityCleanupBatch` locks rows by setting `listenability_locked_at = now`
inside a committed tx. The lock is cleared only by `UpdateTrackListenability` (on success)
or `ReleaseListenabilityCleanupClaim`. When the process is SIGKILL'd mid-batch (which has
happened repeatedly while chasing this bug), the lock is never cleared. The claim query
excludes `listenability_locked_at IS NOT NULL`, so those 44 rows are now orphaned —
permanently skipped.

## Research Findings

- `ScoreTrack` always sets `r.Version = Version` ("listenability-v2") via its single
  return path (`internal/listenability/listenability.go:93-130`). So the loop **does**
  converge — the backlog drains; it is not an infinite loop. It is simply 40k units of
  real work whose **event emission** drives the storm.
- The backlog is legitimate re-scoring work. The problem is (a) it floods events and
  (b) there is no stale-lock recovery.

## Design Proposal

1. **Stale-lock recovery sweep (Phase 1).** At startup (and/or on a timer), clear locks
   older than a threshold:

   ```sql
   UPDATE tracks
   SET listenability_locked_at = NULL, listenability_worker_id = NULL
   WHERE listenability_locked_at IS NOT NULL
     AND listenability_locked_at < datetime('now', '-10 minutes');
   ```

   Add `RecoverStaleListenabilityLocks(db) (int64, error)` next to the existing claim
   helpers; call it from both `runHeadless` and the TUI startup path.

2. **Cleaner event throttling (Phase 4).** The cleaner already emits one
   `EventCleanerBatch` per batch of 10 (`cmd/tui/main.go:1075`). Confirm it emits no
   per-track events on the hot path and, if event volume is still high, emit a coalesced
   summary at most every ~1 s. This reduces the source rate independent of the TUI fix.

3. **Migration backlog (decision required).** Decide whether the 40k re-score is desired
   work or whether those rows should be stamped to current version without re-scoring (if
   the v2 algorithm would not change their decision). See `decisions.md`.

## System Changes

- `internal/db/listenability.go` — add `RecoverStaleListenabilityLocks`.
- `cmd/tui/main.go` (`runHeadless`) and TUI startup — call the recovery sweep.
- (Phase 4) cleaner emit sites — coalesce/throttle if needed.

## Testing Strategy

- Unit test `RecoverStaleListenabilityLocks` against a temp SQLite DB: seed rows with old
  and recent `listenability_locked_at`, assert only stale rows are cleared.
- Manual: after the sweep, re-run the backlog-count query and confirm the 44 stuck rows
  become claimable again.

## Open Questions

See `decisions.md`.
