# Implementation Plan (Revision 2 — TUI-Based, Scriptless)

## Problem

Build the local-first audio indexing pipeline as a single Go binary with a Bubble Tea TUI (default) or headless mode (`--headless`). Each phase must be independently testable from within the TUI before proceeding to the next phase.

## Critical Rule

**Every phase MUST end with a runnable TUI where the phase's results are visible and interactive.** No phase is complete until the user can launch the TUI and verify the deliverables manually.

---

## Phase 1 — TUI Scaffold & Navigation

**Goal**: Skeleton TUI that launches, shows tab bar, switches tabs, has help footer, quits cleanly. Headless mode skeleton.

### Files to Create

```
cmd/tui/main.go                    # Entry point: --headless flag, TUI vs headless dispatch
internal/config/config.go          # Config struct, env var defaults, flag parsing
internal/tui/model.go              # MainModel struct, tea.Model implementation
internal/tui/styles.go             # Lip Gloss theme (colors, borders, tab styles)
internal/tui/components/tabbar.go  # Tab bar renderer (active/inactive styles)
internal/tui/dashboard.go          # Placeholder view: "Dashboard — coming in Phase 2"
internal/tui/livelog.go            # Placeholder view: "Live Log — coming in Phase 3"
internal/tui/browse.go             # Placeholder view: "Browse — coming in Phase 6"
internal/tui/player.go             # Placeholder view: "Player — coming in Phase 7"
internal/tui/controls.go           # ControlCmd types (stub)
data/.gitkeep
```

### Dependencies to Add

```
charm.land/bubbletea/v2
charm.land/bubbles/v2
charm.land/lipgloss/v2
```

### Key Implementation Details

- `cmd/tui/main.go`: Parses `--headless`, `--db-path` (default `data/parso_indexer.db`). If headless, prints "headless mode — not yet implemented" and exits 0. Otherwise starts `tea.NewProgram(model)`.
- `internal/tui/model.go`: `MainModel` with `Tabs []string`, `ActiveTab int`, `Help help.Model`, `Keys KeyMap`. `Init()` returns `tea.SetWindowTitle("Parso IA Music Indexer")`. `Update()` handles `tea.KeyMsg` for tab switching (`tab`/`shift+tab`), help toggle (`?`), quit (`q`/`ctrl+c`), and `tea.WindowSizeMsg`. `View()` composes tab bar + active tab content + help footer.
- `internal/tui/components/tabbar.go`: Renders tab names with active/inactive Lip Gloss styles (active = bold + colored border-bottom, inactive = dimmed). Uses `lipgloss.JoinHorizontal`.
- `internal/tui/styles.go`: Color constants, border definitions, `ActiveTabStyle`, `InactiveTabStyle`, `HelpStyle`, `TitleStyle`.

### Exit Criteria (User-Testable)

1. `go run ./cmd/tui` launches TUI in alt-screen buffer
2. Tab bar shows: `[Dashboard] [Live Log] [Browse] [Player]` — active tab highlighted
3. `tab` cycles forward through tabs; `shift+tab` cycles backward
4. Each tab shows its placeholder name centered on screen
5. `?` toggles help footer showing keybindings at bottom
6. `q` or `ctrl+c` quits cleanly, restores terminal
7. Resize terminal — layout adapts
8. `go run ./cmd/tui --headless` prints "headless mode — not yet implemented" and exits 0

---

## Phase 2 — Database Layer + Embedding Operations

**Goal**: Real SQLite schema with sqlite-vec loaded, queue operations, vector insert/search. Stats visible in Dashboard tab.

### Files to Create

```
internal/db/db.go            # SQLite connection, WAL mode, sqlite-vec loading, migrations
internal/db/queue.go         # ClaimNextBatch, MarkCompleted, MarkFailed, ResetStuckJobs, GetStats
internal/db/embeddings.go    # SaveEmbedding, QuerySimilar, GetTrackCount
```

### Files to Modify

```
cmd/tui/main.go              # Create DB connection, pass to TUI model
internal/tui/model.go        # Add *db.DB field, pass to dashboard
internal/tui/dashboard.go    # Replace placeholder with real DB stats table
```

### Dependencies to Add

```
github.com/mattn/go-sqlite3
github.com/asg017/sqlite-vec-go-bindings
```

### Key Implementation Details

