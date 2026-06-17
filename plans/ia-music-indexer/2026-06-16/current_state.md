# Current State

## Date: 2026-06-17

## Status: PHASE 7 IMPLEMENTED — Player Tab (Pending Verification)

**Phase 7 IMPLEMENTED** — Audio player tab with beep/v2, playback controls (play/pause/stop/seek/volume), play queue, download+stream from IA, graceful shutdown.

## Architecture (Revision 3)

See `architecture.md` for full details. Three-tier pipeline:

```
Tier 1: Coordinator (singleton) [s/x]  → albums table
Tier 2: Album Resolvers (pool)  [r/R]  → tracks table  
Tier 3: Track Analyzers (pool)  [w/W]  → track_embeddings table
```

## Completed Phases

- [x] Research
- [x] Design (Rev 1 → Rev 2 → Rev 3)
- [x] Planning
- [x] Phase 1 — TUI Scaffold & Navigation
- [x] Phase 2 — Database Layer + Embedding Operations
- [x] Phase 2B — Hybrid Recommendation Engine Core (MFCC + Chroma + CLAP + Fusion + Quality)
- [x] Phase 3 — Dashboard Tab: Live Stats & Controls
- [x] Phase 4 — IA Scraping API Client + Real Coordinator
- [x] Phase 5 — Audio Analysis Pipeline + Real Worker Pool
- [x] Phase 6 — Browse / Search Tab
- [x] Phase PX — Per-Track Data Model + Album Art + Browse Redesign
  - [x] PX-1: Schema migration (albums/tracks/track_embeddings), DB ops, IA metadata, worker pipeline, dashboard
  - [x] PX-2: Browse tab redesign (Albums view, Album detail, Tracks view)
  - [x] PX-3: Album art (rasterm, caching, terminal image rendering)
  - [x] Bug fixes: cursor bounds checking, coordinator owns resolution
- [x] Phase P6B — Browse Tab Fixes
  - [x] P6B-1: Schema + download count storage (IA scrape API `fields=identifier,downloads`)
  - [x] P6B-2: Album average quality (AVG subquery on track_embeddings)
  - [x] P6B-3: Browse tab keybinding + navigation fixes (tab focus toggle, mode hints, detail return)
  - [x] P6B-4: Album art hardening (protocol detection, multiplexer fallback, TIMBRE_NO_ART)
  - [x] P6B-5: Default sort + display updates (downloads DESC, new columns, formatting)
  - [x] P6B-6: Verification
- [x] Phase 7 — Player Tab (pending user verification)
  - [x] P7-1: Add beep/v2 dependencies (gopxl/beep/v2, mp3, speaker, effects)
  - [x] P7-2: Player engine (PlayerEngine with beep Ctrl, Volume, Seek, Position)
  - [x] P7-3: PlayerModel UI (queue, now-playing, progress bar, volume, hints)
  - [x] P7-4: Status bar (integrated into player.go View method)
  - [x] P7-5: SwitchToPlayerMsg forwarding + player message routing in model.go
  - [x] P7-6: Graceful shutdown (engine.Close on quit, speaker.Clear on stop)
  - [x] P7-7: Build + test verification (43 tests passing, binary built)

## Pending Phases

None — all planned phases complete.

## Database Schema

```
albums ──1:N──▶ tracks ──1:1──▶ track_embeddings
cursor_state (singleton)
```

Art cache: `data/art/{identifier}.jpg`

## Tests

```
internal/audio   16 tests
internal/db      20 tests
internal/hybrid   5 tests
internal/rate     2 tests
Total: 43 tests passing
```

## Known Blockers

1. ~~sqlite-vec~~ — Resolved: pure Go cosine similarity
2. ~~SQLite WAL + concurrency~~ — Resolved: mutex-serialized writes
3. ~~beep/v2 MP3 streaming~~ — Resolved: bytes.Reader (seekable) wrapping downloaded MP3 data
4. **CLAP model memory** — ~300MB, verify on 8GB MacBooks
5. **gRPC proto generation** — Need protoc toolchain or committed generated code

## Next Action

User verification of Phase 7 exit criteria.
