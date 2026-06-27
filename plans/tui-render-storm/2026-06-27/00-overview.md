# TUI Render Storm — Overview

## Problem

`timbre` (the TUI binary) pegs ~1.4 CPU cores and the analyzer "can't analyze a single
track." Previous fixes chased an MP3-decode busy-loop (commits `80a4ad3b`, `19e4fce6`,
`ef0a6afd`) and a stale sidecar (`4e013337`). A live, non-destructive CPU sample of the
actual stuck process proved those were **the wrong layer**.

## Root Cause (evidence-based)

Live `sample` of the stuck process (PID 8679, observed at 121–138% CPU) shows **all**
on-CPU Go work in the TUI render path — and **zero** samples in the analysis pipeline:

```
MainModel.View → DashboardModel.View → DashboardModel.buildRightPanel → RenderActivityFeed
  → charmbracelet/x/ansi.stringWidth + clipperhouse/displaywidth (grapheme width)
  → runtime.mallocgc / GC churn from per-frame string allocation
```

No samples in `ScoreTrack`, `DecodeMP3`, FFT, `workerLoop`, or `analyzeTrack`.

Mechanism:

1. Each `ActivityEvent` re-subscribes via `waitForActivityEvent` and triggers a full
   dashboard re-render (`internal/tui/model.go:289`). Render count == event count.
2. A high event rate (driven largely by a 40k-track listenability cleaner backlog plus
   resolvers/analyzers) makes the TUI re-render continuously, thrashing the allocator/GC
   and pegging ~1.4 cores.
3. The events channel is buffered to only 100 (`internal/tui/events.go:47`). Worker
   goroutines emit events with **blocking** sends (`events <- ...`). When the render storm
   slows the TUI's draining, the buffer fills and workers block on send — so analysis
   stalls. The analyzers are not crashed; they are **starved by backpressure**.

## Data (from `data/parso_indexer.db`)

| Metric | Value |
|---|---|
| Completed tracks | 40,983 |
| `listenability_version IS NULL` (cleaner backlog) | 40,263 |
| `listenability_version = 'listenability-v2'` | 720 |
| Cleaner claim backlog (NULL/!=v2 AND not locked) | 40,219 |
| Tracks stuck with `listenability_locked_at` set (leaked locks) | 44 |
| Pending tracks (analysis queue) | 186,257 |

The v1→v2 listenability migration (`MigrateListenabilityV1ToV2`,
`internal/db/listenability.go:359`) nulled the version on ~40k completed tracks, creating
the backlog that feeds the event flood.

## Goal

Make the TUI's render rate independent of the event rate, guarantee that workers never
block on the events channel, and recover the leaked cleaner locks — so analysis proceeds
regardless of UI load, and CPU usage stays proportional to actual work.

## Non-Goals

- Rewriting the bubbletea model architecture.
- Changing the listenability scoring algorithm.
- Touching the audio decode/quality code (already correctly capped).

## Documents

- `architecture.md` — event→render pipeline, where the storm forms, proposed decoupling.
- `data-model.md` — listenability version/lock backlog and recovery.
- `implementation.md` — phased, incremental implementation.
- `testing.md` — verification strategy.
- `decisions.md` — open questions requiring human judgment.

## Phase Summary

| Phase | Change | Independently shippable | Unsticks analysis |
|---|---|---|---|
| 1 | Non-blocking event emission (drop-oldest) + stale-lock recovery sweep | yes | yes (primary) |
| 2 | Coalesce/batch-drain events in TUI (one render per burst) | yes | n/a (kills CPU storm) |
| 3 | Throttled redraw tick (FPS cap) | yes | n/a (defense in depth) |
| 4 | Cleaner event-emission throttling + backlog handling | yes | n/a (reduces source) |
| 5 | Revert misdirected sidecar-restart + 120s timeout (`4e013337`) | yes | n/a (separate regression) |
