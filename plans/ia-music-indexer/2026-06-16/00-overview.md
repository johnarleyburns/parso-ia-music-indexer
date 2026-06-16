# 00 — Project Overview (Revision 2 — TUI-Based)

## Problem

The Internet Archive hosts ~14–15 million audio recordings, roughly 3–5 million of which are music. However:
- IA's interface is clunky and has no recommendation/discovery engine
- Metadata is highly inconsistent across collections
- Audio quality varies dramatically (clean modern recordings vs. hissy 78rpm transfers)
- Standard collaborative filtering fails due to "cold start"—most tracks have zero listening history
- Users of the Parso Radio iOS app can search and play music, but cannot discover new content

## Goal

Build a **self-contained, TUI-driven audio indexing pipeline** that:
1. Discovers music tracks from Internet Archive collections
2. Streams only the first ~30 seconds of each MP3 derivative into memory
3. Extracts MFCC-based audio embeddings (40-dim vectors) and a quality score (SNR)
4. Stores embeddings in a local SQLite database with `sqlite-vec` for vector search
5. Provides a rich terminal UI (Bubble Tea) for live progress monitoring, worker control, search, and playback
6. Supports headless mode (`--headless`) for CI/e2e testing and background server operation
7. Runs entirely locally on a developer laptop — zero external services, zero shell scripts

## Target Collections

Using IA's cursor-based Scraping API, sorted by `downloads desc`:
- `collection:etree` — Live Music Archive (280k+ concerts)
- `collection:georgeblood` — 78 RPMs and Cylinder Recordings (400k+ vintage tracks)
- `collection:netlabels` — Independent electronic/indie labels
- `collection:audio_music` — General music drops

Query: `(collection:etree OR collection:georgeblood OR collection:netlabels) AND mediatype:audio`

Excludes: LibriVox, podcasts, spoken word.

## Key Metrics (Estimated)

| Metric | Value |
|---|---|
| Target tracks to index | ~4,000,000 |
| Per-track raw row size | ~198 bytes |
| Per-track with overhead | ~250 bytes |
| Total DB size (4M tracks) | ~1 GB |
| Per-track analysis time | ~30 seconds streaming |
| Per-track bandwidth | ~1.6 MB (30s @ 450kbps MP3) |
| Coordinated discovery time | ~hours (1 server, metadata only) |
| Time on single IP @ 450kbps | ~2.3 years (sequential); ~56 days (50 concurrent) |
| Time on 10 distributed IPs | ~5–6 days |

## Phased Approach

### Phase 1–7 — Local Single-Binary TUI (ALL PHASES)
- Single Go binary (`cmd/tui/main.go`)
- SQLite + sqlite-vec locally
- Coordinator + worker pool as goroutines managed by TUI
- 4-tab interface: Dashboard, Live Log, Browse/Search, Player
- Headless mode (`--headless`) for automated testing
- Process tracks sorted by download count for "maximum impact"
- Pause after each phase for manual TUI verification

### Phase 8+ — Distributed Workers (FUTURE)
- Central Turso/libSQL database
- Multiple VPS/edge nodes running workers
- Deployed via colima/incus containers (reference: `../fast-internet-portal`)

### Phase 9+ — API / Recommendation Server (FUTURE)
- REST API for querying nearest-neighbor tracks by vector
- Integration with Parso Radio iOS app
- User preference vector learning from telemetry

## Files in This Plan

| File | Purpose |
|---|---|
| `00-overview.md` | This document — problem, goals, scope |
| `architecture.md` | System architecture, component design, data flow, event system |
| `data-model.md` | SQLite schema, queue state machine, vector format |
| `implementation.md` | Phased implementation steps, file/module layout, exit criteria per phase |
| `testing.md` | Testing strategy — unit, integration, TUI component, e2e (headless) |
| `decisions.md` | 17 key architectural decisions with rationale |
| `current_state.md` | Current progress, completed/pending phases |
