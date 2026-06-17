# Implementation Plan (Revision 2 — TUI-Based, Scriptless)

**STATUS**: Phases 1–6 COMPLETE. Phase PX (per-track model, three-tier pipeline) COMPLETE. Phase 7 PENDING.
**NOTE**: This document describes the original phase plan. The pipeline architecture has been significantly restructured by Phase PX. See `architecture.md` (Revision 3), `data-model.md` (Revision 2), and `phase-px-per-track.md` for current architecture. Key changes: `catalog_queue` replaced by `albums → tracks → track_embeddings`; two-pool model replaced by three-tier pipeline (coordinator + resolvers + analyzers).

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

## Phase 2B — Hybrid Recommendation Engine Core

**Goal**: Implement the core computation engines: three vector extractors (MFCC, Chroma, CLAP gRPC client), a multi-metric quality scoring gatekeeper (SNR + Spectral Centroid + Crest Factor), and the late fusion engine that combines vectors into a 564-dim hybrid embedding. Everything is testable with unit tests. The database already supports arbitrary-length vectors via BLOB storage — no schema migration needed.

Full details: see `02b-hybrid-engine.md`.

### Files to Create

```
internal/audio/types.go          # Shared audio types (PCMFormat, FeatureResult, QualityScore)
internal/audio/decode.go         # WAV decoder for test fixtures
internal/audio/mfcc.go           # MFCC extraction → 40-dim vector (20 mean + 20 variance)
internal/audio/chroma.go         # Chroma extraction → 12-dim vector (12 semitones)
internal/audio/quality.go        # Quality scoring: SNR, Spectral Centroid, Crest Factor → 0.0–1.0
internal/hybrid/fusion.go        # FuseFeatures() — weighted concatenation → 564-dim
internal/hybrid/fusion_test.go   # Unit tests for fusion engine
internal/clap/client.go          # CLAPClient interface + mock + gRPC implementation
proto/clap.proto                 # gRPC service + message definitions
python_sidecar/server.py         # Python CLAP inference server (standalone)
python_sidecar/requirements.txt  # Python dependencies
```

### Files to Modify

```
internal/db/db_test.go           # Add 564-dim embedding roundtrip + similarity tests
internal/config/config.go        # Add ClapHost, ClapPort fields
go.mod                           # Add gRPC, proto, go-dsp, go-mfcc, go-audio deps
```

### Dependencies to Add

```
google.golang.org/grpc
google.golang.org/protobuf
github.com/mjibson/go-dsp
github.com/zrma/go-mfcc
github.com/go-audio/audio
github.com/go-audio/wav
```

### Key Implementation Details

- `internal/audio/mfcc.go`: `ComputeMFCCPool(samples []float32, sampleRate int) []float32` — uses go-mfcc, 20 bands, returns 40-dim (mean 0..19, variance 20..39).
- `internal/audio/chroma.go`: `ComputeChromaPool(samples []float32) []float32` — FFT → frequency→MIDI mapping → 12-bin pitch histogram → 12-dim vector.
- `internal/audio/quality.go`: Three raw metric functions — `CalculateSNR(samples) → dB`, `CalculateSpectralCentroid(samples, sampleRate) → Hz`, `CalculateCrestFactor(samples) → ratio`. Composite: `CalculateCompositeScore(snr, centroid, crest) → 0.0–1.0`. Weights: SNR 0.50, Centroid 0.30, Crest 0.20. Kill switch: SNR < 10 dB → 0.0. Normalization: min-max clamping with fixed reference ranges.
- `internal/audio/decode.go`: `DecodeWav(filePath string) ([]float32, error)` — for test fixtures only.
- `internal/hybrid/fusion.go`: `FuseFeatures(clap, mfcc, chroma []float32) []float32` — applies weights (0.60, 0.25, 0.15), concatenates → 564-dim.
- `internal/clap/client.go`: `CLAPClient` interface with `GetEmbedding()`, `HealthCheck()`, `Close()`. `NewGRPCClient(host, port)` for real gRPC, `NewMockClient()` for testing.
- `proto/clap.proto`: `CLAPEmbedder` service with `GetEmbedding` RPC. `EmbeddingRequest` (pcm_data bytes, sample_rate int32), `EmbeddingResponse` (repeated float embedding).
- `python_sidecar/server.py`: HuggingFace `laion/clap-htsat-fused`, MPS/CUDA/CPU, gRPC on port 50051.
- `internal/config/config.go`: Add `ClapHost` (env `CLAP_HOST`, default `localhost`) and `ClapPort` (env `CLAP_PORT`, default `50051`).
- DB: Existing BLOB storage + pure Go cosine distance in `embeddings.go` already handles 564-dim. No code changes to `embeddings.go` or `db.go` needed.

### Exit Criteria

