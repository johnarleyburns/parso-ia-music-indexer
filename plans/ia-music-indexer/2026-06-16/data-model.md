# Data Model

## Problem

We need a local SQLite database that can:
1. Track the discovery state of millions of IA audio tracks across distributed workers
2. Store 40-dimensional float32 vector embeddings with vector search capability
3. Survive worker crashes without losing or duplicating work
4. Stay under ~1 GB for 4 million tracks

## Current Behavior

No database exists. Starting from scratch.

## Research Findings

### sqlite-vec

`sqlite-vec` is a SQLite extension that adds vector search. Key facts:
- Loaded as a runtime extension (`.load('vec0')` or auto-loaded)
- Creates virtual tables for vector storage
- Supports cosine similarity, L2 distance
- Stores vectors as compact BLOBs (float32 arrays)
- Works entirely in-process, no separate server

### Queue State Machine

```
                  ┌──────────┐
                  │  pending  │
                  └─────┬─────┘
                        │ ClaimNextBatch()
                  ┌─────▼─────┐
          ┌───────│ processing │───────┐
          │       └─────┬─────┘       │
          │  ResetStuck  │             │
          │  (5min+)     │             │
          │       ┌──────┘             │
          │  ┌────▼────┐    ┌─────────▼──┐
          │  │ pending  │    │  completed  │
          │  └─────────┘    └────────────┘
          │
          │  MarkFailed()
          │
          └──────►┌────────┐
                  │ failed  │
                  └────────┘
```

## Design Proposal

### Table: `catalog_queue`

Tracks the discovery and processing state of every IA audio item.

```sql
CREATE TABLE catalog_queue (
    ia_identifier  TEXT PRIMARY KEY,    -- IA item ID (e.g., "gd1977-05-08.sbd.miller.12345")
    status         TEXT NOT NULL DEFAULT 'pending'
                   CHECK(status IN ('pending','processing','completed','failed')),
    worker_id      TEXT,                -- Which worker claimed this
    locked_at      TEXT,                -- ISO 8601 timestamp when claimed
    retry_count    INTEGER NOT NULL DEFAULT 0,
    error_message  TEXT,                -- Last error if failed
    created_at     TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at     TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_catalog_queue_status ON catalog_queue(status);
CREATE INDEX idx_catalog_queue_locked ON catalog_queue(status, locked_at);
```

### Table: `track_embeddings`

Stores the extracted audio fingerprint vectors.

```sql
-- Requires sqlite-vec extension loaded
CREATE VIRTUAL TABLE track_embeddings USING vec0(
    ia_identifier  TEXT PRIMARY KEY,
    mfcc_embedding FLOAT[40],           -- 40-dim vector: mean(20) + variance(20)
    quality_score  REAL,                -- SNR-based quality metric (higher = cleaner)
    audio_length   REAL,                -- Actual seconds analyzed (for debugging)
    created_at     TEXT NOT NULL DEFAULT (datetime('now'))
);
```

The `vec0` virtual table handles the vector index automatically. Queries use:

```sql
SELECT ia_identifier, distance
FROM track_embeddings
WHERE mfcc_embedding MATCH ?
ORDER BY distance
LIMIT 10;
```

Where `distance` is cosine distance by default.

### Table: `cursor_state`

Stores coordinator resume state.

```sql
CREATE TABLE cursor_state (
    id             INTEGER PRIMARY KEY CHECK(id = 1),  -- Singleton row
    last_cursor    TEXT,                -- IA Scraping API cursor for resume
    items_indexed  INTEGER NOT NULL DEFAULT 0,
    last_run_at    TEXT,
    query_string   TEXT                 -- The query being paginated
);
```

This table serves as the authoritative resume cursor. Also backed by a local text file for extra safety (`data/cursor.txt`).

### Vector Format Details

#### MFCC Embedding (40 dimensions)

```
float32[0..19]  → Mean of each MFCC band across all time frames
float32[20..39] → Variance of each MFCC band across all time frames
```

Example: For 20 MFCC bands × N time frames:
- band 0 mean = mean(frame[0], frame[1], ..., frame[N-1]) for band 0
- band 0 var  = variance(frame[0], frame[1], ..., frame[N-1]) for band 0
- ...
- band 19 mean
- band 19 var

Total: 40 float32 values = 160 bytes raw.

#### Quality Score (SNR)

Signal-to-Noise Ratio estimated from the PCM samples:

```
signal_power = mean(sample²) for all samples
noise_floor  = 10th percentile of frame-level RMS energy
snr_db       = 10 * log10(signal_power / noise_floor)
```

Higher SNR = cleaner recording. 78rpm records typically score <10 dB; modern recordings score >30 dB.

### Database File Layout

```
data/
├── parso_indexer.db          # SQLite database (single file, ~1 GB at 4M tracks)
├── cursor.txt                # Coordinator resume cursor backup
└── logs/
    ├── coordinator.log
    └── worker.log
```

### Migration Strategy

Go module will run migrations on startup:
1. Load sqlite-vec extension
2. Create `catalog_queue` table if not exists
3. Create `track_embeddings` virtual table if not exists
4. Create `cursor_state` table if not exists
5. Create indexes
6. Idempotent — safe to run multiple times

## Open Questions

1. **sqlite-vec vs sqlite-vss**: `sqlite-vec` is the newer replacement for `sqlite-vss`. Need to confirm it's production-ready and has proper Go bindings.

2. **Vector index rebuild**: Does `sqlite-vec` require manual index rebuild after bulk inserts, or does it maintain the index incrementally?

3. **Concurrent writes**: SQLite allows multiple readers but only one writer at a time. How do we handle multiple workers writing simultaneously? WAL mode + retry logic? Or serialize writes through a single DB connection?

4. **Quality score normalization**: Should we normalize SNR to a 0–1 scale for easier filtering in the iOS app?
