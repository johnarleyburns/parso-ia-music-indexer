# Design Decisions (Revision 3)

## Decision 1: Language — Go vs Python

**Decision**: Go

**Rationale**: User familiarity, goroutines for concurrent audio analysis, single statically-linked binary, lower memory footprint, superior for distributed edge-node deployment.

**Tradeoff**: Smaller ML ecosystem. Accepted because MFCC extraction is well-defined and `go-mfcc` exists.

---

## Decision 2: Database — SQLite with BLOB Vectors

**Decision**: SQLite with BLOB-encoded float32 vectors and pure Go cosine similarity. No sqlite-vec extension.

**Rationale**: Zero setup, single file. BLOB storage supports arbitrary-dimension vectors (40-dim, 564-dim, future changes). Pure Go cosine distance avoids CGo dependency for vector search. Sufficient performance for brute-force search up to ~1M vectors.

**Tradeoff**: O(N) similarity search. Acceptable for current scale. ANN indexing can be added later if needed.

---

## Decision 3: API Strategy — Cursor-based Scraping API vs Advanced Search API

**Decision**: IA Scraping API (`/services/search/v1/scrape`) with cursor pagination

**Rationale**: Advanced Search API has hard 10,000 result limit. Scraping API supports unlimited cursor-based traversal and `sorts=downloads desc`.

**Tradeoff**: Less documented. Mitigated by saving cursor state to DB and file.

---

## Decision 4: Audio Analysis — 20 MFCC bands, 30 seconds, 40-dim vector

**Decision**: 20 MFCC bands from first ~30s, mean+variance per band → 40-dim float32

**Rationale**: Industry standard for MIR. 30s captures enough for genre/timbre. Mean+variance captures central tendency and dynamic range. 40 dimensions is compact for fast similarity search.

---

## Decision 5: Quality Score — Multi-Metric Composite

**Decision**: Composite 0.0–1.0 quality score from SNR (0.50), Spectral Centroid (0.30), Crest Factor (0.20) with SNR < 10 dB kill switch.

**Rationale**: See Decision 19 for full rationale. Single SNR is insufficient for the diversity of IA audio sources.

---

## Decision 6: Pipeline Architecture — Three-Tier (Coordinator + Resolvers + Analyzers)

**Decision**: Three-tier pipeline with independently scalable pools:
- Tier 1: Coordinator (singleton) — IA search API scraping → albums
- Tier 2: Album Resolvers (pool) — IA metadata API → tracks
- Tier 3: Track Analyzers (pool) — audio download + ML features → embeddings

**Rationale**: Each tier has different scaling characteristics. The coordinator is inherently sequential (cursor pagination). Resolvers are rate-limited by the metadata API (~15 req/min). Analyzers are CPU/network bound. Separate pools let the user balance resources across pipeline stages and observe backpressure (e.g., lots of pending tracks = add more analyzers).

**Supersedes**: Original Decision 6 (two-pool coordinator/worker model).

---

## Decision 7: Orchestration — TUI (not Shell Scripts)

**Decision**: Single binary with Bubble Tea TUI as primary interface; headless mode for automation.

**Rationale**: User requested "scriptless" design. TUI provides live progress, interactive controls, search, and playback in one tool.

---

## Decision 8: Concurrency Model — Goroutine Pools

**Decision**: Three tiers use goroutine pools with user-controllable concurrency. Coordinator is singleton. Resolver pool (typically 2-3). Analyzer pool (typically 4-8). SQLite writes serialized via mutex.

---

## Decision 9: HTTP Range Header Max Bytes

**Decision**: `Range: bytes=0-1600000` (~1.6 MB) default. Configurable via `MAX_STREAM_BYTES`.

---

## Decision 10: Future Distribution Strategy

**Decision**: Phase 1 = local SQLite; future = Turso/libSQL + container deployment.

**Status**: Not implemented.

---

## Decision 11: Single Binary, Two Modes (TUI + Headless)

**Decision**: One `cmd/tui/main.go` entry point. `--headless` flag switches between TUI and JSON-logging headless mode.

---

## Decision 12: Bubble Tea TUI Framework

**Decision**: `charm.land/bubbletea/v2` with `bubbles/v2` and `lipgloss/v2`.

