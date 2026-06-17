# Phase PX — Per-Track Data Model + Album Art + Browse Redesign

## Date: 2026-06-17

## Architecture Revision 3 — Three-Tier Pipeline

**This section supersedes the Background Goroutines, Control Commands, Event System, and SQLite Database sections in `architecture.md` (Revision 2).**

### Pipeline Overview

The system uses a three-tier pipeline where each tier has distinct scaling characteristics:

```
┌──────────────────────────────────────────────────────────────────┐
│                     SINGLE GO BINARY (bin/timbre)                │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │                  bubbletea TUI (default mode)              │  │
│  │  ┌───────────┐ ┌──────────┐ ┌─────────┐ ┌────────┐       │  │
│  │  │ Dashboard │ │ Live Log │ │ Browse  │ │ Player │       │  │
│  │  │           │ │          │ │ (3-view)│ │        │       │  │
│  │  └───────────┘ └──────────┘ └─────────┘ └────────┘       │  │
│  │           ▲ events channel                                 │  │
│  │           │ control channel                                │  │
│  └───────────┼────────────────────────────────────────────────┘  │
│              │                                                   │
│  ┌───────────┴────────────────────────────────────────────────┐  │
│  │              Three-Tier Background Pipeline                 │  │
│  │                                                             │  │
│  │  ┌──────────────────────────────────────────────────────┐  │  │
│  │  │  TIER 1: Coordinator (singleton)           [s] / [x] │  │  │
│  │  │                                                       │  │  │
│  │  │  IA Scraping API → albums table (status=pending)      │  │  │
│  │  │  Cursor-based pagination, 1000 items/page             │  │  │
│  │  │  Rate: 15 req/min (scrape API)                        │  │  │
│  │  └──────────────────────────────────────────────────────┘  │  │
│  │                         │ albums (pending)                  │  │
│  │                         ▼                                   │  │
│  │  ┌──────────────────────────────────────────────────────┐  │  │
│  │  │  TIER 2: Album Resolvers (pool, N goroutines) [r]/[R]│  │  │
│  │  │                                                       │  │  │
│  │  │  ┌──────┐ ┌──────┐ ┌──────┐                         │  │  │
│  │  │  │  R1  │ │  R2  │ │  R3  │ •••                     │  │  │
│  │  │  └──────┘ └──────┘ └──────┘                         │  │  │
│  │  │                                                       │  │  │
│  │  │  IA Metadata API → parse MP3 files → tracks table     │  │  │
│  │  │  3-tier MP3 filter (VBR/CBR≥192/blacklist)           │  │  │
│  │  │  Title derivation from IA metadata or filename        │  │  │
│  │  │  Rate: 15 req/min (metadata API, shared limiter)      │  │  │
│  │  └──────────────────────────────────────────────────────┘  │  │
│  │                         │ tracks (pending)                  │  │
│  │                         ▼                                   │  │
│  │  ┌──────────────────────────────────────────────────────┐  │  │
│  │  │  TIER 3: Track Analyzers (pool, N goroutines) [w]/[W]│  │  │
│  │  │                                                       │  │  │
│  │  │  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐      │  │  │
│  │  │  │  A1  │ │  A2  │ │  A3  │ │  A4  │ │  A5  │ •••  │  │  │
│  │  │  └──────┘ └──────┘ └──────┘ └──────┘ └──────┘      │  │  │
│  │  │                                                       │  │  │
│  │  │  HTTP download (Range) → MP3 decode → quality gate    │  │  │
│  │  │  → MFCC + Chroma + CLAP → fuse → 564-dim embedding   │  │  │
│  │  │  → track_embeddings table                             │  │  │
│  │  └──────────────────────────────────────────────────────┘  │  │
│  │                                                             │  │
│  │     ActivityEvent channel ──▶ TUI model                     │  │
│  │     ControlCmd channel    ◀── TUI model                     │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                                                                   │
│  ┌────────────────────────────────────────────────┐               │
│  │   SQLite (data/parso_indexer.db)                │               │
│  │                                                 │               │
│  │   albums ──1:N──▶ tracks ──1:1──▶ track_embeddings │            │
│  │   cursor_state (singleton)                      │               │
│  └────────────────────────────────────────────────┘               │
│                                                                   │
│  ┌────────────────────────────────────────────────┐               │
│  │   Art Cache (data/art/*.jpg)                    │               │
│  │   rasterm (iTerm2/Kitty/Sixel)                  │               │
│  └────────────────────────────────────────────────┘               │
└───────────────────────────────────────────────────────────────────┘
                              │
                              │ gRPC (localhost:50051)
                              ▼
┌───────────────────────────────────────────────────────────────────┐
│   PYTHON SIDECAR (optional, separate process)                     │
│   python_sidecar/server.py — CLAP model (laion/clap-htsat-fused) │
└───────────────────────────────────────────────────────────────────┘
```