- `internal/db/db.go`: `Open(path string) (*DB, error)` — opens SQLite with `?_journal_mode=WAL&_busy_timeout=5000`, loads `sqlite-vec` extension via CGo/plugin, runs `Migrate()` which creates `catalog_queue`, `track_embeddings` (vec0 virtual table), `cursor_state` tables if not exist.
- `internal/db/queue.go`: `GetStats(db *sql.DB) (*QueueStats, error)` — returns counts by status. Used by Dashboard for auto-refresh.
- `internal/db/embeddings.go`: Stub `QuerySimilar` — returns empty for now.
- `internal/tui/dashboard.go`: Left panel renders a styled table showing QueueStats. Uses `tea.Tick(2*time.Second)` to auto-refresh. Placeholder for right panel ("Controls — Phase 3").

### Exit Criteria

1. TUI launches, Dashboard tab shows real DB stats table:
   ```
   ┌─ Database ─────────────┐
   │ Total:             0   │
   │ Pending:           0   │
   │ Processing:        0   │
   │ Completed:         0   │
   │ Failed:            0   │
   └────────────────────────┘
   ```
2. Stats numbers auto-refresh every 2s (visible from TUI, cursor doesn't move)
3. `data/parso_indexer.db` exists on disk after running
4. `sqlite3 data/parso_indexer.db ".schema"` shows all three tables with correct columns
5. Other tabs still show placeholders but tab switching works
6. `go run ./cmd/tui --headless` opens DB, prints DB stats as JSON, exits 0

---

## Phase 3 — Dashboard Tab: Live Stats & Controls

**Goal**: Dashboard shows live-updating stats, coordinator/worker controls (start/stop with simulated behavior), and activity feed.

### Files to Create

```
internal/tui/events.go             # ActivityEvent types, event channel setup
internal/tui/components/activity.go # Activity feed viewport component
```

### Files to Modify

```
internal/tui/model.go              # Add Events/Controls channels, coordinator/worker state
internal/tui/dashboard.go          # Full layout: left stats, right controls + activity feed
internal/tui/livelog.go            # Full-screen activity feed (mirrors dashboard feed)
internal/tui/controls.go           # Real ControlCmd types
cmd/tui/main.go                    # Wire channels between TUI and (stub) goroutines
```

### Key Implementation Details

- `internal/tui/events.go`: `ActivityEvent` struct with Type, Timestamp, Identifier, Message, Data. Event type constants. `NewEventChannel() chan ActivityEvent` with buffer size 100.
- `internal/tui/controls.go`: `ControlCmd` struct. `NewControlChannel() chan ControlCmd` with buffer size 10.
- `internal/tui/dashboard.go`:
  - Left: DB stats table (from Phase 2)
  - Right top: Coordinator status panel (▶ Running / ▪ Stopped, cursor, indexed count) + Worker pool panel (N workers, per-worker spinners)
  - Right bottom: Activity feed — viewport showing last 20 ActivityEvents, color-coded
  - Keybindings: `s` → send `CmdStartCoordinator` on control channel; `x` → `CmdStopCoordinator`; `w` → `CmdAddWorker`; `W` → `CmdRemoveWorker`
- `internal/tui/livelog.go`: Full-screen viewport showing all events (unlimited history, scrollable). Auto-scroll on by default (`S` toggles).
- `cmd/tui/main.go`: Wire goroutines that listen on control channel. For Phase 3, coordinator goroutine is a stub — on start, sends mock `EventQueueAdded` events every 3 seconds using real IA identifiers from a hardcoded test set. Worker goroutines are stubs — on start, send `EventWorkerStarted`, then wait for stop command.

### Exit Criteria

1. Dashboard tab shows DB stats (live refresh) + right panel with controls
2. `s` → coordinator status panel shows "▶ Running" (green) + spinner
3. Mock coordinator emits `queue_added` events every 3s → activity feed scrolls with entries like:
   ```
   + etree:gd1977-05-08.sbd.miller.12345  (added to queue)
   + georgeblood:VictorSymphony1928  (added to queue)
   ```
4. Activity feed is color-coded: cyan for queue_added, gray for info
5. `x` → coordinator status shows "▪ Stopped" (gray), spinner stops, events stop
6. `w` → worker count increments; per-worker spinners appear in worker panel
7. `W` → worker count decrements; worker removed from panel
8. Live Log tab shows the same activity feed in full-screen, independently scrollable
9. `S` toggles auto-scroll in Live Log
10. `go run ./cmd/tui --headless` spawns stub coordinator, logs JSON events to stdout, exits cleanly on SIGINT

---

## Phase 4 — IA Scraping API Client + Real Coordinator

**Goal**: Real coordinator goroutine populates `catalog_queue` with IA identifiers via the Scraping API. Progress visible in Dashboard.

### Files to Create

```
internal/ia/client.go          # HTTP client with timeout, User-Agent, retry/backoff
internal/ia/scrape.go          # ScrapePage(ctx, cursor, query, sorts, count) → ([]string, nextCursor, error)
internal/ia/types.go           # IA Scraping API response types
internal/rate/limiter.go       # Token bucket helper: Wait(ctx) error, 15 req/min
```

### Files to Modify

```
internal/tui/dashboard.go      # Wire real coordinator start/stop
cmd/tui/main.go                # Wire real coordinator goroutine (replaces stub)
internal/db/queue.go           # Add BulkInsert(identifiers []string) method
```

### Key Implementation Details

- `internal/ia/scrape.go`: `ScrapePage` makes GET to `https://archive.org/services/search/v1/scrape` with params `q=(collection:etree OR collection:georgeblood OR collection:netlabels) AND mediatype:audio`, `sorts=downloads desc`, `count=1000`, `cursor=<cursor>`. Parses JSON response into `ScrapeResponse{Items: []ScrapeItem{Identifier string}, Cursor string, Total int}`.
- `internal/ia/client.go`: `http.Client` with 60s timeout, User-Agent `ParsoIAIndexer/1.0`, exponential backoff on 429/503 (1s, 2s, 4s, 8s max).
- `internal/rate/limiter.go`: Wraps `golang.org/x/time/rate.Limiter` set to 15 req/min (1 per 4s). `Wait(ctx)` blocks until token available.
- `internal/db/queue.go`: `BulkInsertPending(db, identifiers []string) error` — INSERT OR IGNORE INTO catalog_queue for efficiency.
- Coordinator goroutine: Loop — wait for rate limiter → ScrapePage → BulkInsertPending → save cursor to `cursor_state` table → send `EventCoordProgress` + `EventQueueAdded` events → sleep 2s → next page. On stop signal, save cursor and exit. On startup resume, read cursor from `cursor_state` table (or start fresh if none).

### Exit Criteria

1. `s` in Dashboard → coordinator starts, status "▶ Running", shows cursor position and items indexed count
2. Dashboard DB stats table shows Pending count going up in real-time
3. Activity feed shows real IA identifiers being added:
   ```
   + etree:gd1977-05-08.sbd.miller.12345
   + georgeblood:Victor-Herbert-Orchestra-1918
   + netlabels:ds93-summer-ep
   ```
4. Live Log tab shows streaming coordinator events
5. `x` → coordinator stops. `s` again → resumes from saved cursor (no duplicate IDs; Pending count doesn't jump by large amount for already-seen IDs)
6. `ctrl+c` during coordinator run → coordinated shutdown, cursor saved, exit clean
7. `go run ./cmd/tui --headless` runs real coordinator, populates queue, logs JSON events to stdout, exits cleanly on SIGINT with cursor saved
8. Verified: `sqlite3 data/parso_indexer.db "SELECT count(*) FROM catalog_queue"` returns expected count

---

## Phase 5 — Audio Analysis Pipeline + Real Worker Pool

**Goal**: Workers stream, analyze, and embed real IA audio tracks. Progress visible in Dashboard.

### Files to Create

```
internal/audio/stream.go            # HTTP Range download with throttling
internal/audio/decode.go            # MP3 → PCM samples via go-mp3
internal/audio/mfcc.go              # 20 MFCC bands → 40-dim vector (mean+variance)
internal/audio/snr.go               # Signal-to-Noise Ratio from PCM samples
internal/audio/types.go             # AnalysisResult struct
internal/rate/throttled_reader.go   # io.Reader wrapper: 450 KB/s limit
```

### Files to Modify

```
internal/tui/dashboard.go           # Wire real worker start/stop; per-worker progress bars
internal/tui/events.go              # Add AnalysisStarted, AnalysisComplete, AnalysisFailed events
cmd/tui/main.go                     # Wire real worker goroutines (replaces stubs)
internal/db/embeddings.go           # Full SaveEmbedding implementation
internal/ia/client.go               # Add LookupMP3URL(identifier) method
```

### Dependencies to Add

```
github.com/hajimehoshi/go-mp3
github.com/zrma/go-mfcc
golang.org/x/time/rate
```

### Key Implementation Details

- `internal/audio/stream.go`: `StreamAudioChunk(ctx, mp3URL string, maxBytes int) ([]byte, error)` — creates GET request with `Range: bytes=0-{maxBytes}` header, wraps response body in `ThrottledReader(450 KB/s)`, reads up to maxBytes into `[]byte`, returns buffer.
- `internal/audio/decode.go`: `DecodeMP3(data []byte) ([]float64, int, error)` — uses `go-mp3` to decode MP3 bytes→PCM samples (16-bit→float64 normalized to [-1,1]), returns samples + sample rate.
- `internal/audio/mfcc.go`: `ExtractMFCC(samples []float64, sampleRate int) ([]float32, error)` — passes samples to `go-mfcc` configured for 20 bands, default frame size. Iterates over resulting MFCC frames (2D: bands × time). For each band: computes mean and variance across all time frames. Returns flat 40-dim `[]float32`.
- `internal/audio/snr.go`: `CalculateSNR(samples []float64) float64` — splits signal into frames. For each frame computes RMS energy. Sorts frames by energy. Signal power = mean of top 90% frames. Noise floor = mean of bottom 10% frames. Returns `10 * log10(signal_power / noise_floor)` dB.
- `internal/ia/client.go`: `LookupMP3URL(ctx, identifier string) (string, error)` — hits `https://archive.org/metadata/{identifier}` JSON endpoint, parses `files` array, finds first `.mp3` derivative, returns URL.
- `internal/db/embeddings.go`: `SaveEmbedding(db, identifier, embedding []float32, qualityScore float64) error` — INSERT INTO track_embeddings.
- `internal/rate/throttled_reader.go`: Wraps `io.Reader`, rate-limits to 450 KB/s (460800 B/s) using `rate.Limiter`.
- Worker goroutine: Loop — `ClaimNextBatch(db, workerID, batchSize)` → for each identifier: `LookupMP3URL` → `StreamAudioChunk` → `DecodeMP3` → `ExtractMFCC` → `CalculateSNR` → `SaveEmbedding` → `MarkCompleted`. On failure at any step: `MarkFailed(identifier, errorMessage)`. After batch: check for stop signal. Send `EventAnalysisStarted` / `EventAnalysisComplete` / `EventAnalysisFailed` events.

### Exit Criteria

1. Coordinator running (queue populated) + `w` adds workers
2. Workers process real tracks — Dashboard shows:
   - Completed count incrementing in DB stats
   - Per-worker panel showing: `Worker 1: analyzing etree:xyz... (24%) [progress bar]`
   - Activity feed: `✓ etree:gd1977-05-08... (SNR 24.5 dB)` — green
3. Failed tracks show: `✗ georgeblood:abc... (error: no MP3 derivative)` — red, with error message
4. `ResetStuckJobs` recovers stuck tracks:
   - Kill app while worker was `processing`
   - Restart app, open DB stats → Processing count drops, Pending count rises (stuck jobs reset)
5. Progress bars update smoothly (not jumpy)
6. `go run ./cmd/tui --headless --workers 2` processes tracks headlessly, logs JSON events including `analysis_complete` with SNR values
7. Run for 5+ completed tracks, verify `sqlite3 data/parso_indexer.db "SELECT ia_identifier, quality_score FROM track_embeddings LIMIT 5"` returns sensible data

---

## Phase 6 — Browse / Search Tab

**Goal**: Search IA identifiers, view results as table, trigger vector similarity queries.

### Files to Create

(All files exist as stubs; this phase makes them real.)
```
internal/tui/browse.go   # Full implementation: search input, results table, keybindings
```

### Files to Modify

```
internal/tui/model.go    # Wire Browse tab to DB queries; tab-switching to Player
internal/db/embeddings.go # Full QuerySimilar implementation
```

### Key Implementation Details

- `internal/tui/browse.go`:
  - Search bar: `bubbles/textinput` — focused when tab activates
  - Results table: `bubbles/table` with columns (IA Identifier, Collection, Quality Score, Distance)
  - Default query: shows all completed tracks (paginated)
  - Filtered query: as user types, filters by IA identifier prefix (SQL LIKE)
  - Keybindings:
    - `↑`/`↓` navigate table rows
    - `enter` on a row → sends track identifier to Player tab (queue for playback), switches to Player
    - `v` on a row → `QuerySimilar(embedding, limit=5)` → replaces table with top 5 similar tracks (showing distance column)
    - `esc` after similarity search → returns to normal browse
    - `p` on any row → play directly (same as enter but immediate playback)
    - `/` → focus search bar
- `internal/db/embeddings.go`: `QuerySimilar(db, identifier string, limit int) ([]SimilarTrack, error)` — SELECT from track_embeddings WHERE mfcc_embedding MATCH (SELECT mfcc_embedding FROM track_embeddings WHERE ia_identifier = ?) ORDER BY distance LIMIT ?.
- `internal/db/queue.go` or new method: `GetTrackMeta(db, identifier string) (TrackMeta, error)` — returns collection, quality_score, status from joined query.

### Exit Criteria

1. Browse tab shows search bar (auto-focused on tab activation)
2. Table initially shows all completed tracks (or "No tracks indexed yet" if empty)
3. Typing filters the table (live filtering as you type)
4. `↑`/`↓` moves selection highlight in table
5. `enter` on a track → Player tab opens, track is queued
6. `v` on a completed track → table updates showing top 5 similar tracks:
   ```
   IA Identifier              Quality Score  Distance
   etree:gd1977-05-08...       24.5 dB        0.000
   georgeblood:Victor1928...   12.1 dB        0.123
   netlabels:ds93-summer...    31.2 dB        0.156
   audio_music:folk-123...     28.7 dB        0.189
   etree:ph1999-07-15...       22.3 dB        0.201
   ```
7. `esc` returns to normal browse view
8. `p` on any row starts playback immediately
9. Scrollable table with many results
10. `/` focuses the search bar at any time

---

## Phase 7 — Player Tab

**Goal**: Stream and play IA MP3 tracks via beep, with playback controls and play queue.

### Files to Create

```
internal/tui/components/statusbar.go  # Player status bar component
```

### Files to Modify

```
internal/tui/player.go   # Full implementation: play/pause/stop/seek/volume, queue
internal/tui/model.go    # Wire player audio channel, tab switching preserves playback
```

### Dependencies to Add

```
github.com/gopxl/beep/v2
github.com/gopxl/beep/v2/mp3
github.com/gopxl/beep/v2/speaker
```

### Key Implementation Details

- `internal/tui/player.go`:
  - State: `playing`, `paused`, `stopped`; `currentTrack`; `queue []string`; `elapsed time.Duration`; `total time.Duration`; `volume float64` (0.0–1.0).
  - Player bar (bottom of tab): `▶ Now Playing: etree:gd1977-05-08...  [⏸] [⏭] 00:42/03:15 🔊 ████░░░░`
  - Queue display: scrollable list of upcoming tracks
  - Audio playback goroutine:
    1. Receives track identifier via channel
    2. Calls `ia.LookupMP3URL` to get MP3 URL
    3. HTTP GET the MP3 (streaming, no Range header needed for playback)
    4. Decode with `beep/mp3` → `beep.StreamSeekCloser`
    5. Initialize `speaker.Init(sampleRate, bufferSize)` on first playback
    6. Play via `speaker.Play(beep.Seq(streamer, beep.Callback(func(){ /* done */ })))`
    7. Lock/update player state via mutex+channel
  - Keybindings:
    - `space` → play/pause toggle
    - `s` → stop
    - `n` → skip to next in queue
    - `←`/`→` → seek backward/forward 5s (using `streamer.Seek()`)
    - `↑`/`↓` → volume up/down
  - Seek: Use `beep.ResamplerRatio` or wrap streamer with `beep.Take` for position tracking. For actual seeking, the MP3 streamer from beep implements `beep.Seeker`.
- `internal/tui/components/statusbar.go`: Renders a styled player status bar: play/pause icon, track name (truncated), elapsed/total time, volume bar. Updates via `tea.Tick` every 200ms for smooth elapsed time.

### Exit Criteria

1. Player tab shows: "No track playing" when queue is empty
2. Add track from Browse tab (via `enter`) → Player tab shows track info (IA identifier, collection, quality score)
3. `space` toggles play/pause — audio output is audible through system speakers
4. `s` stops playback, returns to "No track playing"
5. `↑`/`↓` adjusts volume — volume bar updates visually, audio volume changes audibly
6. `n` skips to next track in queue (if multiple queued)
7. Elapsed time display updates in real-time during playback
8. Queue list shows current + upcoming tracks; current track is highlighted
9. Changing tabs while playing → audio continues uninterrupted
10. Returning to Player tab → state is preserved (still playing, elapsed time correct)
11. `ctrl+c` during playback → stops audio, clean exit

---

## Implementation Rules

1. **All code in Go** — no Python, no CGo unless absolutely necessary (sqlite-vec may require it)
2. **No shell scripts** — the TUI replaces all orchestration
3. **No external services** — SQLite lives in `data/`, runs entirely local
4. **Configuration via env vars** — `DB_PATH`, `WORKER_CONCURRENCY`, `MAX_STREAM_BYTES`, `THROTTLE_BPS`, `IA_API_RATE`
5. **Graceful shutdown** — SIGINT/SIGTERM releases queue locks, saves cursor, stops audio
6. **Idempotent** — safe to restart at any time
7. **Headless parity** — every feature works identically in `--headless` mode (just without terminal UI)
8. **Phase gate** — do NOT start phase N+1 until user has confirmed phase N's exit criteria are met in the TUI
