# Architecture (Revision 2 — TUI-Based, Scriptless)

## Problem

We need a local-first audio indexing pipeline that discovers IA music tracks, partially streams them, extracts ML features, and stores vector embeddings — all controllable, observable, and runnable from a single terminal application with no shell scripts, no separate processes, and no external orchestration tools.

## Current Behavior

- Fresh Go module (`github.com/johnarleyburns/parso-ia-music-indexer`)
- No existing code, no database, no pipeline
- Blank slate

## Research Findings

### Internet Archive APIs

Same as Revision 1:
- **Advanced Search API** — 10,000-result pagination limit. Unusable for full catalog traversal.
- **Scraping API** (`archive.org/services/search/v1/scrape`) — Cursor-based pagination, no depth limit, supports `sorts=downloads desc`. **Must use this.**

### Audio Analysis (unchanged)

- 20 MFCC bands via `go-mfcc`, mean+variance → 40-dim float32 vector
- SNR from PCM samples → quality score
- Stream ~30 seconds via HTTP Range, decode with `go-mp3`, discard after analysis

### Audio Playback (new)

- `github.com/gopxl/beep/v2` — High-level audio Streamer abstraction
  - Uses `go-mp3` internally (shared decoder with MFCC pipeline)
  - Oto backend for cross-platform speaker output (macOS, Linux, Windows)
  - Supports play, pause, resume, stop, volume control
  - Can stream from `io.Reader` (HTTP response body directly, no disk write)

### TUI Framework (new)

- `charm.land/bubbletea/v2` — Elm Architecture (Model/Update/View)
- `charm.land/bubbles/v2` — Pre-built components (viewport, table, textinput, spinner, progress, help, key)
- `charm.land/lipgloss/v2` — Terminal styling, layout (JoinHorizontal/JoinVertical), borders, colors
- No built-in tab component in bubbles — custom tab bar built with Lip Gloss borders/styles
- Multi-panel dashboards via composite model pattern: single model holds multiple sub-models, routes messages by focus, composes views with Lip Gloss joins

### Operation Modes (new)

The single binary supports two modes:
- **TUI mode** (default) — Full interactive terminal app
- **Headless mode** (`--headless`) — Coordinator + workers run as goroutines with structured JSON logging to stdout/stderr; no terminal UI. Used for CI, e2e testing, and future background server operation.

## Design Proposal

