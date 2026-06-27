# Decisions — Resolved

Resolved 2026-06-27. Treat each as a constraint.

## Decision 1 — Event overflow policy when the buffer is full

**Resolution: non-blocking send, drop-NEWEST (revised from the recommended drop-oldest).**

Rationale: producer-side "drop-oldest" requires receiving from the shared multi-producer
events channel to evict an element, which races with the single TUI consumer and can steal
legitimate events. Drop-newest (`select { case ch <- ev: default: }`) is concurrency-safe,
never blocks producers, and — combined with the 256 buffer and batch-drain consumer — drops
only under genuine sustained overload. Implemented as `tui.Emit` and used inside
`tui.StartEventDecoupler`.

## Decision 2 — Is the throttled-redraw tick (Phase 3) needed?

**Resolution: (A) Phase 2 (batch-drain) only; Phase 3 deferred.**

Batch-drain coalesces a whole burst into one render, and the decoupler keeps producers
unblocked. A fixed-rate redraw tick was deemed unnecessary complexity for now; revisit only
if measurements show renders still dominate CPU under load.

## Decision 3 — The 40,219-track listenability re-score backlog

**Resolution: (A) re-score all + (C) throttle the cleaner's event emission.**

Evidence: decision split by version showed v1/NULL tracks are ~53% `exclude`, but after v2
re-scoring only ~0.8% `exclude` (98.8% become `include`). Re-scoring is required for catalog
correctness — option (B) "stamp current without re-scoring" would freeze ~20k tracks as
wrongly excluded and was rejected. The cost is mitigated by the render-storm fix
(Phases 1–2) plus a 1s time-throttle on the cleaner's `EventCleanerBatch` emission
(accumulating `Count` so the dashboard total stays accurate).

## Decision 4 — Sidecar restart behavior (from commit `4e013337`)

**Resolution: (A) reuse a healthy running sidecar; restart only if unhealthy/missing.**

`NewGRPCClient` performs a real health probe (a `GetEmbedding` call with a 5s timeout), so a
successful connect means the sidecar is genuinely responsive. The always-restart was a
misdiagnosis (the slowness was the render storm). `killExistingSidecar` is retained only for
the unhealthy/missing path and was fixed to (a) parse multiple `lsof` PIDs line-by-line and
(b) skip our own PID.

## Decision 5 — CLAP per-track timeout

**Resolution: (A) revert 120s → 30s.**

A single forward pass over a ~30s clip is sub-second to a few seconds; 120s only prolonged
stalls on genuinely hung requests. 30s remains generous.

## Decision 6 — Event channel buffer size

**Resolution: (A) 256** (`eventChannelBuffer`), applied to both the producer channel and the
decoupled display channel.
