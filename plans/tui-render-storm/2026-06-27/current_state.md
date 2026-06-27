# Current State — 2026-06-27

## Completed Phases

### Phase 1 — Non-blocking event emission (analysis never stalls on UI)
- `internal/tui/events.go`: `Emit(ch, ev)` non-blocking, drop-newest; `StartEventDecoupler(in)` 
  goroutine that forwards with `Emit`, decoupling producer channel from TUI display channel;
  buffer bumped from 100→256.
- `cmd/tui/main.go`: decoupler wired in `runTUI` — workers send to producer channel, TUI reads
  display channel.
- `internal/db/listenability.go`: `RecoverStaleListenabilityLocks(db, olderThan)` clears
  leaked locks older than the threshold.
- `cmd/tui/main.go`: recovery sweep called in both `runTUI` and `runHeadless` startup paths
  (10-minute stale threshold).

### Phase 2 — Batch-drain events (one render per burst)
- `internal/tui/model.go`: `eventBatchMsg`, `drainActivityEvents(ch)` coalesces queued events
  into a single message. `case eventBatchMsg:` handler loops over events applying to
  Dashboard + LiveLog, then re-subscribes. Removed the old 1:1 `ActivityEvent` handler.
  `Init` subscribes via `drainActivityEvents`.

### Phase 3 — Throttled redraw tick
- Deferred per Decision 2. Not implemented.

### Phase 4 — Cleaner event throttle
- `cmd/tui/main.go`: cleaner `EventCleanerBatch` emission now accumulates `pendingScored`
  and flushes at most every 1 second. Flush also called when the backlog drains
  (`len(claims) == 0`), so the final count appears.

### Phase 5 — Revert misdirected sidecar regression
- `internal/clap/sidecar.go`: `EnsureSidecar` restored to "reuse-if-healthy" — when
  `NewGRPCClient` succeeds (real health probe with 5s timeout), returns the existing client.
  Only restarts when no healthy sidecar responds. `killExistingSidecar` rewritten to
  parse multiple `lsof` PIDs line-by-line and skip `os.Getpid()`.
- `cmd/tui/main.go`: CLAP per-track timeout reverted 120s → 30s (line ~1364).

### Tests
- `internal/db/listenability_test.go`: `TestRecoverStaleListenabilityLocks` (stale vs fresh
  locks, idempotent).
- `internal/tui/events_test.go`: `TestEmitDropsNewestWhenFull`, `TestEmitNeverBlocks`,
  `TestStartEventDecouplerForwards`.
- `internal/tui/model_test.go`: `TestDrainActivityEventsCoalesces`,
  `TestDrainActivityEventsRespectsMax`, `TestDrainActivityEventsClosedChannel`.

## Verification
- `make build` — binary at `bin/timbre`, 25MB, builds clean.
- `go vet ./...` — no warnings.
- `go test -race -count=1 ./...` — all 12 packages pass (1 existing test fixed to use new
  sidecar reuse behavior).

## Decisions Resolved
All 6 decisions in `decisions.md` resolved (see file). Key calls:
- D1: drop-newest (concurrency-safe with multi-producer channel).
- D2: defer Phase 3.
- D3: re-score all 40k backlog (required for correctness; 53%→0.8% exclude shift), with
  cleaner event throttle.
- D4: reuse-if-healthy sidecar.
- D5: 30s CLAP timeout.
- D6: 256 buffer.

## Open Items
None. All planned phases complete or explicitly deferred.

## Remaining Work
None — this is the full implementation per the plan. The user should relaunch timbre
to test the fix live (current stuck process PID 8679 will need exit/restart to pick up
the new binary).
