# Architecture (Revision 3 — Three-Tier Pipeline, Per-Track Model)

**Supersedes**: Revision 2 (TUI-Based, Scriptless) — June 16, 2026

## Problem

We need a local-first audio indexing pipeline that discovers IA music **albums**, resolves them into individual **tracks**, streams ~30s of each track, extracts ML features, stores vector embeddings, and supports browsing/playback — all from a single terminal application.

## High-Level Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                     SINGLE GO BINARY (bin/timbre)                │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │                  bubbletea TUI (default mode)              │  │
│  │  ┌───────────┐ ┌──────────┐ ┌─────────┐ ┌────────┐       │  │
│  │  │ Dashboard │ │ Live Log │ │ Browse  │ │ Player │       │  │
│  │  │ (stats +  │ │ (scroll- │ │ (3-view │ │ (Phase │       │  │
│  │  │ controls) │ │  able)   │ │ + art)  │ │  7)    │       │  │
│  │  └───────────┘ └──────────┘ └─────────┘ └────────┘       │  │
│  └────────────────────────────────────────────────────────────┘  │
│                          ▲ events / ▼ controls                   │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │              Three-Tier Background Pipeline                 │  │
│  │                                                             │  │
│  │  TIER 1: Coordinator (singleton)                [s] / [x]  │  │
│  │  IA Scraping API → albums table (status=pending)            │  │
│  │  Cursor-based, 1000 items/page, 15 req/min                 │  │
│  │                         │                                   │  │
│  │                         ▼ albums (pending)                  │  │
│  │  TIER 2: Album Resolvers (pool)                 [r] / [R]  │  │
│  │  ┌──────┐ ┌──────┐ ┌──────┐                                │  │
│  │  │  R1  │ │  R2  │ │  R3  │ •••                            │  │
│  │  └──────┘ └──────┘ └──────┘                                │  │
│  │  IA Metadata API → parse MP3s → tracks table                │  │
│  │  3-tier MP3 filter, title derivation, 15 req/min            │  │
│  │                         │                                   │  │
│  │                         ▼ tracks (pending)                  │  │
│  │  TIER 3: Track Analyzers (pool)                 [w] / [W]  │  │
│  │  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐             │  │
│  │  │  A1  │ │  A2  │ │  A3  │ │  A4  │ │  A5  │ •••         │  │
│  │  └──────┘ └──────┘ └──────┘ └──────┘ └──────┘             │  │
│  │  HTTP Range download → MP3 decode → quality gate            │  │
│  │  → MFCC + Chroma + CLAP → fuse → track_embeddings          │  │
│  │                                                             │  │
│  │     ActivityEvent channel ──▶ TUI model                     │  │
│  │     ControlCmd channel    ◀── TUI model                     │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                                                                   │
│  ┌────────────────────────────────────────┐  ┌─────────────────┐ │
│  │   SQLite (data/parso_indexer.db)        │  │ Art Cache        │ │
│  │   albums → tracks → track_embeddings   │  │ data/art/*.jpg  │ │
│  │   cursor_state                          │  │ rasterm render  │ │
│  └────────────────────────────────────────┘  └─────────────────┘ │
│                                                                   │
│  ┌────────────────────────────────────────────────────────────┐   │
│  │  Headless Mode (--headless)                                │   │
│  │  Same pipeline, structured JSON to stdout, SIGINT shutdown │   │
│  └────────────────────────────────────────────────────────────┘   │
└───────────────────────────────────────────────────────────────────┘
                              │ gRPC (localhost:50051)
                              ▼
┌───────────────────────────────────────────────────────────────────┐
│   PYTHON SIDECAR (optional) — CLAP model (laion/clap-htsat-fused)│
└───────────────────────────────────────────────────────────────────┘
```

## Pipeline Tiers

### Tier 1 — Coordinator (Singleton)

- **Purpose**: Discover IA items (albums) via the Scraping API
- **Scaling**: Single goroutine — cursor-based pagination is sequential
- **Controls**: `[s]` start / `[x]` stop (also starts/stops Tier 2 resolvers)
- **Input**: IA Scraping API (`/services/search/v1/scrape`)
- **Output**: `albums` table rows (status=`pending`)
- **Rate**: 15 req/min (own limiter)
- **Resume**: Saves cursor to `cursor_state`; resumes on restart

### Tier 2 — Album Resolvers (Pool)

- **Purpose**: Fetch album metadata, enumerate MP3 files, create track records
- **Scaling**: Pool of N goroutines (typically 2-3)
- **Controls**: `[r]` add / `[R]` remove
- **Input**: `albums` table (status=`pending`)
- **Output**: `tracks` table rows (status=`pending`) + album metadata
- **Rate**: 15 req/min (shared limiter across all resolvers)
- **MP3 filter**: VBR MP3 always; CBR ≥192kbps; blacklist 64/128Kbps
- **Title**: IA metadata `title` field, or derived from filename

### Tier 3 — Track Analyzers (Pool)

- **Purpose**: Download, decode, quality-gate, extract features, store embeddings
- **Scaling**: Pool of N goroutines (typically 4-8)
- **Controls**: `[w]` add / `[W]` remove
- **Input**: `tracks` table (status=`pending`)
- **Output**: `track_embeddings` table rows
- **Pipeline per track**:
  1. `StreamAudioFromURL(download_url, maxBytes)` — HTTP Range
  2. `DecodeMP3` → PCM samples
  3. Quality gate: SNR + Centroid + Crest → composite score ≥ 0.3
  4. MFCC (40-dim) + Chroma (12-dim) + CLAP (512-dim)
  5. `FuseFeatures` → 564-dim hybrid vector
  6. `SaveEmbedding(trackID, vector, quality)`

## Control Commands

```go
CmdStartCoordinator  // [s] start Tier 1 coordinator
CmdStopCoordinator   // [x] stop Tier 1 coordinator
CmdAddResolver       // [r] add one album resolver
CmdRemoveResolver    // [R] remove one album resolver
CmdAddWorker         // [w] add one track analyzer
CmdRemoveWorker      // [W] remove one track analyzer
CmdShutdown          // ctrl+c — stop everything, save state, exit
```

## Event Types

```go
EventQueueAdded       // album discovered by coordinator
EventCoordStarted/Stopped/Progress
EventAlbumResolving   // resolver working on album
EventAlbumResolved    // album metadata fetched, tracks created
EventAlbumFailed      // metadata fetch failed
EventAnalysisStarted  // analyzer working on track
EventAnalysisComplete // track embedding saved
EventAnalysisFailed   // track analysis failed
EventWorkerStarted/Stopped
EventInfo
```

## Database Schema

```sql
CREATE TABLE albums (
    ia_identifier TEXT PRIMARY KEY,
    title TEXT, creator TEXT, collection TEXT, art_url TEXT,
    track_count INTEGER DEFAULT 0,
    status TEXT CHECK(status IN ('pending','resolving','resolved','failed')),
    error_message TEXT, created_at TEXT, updated_at TEXT
);

CREATE TABLE tracks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    album_id TEXT REFERENCES albums(ia_identifier),
    filename TEXT, title TEXT, track_number INTEGER,
    format TEXT, bitrate INTEGER, download_url TEXT,
    status TEXT CHECK(status IN ('pending','processing','completed','failed')),
    worker_id TEXT, locked_at TEXT, retry_count INTEGER DEFAULT 0,
    error_message TEXT, created_at TEXT, updated_at TEXT,
    UNIQUE(album_id, filename)
);

CREATE TABLE track_embeddings (
    track_id INTEGER PRIMARY KEY REFERENCES tracks(id),
    embedding BLOB,        -- 564-dim float32 (2256 bytes)
    quality_score REAL, created_at TEXT
);

CREATE TABLE cursor_state (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    last_cursor TEXT, items_indexed INTEGER DEFAULT 0, last_run_at TEXT
);
```

## TUI Tabs

### Dashboard
- Dual stats: Albums (total/pending/resolving/resolved/failed) + Tracks (total/pending/processing/completed/failed)
- Coordinator status, Resolver pool, Analyzer pool sections
- Activity feed (last 20 events, color-coded)
- Controls: `[s/x]` coordinator, `[r/R]` resolvers, `[w/W]` analyzers

### Live Log
- Full-screen scrollable event viewport, auto-scroll toggle `[S]`

### Browse (3 views)
- **Albums view**: Table of resolved albums, art preview for selected, `[enter]` detail, `[m]` toggle
- **Album detail**: Album art + header + track list, `[enter/p]` play, `[esc]` back
- **Tracks view**: Flat list, `[a]` view album, `[v]` similar, `[m]` toggle

### Player (Phase 7)
- Playback controls, queue, volume, seek, elapsed time

## Dependencies

| Package | Purpose |
|---|---|
| `charm.land/bubbletea/v2` | TUI framework |
| `charm.land/bubbles/v2` | TUI components |
| `charm.land/lipgloss/v2` | Terminal styling |
| `github.com/mattn/go-sqlite3` | SQLite driver |
| `github.com/hajimehoshi/go-mp3` | MP3 decoder |
| `github.com/zrma/go-mfcc` | MFCC extraction |
| `github.com/mjibson/go-dsp` | FFT for chroma |
| `github.com/go-audio/audio`, `wav` | WAV test fixtures |
| `github.com/BourgeoisBear/rasterm` | Terminal image rendering |
| `golang.org/x/image` | Image resizing |
| `golang.org/x/time/rate` | Rate limiting |
| `google.golang.org/grpc`, `protobuf` | CLAP sidecar communication |

## Operation Modes

- **TUI mode** (default): Full interactive terminal app
- **Headless mode** (`--headless`): JSON event logging to stdout, SIGINT shutdown