### High-Level Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                   SINGLE GO BINARY                            │
│                                                               │
│  ┌─────────────────────────────────────────────────────┐     │
│  │              bubbletea TUI (default mode)            │     │
│  │  ┌───────────┐ ┌──────────┐ ┌─────────┐ ┌────────┐ │     │
│  │  │ Dashboard │ │ Live Log │ │ Browse  │ │ Player │ │     │
│  │  │   Tab     │ │   Tab    │ │   Tab   │ │  Tab   │ │     │
│  │  └───────────┘ └──────────┘ └─────────┘ └────────┘ │     │
│  │           ▲ TUI receives events via channels         │     │
│  └───────────┼─────────────────────────────────────────┘     │
│              │                                                │
│  ┌───────────┴─────────────────────────────────────────┐     │
│  │             internal/ packages (shared)              │     │
│  │                                                      │     │
│  │  ┌────────────┐ ┌────────────┐ ┌──────────────────┐ │     │
│  │  │ internal/ia│ │internal/db │ │ internal/audio/   │ │     │
│  │  │ /          │ │/           │ │                   │ │     │
│  │  │ scrape.go  │ │ db.go      │ │ stream.go         │ │     │
│  │  │ client.go  │ │ queue.go   │ │ decode.go         │ │     │
│  │  │ types.go   │ │ embed.go   │ │ mfcc.go           │ │     │
│  │  └────────────┘ └────────────┘ │ snr.go            │ │     │
│  │                                └──────────────────┘ │     │
│  │  ┌────────────┐ ┌──────────────┐                    │     │
│  │  │internal/   │ │ internal/    │                    │     │
│  │  │rate/       │ │ config/      │                    │     │
│  │  └────────────┘ └──────────────┘                    │     │
│  └──────────────┬──────────────────────────────────────┘     │
│                 │                                             │
│  ┌──────────────▼──────────────────────────────────────┐     │
│  │            Background Goroutines                     │     │
│  │  ┌──────────────────┐  ┌─────────────────────────┐  │     │
│  │  │   Coordinator    │  │     Worker Pool           │  │     │
│  │  │   goroutine      │  │  (N goroutines)           │  │     │
│  │  │                  │  │  ┌────┐ ┌────┐ ┌────┐    │  │     │
│  │  │  IA Scraping API │  │  │ W1 │ │ W2 │ │ W3 │••• │  │     │
│  │  │  → catalog_queue │  │  └────┘ └────┘ └────┘    │  │     │
│  │  │  → events chan   │  │       │ events chan       │  │     │
│  │  └──────────────────┘  └───────┼───────────────────┘  │     │
│  │                                 │                       │     │
│  │             ┌───────────────────┘                       │     │
│  │             ▼                                          │     │
│  │     ActivityEvent channel ──▶ TUI model                │     │
│  │     ControlCmd channel    ◀── TUI model                │     │
│  └────────────────────────────────────────────────────────┘     │
│                                                                 │
│          ┌──────────────────────────┐                           │
│          │   SQLite + sqlite-vec     │                           │
│          │   (data/parso_indexer.db) │                           │
│          └──────────────────────────┘                           │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Headless Mode (--headless)                               │   │
│  │  • Same internal packages + goroutines                    │   │
│  │  • Structured JSON logging to stdout                      │   │
│  │  • Graceful shutdown on SIGINT/SIGTERM                    │   │
│  │  • Exit codes for CI                                      │   │
│  └──────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────┘
```

### Component Design

#### 1. Single Entry Point (`cmd/tui/main.go`)

Parses flags, initializes config, opens database. If `--headless`:
- Creates coordinator + worker pool goroutines
- Logs structured JSON to stdout
- Blocks until signal, then graceful shutdown

If no `--headless` (default):
- Initializes `bubbletea.NewProgram(mainModel{...})`
- Runs TUI event loop
- On quit, graceful shutdown of background goroutines

#### 2. TUI Model (`internal/tui/model.go`)

Main model holding all state:
```go
type MainModel struct {
    // Navigation
    Tabs      []string
    ActiveTab int

    // Sub-models per tab
    Dashboard  DashboardModel
    LiveLog    LiveLogModel
    Browse     BrowseModel
    Player     PlayerModel

    // Background communication
    Events      chan ActivityEvent    // goroutines → TUI
    Controls    chan ControlCmd       // TUI → goroutines

    // Global state
    DB          *db.DB
    Config      *config.Config
    Coordinator *CoordinatorState     // running/stopped, cursor, count
    WorkerPool  *WorkerPoolState      // worker count, per-worker progress

    // Keybindings
    Keys        KeyMap
    Help        help.Model

    // Terminal
    Width, Height int
    Ready         bool
}
```

#### 3. Dashboard Tab (`internal/tui/dashboard.go`)

- **Left panel**: DB stats table (total, pending, processing, completed, failed) — auto-refreshes every 2 seconds via `tea.Tick`
- **Right top**: Coordinator status widget (▶ running / ▪ stopped, items indexed, cursor position), Worker pool status (N workers, per-worker progress bars)
- **Right bottom**: Activity feed (last ~20 events, color-coded, scrollable viewport)
- **Controls**: `s` start coordinator, `x` stop coordinator, `w` add worker, `W` remove worker, `+`/`-` adjust max concurrency

#### 4. Live Log Tab (`internal/tui/livelog.go`)

- Full-screen scrollable viewport of all events
- Color-coded: green (completed), cyan (queue added), yellow (processing), red (failed), gray (info)
- Auto-scroll toggle (`S` key)
- Filter by event type (future)

#### 5. Browse / Search Tab (`internal/tui/browse.go`)

- Text input for IA identifier or free-text search
- Results in a `bubbles/table`: IA identifier, collection, quality score, status, distance
- `enter` on result → jump to Player tab and queue track for playback
- `v` on result → vector similarity search: top 5 most similar by cosine distance
- Similarity results shown in same table with distance column
- `p` on any result → play it directly

#### 6. Player Tab (`internal/tui/player.go`)

- Track info display (IA identifier, collection, quality score)
- Playback controls: `space` play/pause, `s` stop, `n` next track, `←`/`→` seek, `↑`/`↓` volume
- Elapsed time / total time display
- Volume bar
- Play queue: list of queued track identifiers, shows current + upcoming
- Uses `gopxl/beep/v2` for audio: HTTP stream → beep MP3 decoder → Oto speaker
- Audio runs in its own goroutine, updates player state via channel

### Event System

#### Event Types (goroutines → TUI)

```go
type ActivityEvent struct {
    Timestamp  time.Time
    Type       EventType
    Identifier string                 // IA identifier (if track-specific)
    Message    string                 // Human-readable
    Data       map[string]interface{} // Additional fields (worker_id, error, snr, etc.)
}