1. `go test ./internal/audio/...` — MFCC, Chroma, and Quality scoring extractors produce correct output
2. `go test ./internal/hybrid/...` — Fusion produces correct 564-dim output with weights
3. `go test ./internal/clap/...` — Mock client works; gRPC client struct compiles
4. `go test ./internal/db/...` — Existing 10 tests pass + new 564-dim tests pass
5. `go build ./cmd/tui` compiles without errors
6. `proto/clap.proto` is complete with documented generation commands
7. `python_sidecar/server.py` is present and well-documented (manual launch optional at this phase)
8. Quality scoring: sine wave > 0.7, white noise < 0.3, SNR < 10 dB kill switch triggers

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

## Phase 5 — Audio Analysis Pipeline + Real Worker Pool (Hybrid)

**Goal**: Workers stream real IA audio, decode to PCM, run quality gate (skip unusable tracks), extract all three feature vectors (MFCC, Chroma, CLAP), fuse into a 564-dim hybrid vector, and store in DB. Progress visible in Dashboard.

### Files to Create

```
internal/audio/stream.go            # HTTP Range download with throttling
internal/audio/mp3decode.go         # MP3 → PCM samples via go-mp3
internal/audio/types.go             # (MODIFIED: AnalysisResult includes hybrid vector + quality)
internal/rate/throttled_reader.go   # io.Reader wrapper: 450 KB/s limit
```

### Files to Modify

```
internal/tui/dashboard.go           # Wire real worker start/stop; per-worker progress bars
internal/tui/events.go              # Add AnalysisStarted, AnalysisComplete, AnalysisFailed events
cmd/tui/main.go                     # Wire real worker goroutines (replaces stubs)
internal/db/embeddings.go           # (No change needed — already handles 564-dim BLOBs)
internal/ia/client.go               # Add LookupMP3URL(identifier) method
```

### Dependencies to Add

```
github.com/hajimehoshi/go-mp3
github.com/zrma/go-mfcc              # (Already added in Phase 2B)
golang.org/x/time/rate
```

### Key Implementation Details

- Worker goroutine pipeline (MODIFIED from original Phase 5):
  1. `ClaimNextBatch(db, workerID, batchSize)` (unchanged)
  2. For each identifier:
     a. `LookupMP3URL` → HTTP Range download → PCM samples (unchanged)
     b. **Quality gate**: `CalculateSNR` + `CalculateSpectralCentroid` + `CalculateCrestFactor` → `CalculateCompositeScore` (uses engine from Phase 2B)
        - If composite < 0.3: `MarkFailed(identifier, "low quality: score=X.XX")`, skip remainder
        - If composite >= 0.3: continue
     c. `ComputeMFCCPool(samples, sampleRate)` → 40-dim MFCC (uses engine from Phase 2B)
     d. `ComputeChromaPool(samples)` → 12-dim Chroma (uses engine from Phase 2B)
     e. `clapClient.GetEmbedding(ctx, pcmBytes, sampleRate)` → 512-dim CLAP (uses engine from Phase 2B)
     f. `FuseFeatures(clap, mfcc, chroma)` → 564-dim hybrid vector (uses fusion from Phase 2B)
     g. `SaveEmbedding(db, identifier, hybridVector, qualityScore)` (uses existing DB layer)
     h. `MarkCompleted(identifier)` (unchanged)
  3. Send `EventAnalysisStarted` / `EventAnalysisComplete` / `EventAnalysisFailed` events

- CLAP sidecar communication: Go worker calls gRPC client (built in Phase 2B). If sidecar is unreachable, worker uses mock CLAP client as fallback with degraded recommendation quality (configurable — `CLAP_FALLBACK_MOCK=true`).

- Quality scoring is computed **before** CLAP (which is expensive), so unusable tracks are rejected early without wasting GPU time.

### Exit Criteria

Same as original Phase 5 exit criteria, plus:
- Verified: hybrid vectors stored are 564-dimensional
- Verified: similarity search on hybrid vectors returns meaningful results
- Verified: MFCC + Chroma extraction happen locally (no Python dependency for these)
- Verified: CLAP extraction succeeds when Python sidecar is running; falls back to mock when not
- Verified: low-quality tracks (composite < 0.3) are marked as failed with "low quality" error; high-quality tracks proceed normally
- Verified: SNR < 10 dB kill switch marks tracks as failed regardless of other metrics

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

1. **All code in Go** — no Python, no CGo unless absolutely necessary
2. **No shell scripts** — the TUI replaces all orchestration
3. **No external services** — SQLite lives in `data/`, runs entirely local
4. **Configuration via env vars** — `DB_PATH`, `WORKER_CONCURRENCY`, `MAX_STREAM_BYTES`, `THROTTLE_BPS`, `IA_API_RATE`, `CLAP_HOST`, `CLAP_PORT`
5. **Graceful shutdown** — SIGINT/SIGTERM releases queue locks, saves cursor, stops audio
6. **Idempotent** — safe to restart at any time
7. **Headless parity** — every feature works identically in `--headless` mode (just without terminal UI)
8. **Phase gate** — do NOT start phase N+1 until user has confirmed phase N's exit criteria are met in the TUI
9. **Binary first** — always build via `make build` and run tests before asking the user to verify. Never say `go run`.
10. **Binary is deliverable** — `bin/timbre` is the single binary; the user runs `./bin/timbre`, never `go run ./cmd/tui`.
