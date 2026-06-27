# Architecture — Event → Render Pipeline

## Problem

The TUI couples three things that should be independent: (a) the rate at which workers
produce activity events, (b) the rate at which the model updates, and (c) the rate at
which the screen is rendered. Under load all three become equal, producing a render storm.

## Current Behavior

Producer side (`cmd/tui/main.go`, all worker loops):

```go
events <- tui.ActivityEvent{ ... }   // BLOCKING send
```

Channel (`internal/tui/events.go:46`):

```go
func NewEventChannel() chan ActivityEvent { return make(chan ActivityEvent, 100) }
```

Consumer side (`internal/tui/model.go`):

```go
func waitForActivityEvent(ch <-chan ActivityEvent) tea.Cmd {
    return func() tea.Msg {
        event, ok := <-ch
        if !ok { return nil }
        return event            // exactly ONE event per Cmd
    }
}

// Update, model.go:285
case ActivityEvent:
    m.Dashboard, cmd1 = m.Dashboard.Update(msg)
    m.LiveLog, cmd2 = m.LiveLog.Update(msg)
    return m, tea.Batch(waitForActivityEvent(m.Events), cmd1, cmd2)
```

bubbletea calls `View()` after each `Update`. So: **1 event → 1 Update → 1 full render.**
`Dashboard.Update` for an `ActivityEvent` is cheap (append + cap to 100, counter bumps:
`internal/tui/dashboard.go:106-260`). The cost is the **render**:
`DashboardModel.View → buildRightPanel → RenderActivityFeed` (`internal/tui/activity.go:119`),
which per frame does `lipgloss ... Width(width).Render(line)` and `lipgloss.Width(line)`
over up to 100 rows — each invoking grapheme-cluster width calculation (the hot
`ansi.stringWidth` / `displaywidth` frames in the sample) and allocating many strings
(GC pressure).

### Failure modes

1. **Render storm:** event rate == render rate. High event rate ⇒ continuous renders ⇒
   CPU pegged + GC churn.
2. **Backpressure stall:** the TUI event loop is single-threaded. While rendering, it is
   not draining `Events`. With only 100 buffer slots and blocking producer sends, the
   buffer fills and workers block on `events <- ...`. Analysis halts.

## Research Findings

- `waitForActivityEvent` correctly handles a closed channel (returns `nil`), so this is
  **not** an infinite-loop-on-closed-channel bug. It is a legitimate-but-excessive
  event/render rate.
- The dashboard already caps its retained slice to 100 events (`dashboard.go:108`), so
  there is no unbounded slice growth. The expense is purely render frequency × per-frame
  render cost.
- Headless mode drains events promptly in a tight `for` loop (`cmd/tui/main.go:1888`),
  so headless does not exhibit the storm — confirming the storm is TUI-render-specific.

## Design Proposal

Decouple the three rates:

```
workers ──(non-blocking emit)──▶ [ring/drop buffer, cap N] ──(batch drain)──▶ model state
                                                                                   │
                                                              (throttled redraw tick @ ~10 Hz)
                                                                                   ▼
                                                                                 View()
```

1. **Non-blocking emit (Phase 1).** Introduce `emit(ch, ev)`:

   ```go
   func emit(ch chan ActivityEvent, ev ActivityEvent) {
       select {
       case ch <- ev:
       default:
           // buffer full: drop oldest, enqueue newest (newest is what the feed shows)
           select { case <-ch: default: }
           select { case ch <- ev: default: }
       }
   }
   ```

   Replace blocking `events <- ...` sites in `cmd/tui/main.go` with `tui.Emit(events, ...)`.
   Guarantees workers never block ⇒ analysis never stalls on UI load.

2. **Batch drain (Phase 2).** Replace `waitForActivityEvent` (one event/Cmd) with a
   drain that blocks for the first event, then non-blocking-drains all currently-queued
   events, returning a single `eventBatchMsg{events []ActivityEvent}`. Update applies the
   whole batch, then re-subscribes. Result: **one render per burst**, not per event.

3. **Throttled redraw (Phase 3, defense in depth).** A fixed-rate `redrawTickMsg`
   (~100 ms) that simply forces a render. Combined with batch drain this bounds renders to
   ~10 Hz even under a sustained flood. (If batch-drain alone proves sufficient in
   testing, Phase 3 may be deferred — see `decisions.md`.)

## System Changes

- `internal/tui/events.go` — add `Emit(ch, ev)`; optionally bump buffer to 256.
- `internal/tui/model.go` — replace `waitForActivityEvent` with batched drain
  (`drainActivityEvents`), handle `eventBatchMsg`, optional `redrawTickMsg`.
- `internal/tui/dashboard.go` / `livelog.go` — accept a batch (loop over events) or keep
  per-event `Update` invoked from the batch handler.
- `cmd/tui/main.go` — swap blocking `events <- ...` for `tui.Emit(events, ...)` at all
  worker emit sites.

## Open Questions

See `decisions.md` (overflow policy, FPS cap, whether Phase 3 is needed).