### Pipeline Tiers

#### Tier 1 — Coordinator (Singleton)

- **What**: Scrapes IA search API to discover albums (IA items)
- **Scaling**: Single goroutine — cursor-based pagination is inherently sequential
- **Controls**: `[s]` start / `[x]` stop
- **Input**: IA Scraping API (`/services/search/v1/scrape`)
- **Output**: `albums` table rows (status=`pending`)
- **Rate limit**: 15 req/min (configurable via `--ia-api-rate`)
- **Resume**: Saves cursor to `cursor_state` table; resumes from last position on restart

#### Tier 2 — Album Resolvers (Pool)

- **What**: Resolves pending albums by fetching IA metadata, enumerating MP3 files, creating track records
- **Scaling**: Pool of N goroutines (typically 2-3). Rate-limited by IA metadata API.
- **Controls**: `[r]` add resolver / `[R]` remove resolver
- **Input**: `albums` table (status=`pending`)
- **Output**: `tracks` table rows (status=`pending`) + album metadata (title, creator, collection, art URL)
- **Rate limit**: 15 req/min (shared limiter across all resolvers)
- **MP3 filtering**: 3-tier — VBR MP3 always accepted; CBR MP3 only if bitrate ≥ 192kbps; 64Kbps/128Kbps blacklisted
- **Title derivation**: Uses IA metadata `title` field when present; derives from filename otherwise

#### Tier 3 — Track Analyzers (Pool)

- **What**: Downloads MP3 audio, decodes to PCM, runs quality gate, extracts features, computes hybrid embedding
- **Scaling**: Pool of N goroutines (typically 4-8). CPU + network bound.
- **Controls**: `[w]` add analyzer / `[W]` remove analyzer
- **Input**: `tracks` table (status=`pending`)
- **Output**: `track_embeddings` table rows (564-dim hybrid vector + quality score)
- **Pipeline per track**:
  1. `StreamAudioFromURL(download_url, maxBytes)` — HTTP Range download
  2. `DecodeMP3(data)` → PCM samples
  3. Quality gate: `CalculateCompositeScore(SNR, SpectralCentroid, CrestFactor)` — reject if < 0.3
  4. `ComputeMFCCPool(samples, sampleRate)` → 40-dim
  5. `ComputeChromaPool(samples)` → 12-dim
  6. `clapClient.GetEmbedding(pcm, sampleRate)` → 512-dim
  7. `FuseFeatures(clap, mfcc, chroma)` → 564-dim hybrid vector
  8. `SaveEmbedding(trackID, vector, qualityScore)`
  9. `MarkTrackCompleted(trackID)`

### Control Commands

```go
type ControlAction string
const (
    CmdStartCoordinator ControlAction = "start_coordinator"   // [s] — starts Tier 1 + all Tier 2 resolvers
    CmdStopCoordinator  ControlAction = "stop_coordinator"    // [x] — stops Tier 1 + all Tier 2 resolvers
    CmdAddResolver      ControlAction = "add_resolver"        // [r] — add one album resolver goroutine
    CmdRemoveResolver   ControlAction = "remove_resolver"     // [R] — remove one album resolver goroutine
    CmdAddWorker        ControlAction = "add_worker"          // [w] — add one track analyzer goroutine
    CmdRemoveWorker     ControlAction = "remove_worker"       // [W] — remove one track analyzer goroutine
    CmdShutdown         ControlAction = "shutdown"            // ctrl+c — stop everything, save state, exit
)
```

### Event Types

```go
const (
    // Coordinator events
    EventQueueAdded       = "queue_added"           // album discovered
    EventCoordStarted     = "coordinator_started"
    EventCoordStopped     = "coordinator_stopped"
    EventCoordProgress    = "coordinator_progress"

    // Resolver events
    EventAlbumResolving   = "album_resolving"       // resolver started on album
    EventAlbumResolved    = "album_resolved"         // album metadata fetched, tracks created
    EventAlbumFailed      = "album_failed"           // metadata fetch failed

    // Analyzer events
    EventAnalysisStarted  = "analysis_started"
    EventAnalysisComplete = "analysis_complete"
    EventAnalysisFailed   = "analysis_failed"

    // Pool events
    EventWorkerStarted    = "worker_started"         // resolver or analyzer started
    EventWorkerStopped    = "worker_stopped"          // resolver or analyzer stopped

    // System
    EventInfo             = "info"
)
```