type EventType string
const (
    EventQueueAdded        EventType = "queue_added"
    EventAnalysisStarted   EventType = "analysis_started"
    EventAnalysisComplete  EventType = "analysis_complete"
    EventAnalysisFailed    EventType = "analysis_failed"
    EventCoordProgress     EventType = "coordinator_progress"
    EventCoordDone         EventType = "coordinator_done"
    EventWorkerStarted     EventType = "worker_started"
    EventWorkerStopped     EventType = "worker_stopped"
    EventStatsUpdate       EventType = "stats_update"
    EventAppStarted        EventType = "app_started"
    EventAppShutdown       EventType = "app_shutdown"
)
```

#### Control Commands (TUI → goroutines)

```go
type ControlCmd struct {
    Action   ControlAction
    WorkerID string
}

type ControlAction string
const (
    CmdStartCoordinator ControlAction = "start_coordinator"
    CmdStopCoordinator  ControlAction = "stop_coordinator"
    CmdAddWorker        ControlAction = "add_worker"
    CmdRemoveWorker     ControlAction = "remove_worker"
    CmdSetConcurrency   ControlAction = "set_concurrency"
    CmdShutdown         ControlAction = "shutdown"
)
```

### Headless Mode Design

```
$ parso-indexer --headless --db-path data/parso_indexer.db --workers 4

