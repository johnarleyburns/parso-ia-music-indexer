# Current State

## Date: 2026-06-18

## Status: CLAP Sidecar Lifecycle Management COMPLETE — Pending Verification

**CLAP sidecar lifecycle management COMPLETE** — Binary auto-starts Python sidecar if not running, hard error if connection fails, process killed on exit.

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
- [x] CLAP gRPC Integration
  - [x] Added gRPC Go dependencies (google.golang.org/grpc, google.golang.org/protobuf)
  - [x] Generated Go proto stubs (internal/clap/clap_proto/)
  - [x] Generated Python proto stubs (python_sidecar/clap_pb2*.py)
  - [x] Implemented real grpcCLAPClient (internal/clap/grpc_client.go)
  - [x] Added Float32ToBytes PCM conversion utility
  - [x] Wired real CLAP client into main.go with graceful fallback to mock
  - [x] Fixed analyzeTrack to pass actual PCM data to GetEmbedding
  - [x] Updated Makefile with `proto` and `test` targets
  - [x] Added 9 tests for CLAP client (mock, PCM conversion, unreachable host)
- [x] CLAP Sidecar Lifecycle Management
  - [x] Added `internal/clap/sidecar.go` with `EnsureSidecar()` and `SidecarProcess`
  - [x] Auto-detects running sidecar, auto-starts if not running
  - [x] Hard error on connection failure (no silent mock fallback)
  - [x] Process cleanup on program exit (SIGTERM → SIGKILL)
  - [x] Added `--clap-sidecar-dir` config flag
  - [x] Moved CLAP client ownership to `runTUI`/`runHeadless`
  - [x] Added 5 sidecar tests

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
internal/clap    14 tests
internal/db      20 tests
internal/hybrid   5 tests
internal/rate     2 tests
internal/tui     15 tests
Total: 72 tests passing
```

## Known Blockers

1. ~~sqlite-vec~~ — Resolved: pure Go cosine similarity
2. ~~SQLite WAL + concurrency~~ — Resolved: mutex-serialized writes
3. ~~beep/v2 MP3 streaming~~ — Resolved: bytes.Reader (seekable) wrapping downloaded MP3 data
4. **CLAP model memory** — ~300MB, verify on 8GB MacBooks
5. ~~gRPC proto generation~~ — Resolved: protoc + Go/Python plugins, `make proto` target, stubs committed

## Next Action

User verification of CLAP sidecar lifecycle management:
1. `make build` produces `bin/timbre`
2. `make test` — 72 tests passing
3. Without sidecar running: binary auto-starts Python sidecar, connects, and proceeds
4. With sidecar already running: binary detects it and connects immediately
5. Without python_sidecar/ dir: binary exits with clear error message
6. On quit: sidecar child process is killed
