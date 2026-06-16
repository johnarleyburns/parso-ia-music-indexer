# Current State

## Date: 2026-06-16

## Status: PLANNING COMPLETE (Revision 2) — TUI-Based, Scriptless

## Completed Phases

- [x] Research — Chat session analysis, IA API research, ML model research, database comparison, TUI framework research
- [x] Design — Architecture design (Rev 2: TUI-based), data model design, component specifications, event system design
- [x] Planning — 7-phase implementation plan with per-phase exit criteria, e2e test suite plan

## Pending Phases

- [ ] Phase 1 — TUI Scaffold & Navigation
- [ ] Phase 2 — Database Layer + Embedding Operations
- [ ] Phase 3 — Dashboard Tab: Live Stats & Controls
- [ ] Phase 4 — IA Scraping API Client + Real Coordinator
- [ ] Phase 5 — Audio Analysis Pipeline + Real Worker Pool
- [ ] Phase 6 — Browse / Search Tab
- [ ] Phase 7 — Player Tab

## Architecture (Revision 2 Key Changes from Rev 1)

| Aspect | Rev 1 (Shell Scripts) | Rev 2 (TUI) |
|---|---|---|
| Entry point | 3+ binaries (coordinator, worker, search) | 1 binary (TUI or --headless) |
| Orchestration | Shell scripts (`scripts/*.sh`) | Bubble Tea TUI (4 tabs) |
| Worker mgmt | Separate Go processes | Goroutines managed by TUI model |
| Progress view | No live view (log files) | Dashboard with live stats, progress bars, activity feed |
| Search | Ad-hoc CLI | Browse tab with textinput + table |
| Playback | None | Player tab via beep/v2 |
| Testing | Unit + integration | Full e2e suite via --headless mode |
| Shell scripts | Required | **Eliminated** |

### TUI Tabs

| Tab | Purpose |
|---|---|
| Dashboard | DB stats (auto-refresh), coordinator/worker controls, progress bars, activity feed |
| Live Log | Full-screen scrollable event stream (color-coded) |
| Browse | Search bar, results table, vector similarity queries |
| Player | Play/pause/stop/seek/volume, play queue |

### Operation Modes

| Mode | Command | Use Case |
|---|---|---|
| TUI (default) | `go run ./cmd/tui` | Interactive development, manual operation |
| Headless | `go run ./cmd/tui --headless` | CI, e2e testing, background server |

## Known Blockers

1. **sqlite-vec Go bindings**: Need to verify `asg017/sqlite-vec-go-bindings` works on macOS arm64 with vec0 virtual table.

2. **go-mfcc library**: Need to verify output compatibility with librosa's MFCC. If large discrepancies, may need custom implementation.

3. **IA Scraping API stability**: Need to test `/services/search/v1/scrape` with music collection queries.

4. **beep/v2 MP3 streaming**: Need to verify `beep/v2/mp3` can decode from a non-seekable `io.Reader` (HTTP response body) for playback. May need to buffer in memory.

5. **SQLite WAL + concurrent goroutines**: Need connection strategy (mutex-protected writes, or single writer goroutine).

## Architectural Decisions (17 total)

| # | Decision | Status |
|---|---|---|
| D1 | Go language | Final |
| D2 | SQLite + sqlite-vec (local) | Final |
| D3 | Cursor-based Scraping API | Final |
| D4 | 20 MFCC bands → 40-dim vector, 30s | Final |
| D5 | SNR quality score from PCM | Final |
| D6 | Coordinator/Worker decoupled goroutines | Final |
| D7 | TUI orchestration (was: shell scripts) | Final |
| D8 | Goroutine pool, configurable concurrency | Final |
| D9 | 1.6 MB HTTP Range request | Final |
| D10 | Future: Turso/libSQL + colima/incus | Design only |
| D11 | Single binary, TUI + headless modes | Final |
| D12 | Bubble Tea TUI framework | Final |
| D13 | Goroutines (not subprocesses) | Final |
| D14 | gopxl/beep/v2 for audio playback | Final |
| D15 | Event channels (TUI ↔ goroutines) | Final |
| D16 | Per-phase exit criteria (TUI-runnable) | Final |
| D17 | JSON-structured stdout in headless mode | Final |

## Database Schema (Designed, Not Implemented)

- `catalog_queue` — Queue state machine (pending/processing/completed/failed)
- `track_embeddings` — Vector embeddings via sqlite-vec `vec0`
- `cursor_state` — Coordinator resume state

## Go Dependencies (Identified, Not Added)

| Package | Purpose |
|---|---|
| `charm.land/bubbletea/v2` | TUI framework |
| `charm.land/bubbles/v2` | TUI components |
| `charm.land/lipgloss/v2` | Terminal styling |
| `github.com/mattn/go-sqlite3` | SQLite driver |
| `github.com/asg017/sqlite-vec-go-bindings` | sqlite-vec |
| `github.com/hajimehoshi/go-mp3` | MP3 decoder |
| `github.com/zrma/go-mfcc` | MFCC extraction |
| `github.com/gopxl/beep/v2` | Audio playback |
| `github.com/gopxl/beep/v2/mp3` | MP3 beep decoder |
| `github.com/gopxl/beep/v2/speaker` | Audio output |
| `golang.org/x/time/rate` | Rate limiting |

## Files on Disk

```
parso-ia-music-indexer/
├── .git/
├── AGENTS.md
├── chat-session.txt
├── go.mod
└── plans/
    └── ia-music-indexer/
        └── 2026-06-16/
            ├── 00-overview.md       ← UPDATED (Rev 2)
            ├── architecture.md      ← UPDATED (Rev 2 — TUI-based)
            ├── data-model.md        ← (unchanged from Rev 1)
            ├── implementation.md    ← UPDATED (Rev 2 — 7 phases + exit criteria)
            ├── testing.md           ← UPDATED (Rev 2 — +e2e suite)
            ├── decisions.md         ← UPDATED (Rev 2 — 17 decisions)
            └── current_state.md     ← UPDATED (this file)
```

## Next Action

**Ready to begin Phase 1 implementation.**

Phase 1 deliverables:
1. `cmd/tui/main.go` — entry point with `--headless` flag
2. `internal/tui/` — main model, tab bar, placeholder tab views, styles
3. `internal/config/` — configuration from env vars + flags
4. Exit criteria: TUI launches, 4 tabs visible and switchable, `q` quits, `--headless` prints message and exits
