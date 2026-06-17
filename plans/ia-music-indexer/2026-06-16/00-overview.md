# 00 — Project Overview (Revision 3 — Three-Tier Pipeline, Per-Track)

## Problem

The Internet Archive hosts ~14–15 million audio recordings, roughly 3–5 million of which are music. However:
- IA's interface is clunky and has no recommendation/discovery engine
- Metadata is highly inconsistent across collections
- Audio quality varies dramatically (clean modern recordings vs. hissy 78rpm transfers)
- Standard collaborative filtering fails due to "cold start"—most tracks have zero listening history
- Users of the Parso Radio iOS app can search and play music, but cannot discover new content

## Goal

Build a **self-contained, TUI-driven audio indexing pipeline** that:
1. Discovers music **albums** (IA items) from Internet Archive collections
2. Resolves each album into individual **tracks** (MP3 files), filtering by quality (VBR or ≥192kbps CBR)
3. Streams only the first ~30 seconds of each track into memory
4. Extracts a hybrid audio embedding (564-dim vector) via late fusion of three complementary features:
   - MFCC (40-dim) — acoustic texture and timbre via `go-mfcc`
   - Chroma (12-dim) — harmonic profile via FFT frequency→pitch mapping
   - CLAP (512-dim) — deep semantic understanding (mood, genre, instruments) via `laion/clap-htsat-fused` in a Python sidecar over gRPC
5. Computes a multi-metric quality score (SNR + Spectral Centroid + Crest Factor) per track
6. Stores per-track embeddings in a local SQLite database (BLOB storage, pure Go cosine similarity)
7. Provides a rich terminal UI (Bubble Tea) with album art, album/track browsing, and playback
8. Uses a three-tier pipeline: Coordinator (discovery) → Resolvers (album→tracks) → Analyzers (track→embedding)
9. Supports headless mode (`--headless`) for CI/e2e testing and background operation
10. Runs entirely locally on a developer laptop — zero external services, zero shell scripts

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
| Per-track raw row size | ~2344 bytes |
| Per-track with overhead | ~2500 bytes |
| Total DB size (4M tracks) | ~4–9 GB |
| Per-track analysis time | ~30 seconds streaming + ~200ms MFCC/Chroma + ~200–500ms CLAP |
| Per-track bandwidth | ~1.6 MB (30s @ 450kbps MP3) |
| Coordinated discovery time | ~hours (1 server, metadata only) |
| Time on single IP @ 450kbps | ~2.3 years (sequential); ~56 days (50 concurrent) |
| Time on 10 distributed IPs | ~5–6 days |

## Phased Approach

### Phase 1–6 — Core Pipeline (COMPLETE)
- Single Go binary with Bubble Tea TUI
- SQLite with BLOB vector storage, pure Go cosine similarity
- Coordinator + worker pool as goroutines
- 4-tab interface: Dashboard, Live Log, Browse/Search, Player

### Phase 2B — Hybrid Recommendation Engine (COMPLETE)
- MFCC (40-dim), Chroma (12-dim), CLAP gRPC client (512-dim)
- Late fusion engine (564-dim weighted concatenation)
- Multi-metric quality scoring (SNR + Centroid + Crest Factor)

### Phase PX — Per-Track Model + Three-Tier Pipeline (COMPLETE)
- Albums → Tracks → Embeddings data model (replaces per-item catalog_queue)
- Three-tier pipeline: Coordinator (singleton) → Album Resolvers (pool) → Track Analyzers (pool)
- Album art rendering via rasterm (iTerm2/Kitty/Sixel)
- Browse tab redesign: Albums view, Album detail, Tracks view
- MP3 3-tier filtering: VBR accept, CBR ≥192, blacklist 64/128

### Phase 7 — Player Tab (PENDING)
- Audio playback via `gopxl/beep/v2`
- Play/pause/stop/seek/volume controls, play queue

### Phase 8+ — Distributed Workers (FUTURE)
- Central Turso/libSQL database
- Multiple VPS/edge nodes running workers

### Phase 9+ — API / Recommendation Server (FUTURE)
- REST API for querying nearest-neighbor tracks by vector
- Integration with Parso Radio iOS app
- User preference vector learning from telemetry

## Files in This Plan

| File | Purpose |
|---|---|
| `00-overview.md` | This document — problem, goals, scope |
| `02b-hybrid-engine.md` | Phase 2B plan — hybrid recommendation engine core (MFCC + Chroma + CLAP + Fusion) |
| `architecture.md` | System architecture, component design, data flow, event system |
| `data-model.md` | SQLite schema, queue state machine, hybrid vector format |
| `implementation.md` | Phased implementation steps, file/module layout, exit criteria per phase |
| `testing.md` | Testing strategy — unit, integration, TUI component, e2e (headless) |
| `decisions.md` | 18 key architectural decisions with rationale |
| `current_state.md` | Current progress, completed/pending phases |