### Database Schema

```sql
-- Tier 1 output: discovered IA items
CREATE TABLE albums (
    ia_identifier TEXT PRIMARY KEY,
    title         TEXT,
    creator       TEXT,
    collection    TEXT,
    art_url       TEXT,
    track_count   INTEGER NOT NULL DEFAULT 0,
    status        TEXT CHECK(status IN ('pending','resolving','resolved','failed')),
    ...
);

-- Tier 2 output: individual audio files
CREATE TABLE tracks (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    album_id      TEXT REFERENCES albums(ia_identifier),
    filename      TEXT,
    title         TEXT,
    track_number  INTEGER,
    format        TEXT,       -- "VBR MP3", "MP3", etc.
    bitrate       INTEGER,
    download_url  TEXT,
    status        TEXT CHECK(status IN ('pending','processing','completed','failed')),
    UNIQUE(album_id, filename)
);

-- Tier 3 output: feature embeddings
CREATE TABLE track_embeddings (
    track_id      INTEGER PRIMARY KEY REFERENCES tracks(id),
    embedding     BLOB,       -- 564-dim float32 hybrid vector
    quality_score REAL
);

-- Coordinator resume state
CREATE TABLE cursor_state (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    last_cursor TEXT,
    items_indexed INTEGER
);
```

### Dashboard Layout

```
Dashboard

┌─ Albums ──────────┐  ┌─ Tracks ──────────┐
│ Total:         42 │  │ Total:         312 │
│ Pending:       30 │  │ Pending:       200 │
│ Resolving:      2 │  │ Processing:      4 │
│ Resolved:       8 │  │ Completed:      96 │
│ Failed:         2 │  │ Failed:         12 │
└───────────────────┘  └────────────────────┘

Coordinator                          
  Status: ▶ Running                  
  Indexed: 3000  |  Cursor: abc123...
  [s] start  [x] stop               

Resolver Pool                        
  Resolvers: 2                       
    resolver-1: resolving: etree:gd1977...
    resolver-2: resolving: georgeblood:v...
  [r] add  [R] remove                

Analyzer Pool                        
  Analyzers: 4                       
    analyzer-1: analyzing: Dark Star     
    analyzer-2: 12 ok / 1 fail           
  [w] add  [W] remove                

Activity Feed
  12:00:05  ✔ [analyzer-1] Complete Dark Star (quality: 0.85)
  12:00:04  ☑ [resolver-1] Resolved etree:gd1977: "Grateful Dead" — 12 tracks
  12:00:03  + etree:gd1977-05-08  (album added)
```

### Browse Tab (3 Views)

- **Albums view** (default): Table of resolved albums. Art preview for selected album. `[enter]` drill into album. `[m]` switch to tracks.
- **Album detail**: Album header with art + track list. `[enter/p]` play track. `[esc]` back.
- **Tracks view**: Flat list of completed tracks across all albums. `[a]` view album. `[m]` switch to albums.

---

## Design Decisions

1. **Track titles**: Store IA metadata `title` if present; derive from filename when missing.
2. **MP3 format filtering (3-tier)**: VBR auto-accept; CBR ≥192; blacklist 64/128Kbps.
3. **Album art**: Essential. rasterm for terminal images. Cache to `data/art/`. Display in Browse + Player.
4. **IA `creator` field**: Join with ", " if array.
5. **Coordinator owns resolution**: Album resolvers run alongside the coordinator (started/stopped together with `s`/`x`), not as part of the worker pool. This ensures tracks appear in the Browse tab as soon as the coordinator is running.
6. **Three-tier pipeline**: Each tier scales independently. Coordinator is singleton. Resolvers and analyzers are pools.

## Exit Criteria (Phase PX)

1. `albums` table stores IA items with title, creator, collection, art_url
2. `tracks` table stores individual MP3 files per 3-tier filter
3. Coordinator discovers albums; resolver pool resolves them into tracks; analyzer pool processes tracks
4. Dashboard shows album + track stats, coordinator status, resolver pool, analyzer pool
5. `[s/x]` start/stop coordinator + resolvers; `[r/R]` add/remove resolvers; `[w/W]` add/remove analyzers
6. Browse has three views: Albums, Album detail, Tracks
7. Album art renders inline in supported terminals
8. All tests pass, make build clean