---

## Decision 13: Coordinator/Workers as Goroutines, Not Subprocesses

**Decision**: All pipeline tiers run as goroutines within the TUI binary, communicating via Go channels.

---

## Decision 14: `gopxl/beep/v2` for Audio Playback

**Decision**: `github.com/gopxl/beep/v2` (active fork of `faiface/beep`).

**Status**: Not yet implemented (Phase 7).

---

## Decision 15: Event Channels (TUI ↔ Goroutines)

**Decision**: Bidirectional Go channels — `chan ActivityEvent` (goroutines→TUI) and `chan ControlCmd` (TUI→goroutines).

---

## Decision 16: Phase Exit Criteria = TUI-Runnable

**Decision**: Every implementation phase MUST end with a runnable TUI where deliverables are visible and interactive.

---

## Decision 17: Headless Mode Uses JSON-Structured stdout

**Decision**: Headless mode logs structured JSON lines to stdout, errors to stderr.

---

## Decision 18: Hybrid Recommendation Engine (MFCC + Chroma + CLAP)

**Decision**: Three-component hybrid vector (MFCC 40-dim + Chroma 12-dim + CLAP 512-dim) → 564-dim via late fusion with weights 0.25/0.15/0.60.

---

## Decision 19: Multi-Metric Quality Scoring (SNR + Spectral Centroid + Crest Factor)

**Decision**: Composite 0.0–1.0 quality score with three weighted metrics and SNR < 10 dB kill switch.

---

## Decision 20: Per-Track Data Model (Albums → Tracks → Embeddings)

**Decision**: Model IA items as albums containing multiple tracks. Each track gets its own embedding. Replace the old per-item model (`catalog_queue` → `track_embeddings` by `ia_identifier`).

**Rationale**: IA items are albums/collections containing many audio files. The old model picked one arbitrary MP3 per item, discarding all other tracks. A 15-song concert got one embedding from one random song. Per-track granularity enables meaningful browsing, searching, similarity comparison, and playback of individual songs.

**Schema**: `albums` (1) → `tracks` (N) → `track_embeddings` (1:1 with tracks).

---

## Decision 21: Coordinator Owns Album Resolution

**Decision**: Album resolution (fetching metadata, creating track records) runs in the coordinator's lifecycle, NOT in the worker/analyzer pool. Resolvers start/stop with the coordinator (`[s]`/`[x]`), while analyzers are managed independently (`[w]`/`[W]`).

**Rationale**: Users expect the coordinator to fully populate the work queue. If workers resolved albums, the Browse tab would show nothing until workers ran. Having the coordinator handle both discovery and resolution ensures tracks are available for analysis (and browsing) as soon as the coordinator runs.

---

## Decision 22: MP3 Format 3-Tier Filtering

**Decision**: When resolving albums, filter MP3 files with three tiers:
1. Accept ALL "VBR MP3" automatically
2. For "MP3" (CBR), check bitrate ≥ 192kbps
3. Blacklist "64Kbps MP3" and "128Kbps MP3"

**Rationale**: VBR MP3 files are either modern IA engine derivatives or high-quality user uploads. CBR files labeled simply "MP3" may be low bitrate. Legacy IA format tags like "128Kbps MP3" explicitly indicate low-fi compression.

---

## Decision 23: Album Art via Terminal Image Protocols

**Decision**: Download and cache album art from `https://archive.org/services/img/{identifier}`. Render inline in terminals that support iTerm2/Kitty/Sixel protocols via `github.com/BourgeoisBear/rasterm`. Text placeholder fallback for unsupported terminals.

**Rationale**: Album art is essential for verifying album identity when browsing. Most modern terminals (Ghostty, WezTerm, iTerm2, Kitty) support inline images. Art is cached to `data/art/` to avoid re-downloading.

---

## Decision 24: Browse Tab — Three View Modes

**Decision**: Browse tab has three views: Albums list, Album detail, Tracks list. Toggle with `[m]`, drill into albums with `[enter]`, back with `[esc]`.

**Rationale**: Users need to browse both at the album level (to verify what's been indexed, see art, check track counts) and at the track level (to search/play individual songs, run similarity queries).
