# Testing Strategy

## Principles

Each phase must be verifiable independently. Prefer fast, deterministic unit tests for the
new helpers; use a measurable reproduction for the render-rate behavior.

## Unit Tests

### Phase 1

- `TestEmit_DropsOldestWhenFull` (`internal/tui/events_test.go`)
  - Make a `chan ActivityEvent` cap 2; `Emit` three events; assert no goroutine blocks and
    the channel holds the two newest.
- `TestEmit_NeverBlocks`
  - Spawn `Emit` in the test goroutine with a full unread channel; assert it returns
    (use a deadline) — proves producers can't stall.
- `TestRecoverStaleListenabilityLocks` (`internal/db/listenability_test.go`)
  - Open a temp SQLite DB with the tracks schema; insert rows with
    `listenability_locked_at` set to (a) 30 min ago, (b) 1 min ago, (c) NULL.
  - Call `RecoverStaleListenabilityLocks`; assert only (a) is cleared and the returned
    count == 1.

### Phase 2

- `TestDrainActivityEvents_Coalesces` (`internal/tui/model_test.go` or a focused test)
  - Pre-load a channel with 50 events; the drain Cmd returns a single `eventBatchMsg`
    containing all 50 (up to the batch cap).
  - With an empty channel, the drain blocks until one event arrives, then returns a batch
    of 1.

## Behavioral / Performance Verification

### Render-rate harness (Phase 2/3)

Add a counter (test-only or build-tagged) incremented in `DashboardModel.View`. Drive a
synthetic flood: push K events as fast as possible into the channel, run the model loop for
T seconds, assert `renderCount` is bounded (e.g. `≤ ~10*T` with throttling, or
`≪ K` with batch-drain) rather than `≈ K`.

### End-to-end CPU/stall check (manual, on the real DB)

This is the reproduction that originally exposed the bug.

1. `make build`.
2. Launch the TUI; start resolvers + analyzers + cleaners so the 40k backlog churns.
3. While running, sample without killing the session:
   `sample <pid> 3 -file /tmp/s.txt` and confirm `MainModel.View`/`RenderActivityFeed`
   no longer dominate, and CPU settles to a level proportional to real work.
4. Confirm via `data/debug.log` that analyzer `DOWNLOADING`/`QUALITY`/`COMPLETE` lines
   appear (tracks are actually being analyzed) — the original symptom is gone.
5. `sqlite3 data/parso_indexer.db` — confirm the leaked-lock count
   (`listenability_locked_at IS NOT NULL`) is 0 after the recovery sweep.

> Do not use `kill -3` on the user's interactive session to get a goroutine dump; prefer
> `sample` (non-destructive). The binary does not trap SIGQUIT, so `kill -3` would crash
> the session.

## Regression Suite

- `make build && go vet ./... && go test -race -count=1 ./...` must pass after every phase.
- Existing `internal/clap/sidecar_test.go` must still pass after Phase 5 changes.

## Acceptance Criteria

- Workers never block on `events <- ...` (Phase 1).
- Under a sustained event flood, render invocations are bounded, not 1:1 with events
  (Phase 2/3).
- Analysis proceeds (new `COMPLETE` lines) while the cleaner backlog is active.
- Leaked listenability locks are recovered to 0 on startup.
- Full build/vet/test green.
