# Status Bar Feature — Overview

## Problem
No visibility into IA API usage rates, bandwidth consumption, completion ETAs, or resource utilization during indexing runs. Users cannot gauge how close they are to IA rate limits or estimate when work will finish.

## Current Behavior
- Dashboard shows album/track counts by status, worker pool sizes, and an activity feed
- No bandwidth or API rate tracking
- No ETA estimates
- No memory or disk usage display

## Design Proposal
Add a persistent 2-line status bar at the bottom of the TUI (above help text), visible across all tabs.

### Status Bar Layout
```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 IA: 45.2 KB/s (4.3%) | 0.25 req/s (15.0%) | Resolvers ETA: 2h 15m | Analyzers ETA: 5h 30m
 Mem: 45 MB | Disk: DB 128 MB  Art 23 MB  Total 151 MB
```

### Metrics Tracked
1. **IA Bandwidth (KB/s)** — bytes received from archive.org over a 60-second sliding window, shown as % of conservative 1 MB/s IA limit
2. **IA API Calls (req/s)** — HTTP requests to archive.org over a 60-second sliding window, shown as % of conservative 100 req/min IA limit
3. **Resolver ETA** — pending albums / aggregate resolver completion rate (wall clock)
4. **Analyzer ETA** — pending tracks / aggregate analyzer completion rate (wall clock)
5. **Memory** — Go runtime allocated memory (heap)
6. **Disk** — SQLite DB file size (incl. WAL/SHM) + art cache directory size

### Conservative IA Limits (for % calculation)
- API rate: 100 req/min (~1.67 req/s) — well below aggressive use that triggers bans
- Bandwidth: 1 MB/s (1,048,576 bytes/s) — conservative sustained limit

## Implementation Phases

### Phase 1: Metrics Collector (`internal/tui/metrics.go`)
Thread-safe sliding window (60s) tracking API calls, bytes transferred, resolver/analyzer completions.

### Phase 2: Instrumented HTTP Transport (in `cmd/tui/main.go`)
Wrap `http.Client.Transport` with a RoundTripper that auto-records API calls and wraps response bodies to count bytes.

### Phase 3: Status Bar Renderer (`internal/tui/statusbar.go`)
Compact 2-line lipgloss-styled bar computing rates, ETAs, and resource stats.

### Phase 4: Model Integration (`internal/tui/model.go`)
Add Metrics pointer, resource stats cache, 2-second refresh tick, render status bar in View().

### Phase 5: Wiring (`cmd/tui/main.go`)
Create Metrics, instrument IA client, pass to TUI model and coordinator, record completions in worker loops.