{"ts":"2026-06-16T10:00:00Z","event":"app_started","mode":"headless","workers":4}
{"ts":"2026-06-16T10:00:01Z","event":"coordinator_started","query":"(collection:etree OR ...)","sorts":"downloads desc"}
{"ts":"2026-06-16T10:00:03Z","event":"queue_added","count":1000,"cursor":"abc123"}
{"ts":"2026-06-16T10:00:04Z","event":"worker_started","worker_id":"w1"}
{"ts":"2026-06-16T10:00:05Z","event":"worker_started","worker_id":"w2"}
{"ts":"2026-06-16T10:00:06Z","event":"analysis_started","identifier":"gd1977-05-08.sbd.miller.12345","worker_id":"w1"}
{"ts":"2026-06-16T10:00:36Z","event":"analysis_complete","identifier":"gd1977-05-08.sbd.miller.12345","snr":24.5,"worker_id":"w1"}
{"ts":"2026-06-16T10:00:40Z","event":"queue_added","count":1000,"cursor":"def456"}
...
{"ts":"2026-06-16T10:05:00Z","event":"app_shutdown","reason":"SIGINT","completed":42,"pending":3958000}
```

Key rules for headless mode:
- Same internal packages as TUI mode — no code duplication
- Structured JSON logging to stdout; errors to stderr
- Graceful shutdown on SIGINT/SIGTERM — releases queue locks, writes cursor state
- Exit code 0 on clean shutdown, non-zero on fatal error
- Accepts same env vars / flags as TUI mode for configuration

### SQLite Database (unchanged from Rev 1)

Tables: `catalog_queue`, `track_embeddings` (vec0 virtual table), `cursor_state`

### Vector Format (unchanged from Rev 1)

40-dim float32: mean(20 MFCC bands) + variance(20 MFCC bands)

## System Changes

This is a greenfield project. All files are new.

Target Go module: `github.com/johnarleyburns/parso-ia-music-indexer`

### Dependencies

| Package | Purpose |
|---|---|
| `charm.land/bubbletea/v2` | TUI framework (Elm Architecture) |
| `charm.land/bubbles/v2` | Pre-built TUI components |
| `charm.land/lipgloss/v2` | Terminal styling and layout |
| `charm.land/log/v2` | Structured logging for headless mode |
| `github.com/mattn/go-sqlite3` | SQLite driver |
| `github.com/asg017/sqlite-vec-go-bindings` | sqlite-vec Go bindings |
| `github.com/hajimehoshi/go-mp3` | MP3 decoder (shared by MFCC pipeline + beep) |
| `github.com/zrma/go-mfcc` | MFCC extraction |
| `github.com/gopxl/beep/v2` | Audio playback |
| `github.com/gopxl/beep/v2/mp3` | MP3 decoder for beep |
| `github.com/gopxl/beep/v2/speaker` | Audio output via Oto |
| `golang.org/x/time/rate` | Rate limiting |

### File Layout (Revised)

```
parso-ia-music-indexer/
├── cmd/
│   └── tui/
│       └── main.go              # Entry point: TUI mode or --headless
├── internal/
│   ├── audio/                   # Audio analysis pipeline
│   │   ├── stream.go
│   │   ├── decode.go
│   │   ├── mfcc.go
│   │   ├── snr.go
│   │   └── types.go
│   ├── config/
│   │   └── config.go            # Config struct, env vars, flags
│   ├── db/                      # Database operations
│   │   ├── db.go
│   │   ├── queue.go
│   │   └── embeddings.go
│   ├── ia/                      # IA API client
│   │   ├── client.go
│   │   ├── scrape.go
│   │   └── types.go
│   ├── rate/                    # Rate limiting
│   │   ├── limiter.go
│   │   └── throttled_reader.go
│   └── tui/                     # TUI layer (NEW)
│       ├── model.go             # Main model, tabs, keymap
│       ├── dashboard.go         # Dashboard tab
│       ├── livelog.go           # Live log tab
│       ├── browse.go            # Browse/search tab
│       ├── player.go            # Player tab
│       ├── components/
│       │   ├── statusbar.go     # Player bar component
│       │   ├── activity.go      # Activity feed component
│       │   └── tabbar.go        # Tab bar component
│       ├── events.go            # Event types + channel types
│       ├── controls.go          # Control command types
│       └── styles.go            # Lip Gloss theme/colors
├── tests/
│   └── e2e/
│       ├── run.sh               # E2E test runner
│       └── assertions.go        # Go helpers for DB assertions
├── data/
│   └── .gitkeep
├── plans/
│   └── ia-music-indexer/
│       └── 2026-06-16/
│           ├── 00-overview.md
│           ├── architecture.md
│           ├── data-model.md
│           ├── implementation.md
│           ├── testing.md
│           ├── decisions.md
│           └── current_state.md
├── go.mod
├── go.sum
├── .gitignore
└── AGENTS.md
```

## Open Questions

1. **sqlite-vec Go bindings**: Need to verify `asg017/sqlite-vec-go-bindings` works on macOS arm64 and supports the `vec0` virtual table API.

2. **go-mfcc compatibility**: Does `zrma/go-mfcc` match librosa's MFCC output sufficiently? If discrepancies are large, may need a custom implementation.

3. **IA MP3 derivative reliability**: Does every IA audio item have a `.mp3` derivative? Fallback strategy for items with only FLAC/OGG needed.

4. **Range request reliability**: Does IA's CDN reliably support HTTP Range requests on all items? Need to test with real items.

5. **beep/v2 MP3 decoder**: Does `beep/v2/mp3` work with streaming `io.Reader` (HTTP response body) or does it require a full seekable file? May need to buffer the stream in memory.

6. **SQLite WAL mode + concurrent goroutines**: Multiple goroutines writing to SQLite. Need connection pooling with mutex for writes, or a single writer goroutine with channel-based submission.
