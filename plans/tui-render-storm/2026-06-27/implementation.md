# Implementation — Phased Plan

Each phase is independently testable, reversible, and leaves the system functional.
Follow `Research → Plan → Implement → Verify` per phase. One branch per phase.

## Phase 1 — Stop the stall (highest priority)

**Goal:** analysis never blocks on the TUI, and leaked locks are recovered.

1. `internal/tui/events.go`
   - Add `func Emit(ch chan ActivityEvent, ev ActivityEvent)` (non-blocking, drop-oldest —
     see `architecture.md`).
   - Optionally raise buffer in `NewEventChannel` from 100 → 256.
2. `cmd/tui/main.go`
   - Replace every blocking `events <- tui.ActivityEvent{...}` with
     `tui.Emit(events, tui.ActivityEvent{...})` in all worker loops
     (analyzer, resolver, enhancer, cleaner, coordinator).
3. `internal/db/listenability.go`
   - Add `RecoverStaleListenabilityLocks(db *sql.DB) (int64, error)`.
4. `cmd/tui/main.go`
   - Call the recovery sweep in `runHeadless` (near the migration call) and in the TUI
     startup path; log the count.

**Verify:** with a synthetic high event rate, workers do not block; analysis progresses.
Unit tests for `Emit` (full-buffer drop) and `RecoverStaleListenabilityLocks`.

## Phase 2 — Kill the render storm

**Goal:** one render per burst instead of one render per event.

1. `internal/tui/model.go`
   - Add `type eventBatchMsg struct{ Events []ActivityEvent }`.
   - Add `drainActivityEvents(ch)`: block for the first event, then non-blocking-drain all
     currently-queued events (cap the batch, e.g. 512) into `eventBatchMsg`.
   - In `Init` and after handling, subscribe via `drainActivityEvents` instead of
     `waitForActivityEvent`.
   - Add `case eventBatchMsg:` that applies all events to `m.Dashboard`/`m.LiveLog` and
     returns `tea.Batch(drainActivityEvents(m.Events), cmd1, cmd2)`.
2. `internal/tui/dashboard.go`, `internal/tui/livelog.go`
   - Either accept a batch (`UpdateBatch([]ActivityEvent)`) or have the model handler loop
     and call the existing per-event `Update`. Render still happens once after the handler.

**Verify:** with the same synthetic flood, render invocations drop from ~N/event to
~1/burst; CPU drops proportionally. Keep `waitForActivityEvent` only if still referenced.

## Phase 3 — Throttled redraw (defense in depth, optional)

**Goal:** bound renders to ~10 Hz even under pathological sustained bursts.

1. `internal/tui/model.go`
   - Add a `redrawTickMsg` on a ~100 ms `tea.Tick`; `eventBatchMsg` updates state only and
     does not itself force extra renders beyond bubbletea's post-Update render.
   - If batch-drain (Phase 2) already holds CPU acceptably under load (per `testing.md`),
     **skip this phase** and record the decision.

## Phase 4 — Reduce the event source

**Goal:** lower the cleaner's contribution to the flood.

1. Audit cleaner emit sites (`cmd/tui/main.go:~1062-1080`); ensure no per-track event on
   the hot path. The per-batch `EventCleanerBatch` stays.
2. If volume is still high, coalesce cleaner progress into a summary emitted at most every
   ~1 s.

## Phase 5 — Revert the misdirected sidecar regression (separate concern)

**Goal:** undo changes from `4e013337` that degraded startup/per-track latency and were
based on the wrong diagnosis.

1. `internal/clap/sidecar.go`
   - Restore "reuse a healthy running sidecar" behavior; only restart if missing/unhealthy.
   - If kill-on-restart is kept, fix `killExistingSidecar`: `lsof -ti :PORT` can return
     multiple PIDs (the Go gRPC client also holds the port) → `strconv.Atoi` fails and the
     kill is silently skipped. Parse line-by-line and skip our own PID.
2. `cmd/tui/main.go`
   - Revert the CLAP per-track timeout `120s → 30s` (or a value justified by measured
     inference time), so a slow CLAP fails fast instead of stalling a worker for 2 min.

## Sequencing & Branches

```
feature/tui-render-storm-phase-1   (stall fix + lock recovery)   ← ship first
feature/tui-render-storm-phase-2   (batch drain)
feature/tui-render-storm-phase-3   (redraw throttle, optional)
feature/tui-render-storm-phase-4   (cleaner event throttle)
feature/sidecar-restart-revert     (phase 5, independent)
```

Phase 1 alone is expected to restore "can analyze tracks." Phase 2 restores normal CPU.

## Post-Implementation (per AGENTS.md)

After each phase: `make build && go vet ./... && go test -race -count=1 ./...`; reconcile
plan vs. implementation; update `current_state.md`; update README if usage/behavior
changed; commit with a concise message; push / open PR.
