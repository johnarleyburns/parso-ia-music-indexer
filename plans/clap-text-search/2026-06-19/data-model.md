# Data Model

## Current Schema: `track_embeddings`

```sql
CREATE TABLE IF NOT EXISTS track_embeddings (
    track_id      INTEGER PRIMARY KEY REFERENCES tracks(id),
    embedding     BLOB NOT NULL,          -- 564-dim f32, 2256 bytes
    quality_score REAL,
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
```

- Single fused vector: `[clap*0.60 (512) | mfcc*0.25 (40) | chroma*0.15 (12)]`
- Encoding: little-endian IEEE-754 float32, 4 bytes/dim, 2256 bytes total.
- No provenance metadata (model version, dtype, dimensions).

## Target Schema: `track_embeddings_v2`

```sql
CREATE TABLE IF NOT EXISTS track_embeddings_v2 (
    track_id       INTEGER PRIMARY KEY REFERENCES tracks(id),
    clap           BLOB NOT NULL,         -- 512-dim f16, L2-normalized, 1024 bytes
    mfcc           BLOB NOT NULL,         -- 40-dim f16, 80 bytes
    chroma         BLOB NOT NULL,         -- 12-dim f16, 24 bytes
    model_version  TEXT NOT NULL DEFAULT 'clap-htsat-fused:audio+text:512:l2:f16',
    dim            INTEGER NOT NULL DEFAULT 512,
    dtype          TEXT NOT NULL DEFAULT 'f16',
    quality_score  REAL,
    created_at     TEXT NOT NULL DEFAULT (datetime('now'))
);
```

### Column Details

| Column          | Type    | Description |
|-----------------|---------|-------------|
| `track_id`      | INTEGER | PK, FK -> tracks.id |
| `clap`          | BLOB    | 512-dim f16 L2-normalized CLAP embedding (1024 bytes) |
| `mfcc`          | BLOB    | 40-dim f16 MFCC pool vector (80 bytes) |
| `chroma`        | BLOB    | 12-dim f16 chroma pool vector (24 bytes) |
| `model_version` | TEXT    | Provenance string: `clap-htsat-fused:audio+text:512:l2:f16` |
| `dim`           | INTEGER | Primary embedding dimension (512 for CLAP) |
| `dtype`         | TEXT    | Storage dtype: `f16` |
| `quality_score` | REAL    | Audio quality composite score |
| `created_at`    | TEXT    | ISO timestamp |

### Storage Comparison

| Schema | Bytes/Track | Notes |
|--------|-------------|-------|
| v1 (current) | 2256 | 564 * 4 bytes (f32) |
| v2 (target)  | 1128 | (512+40+12) * 2 bytes (f16) |
| Savings      | 50%  | |

### model_version Convention

Format: `{checkpoint}:{towers}:{dim}:{norm}:{dtype}`

Example: `clap-htsat-fused:audio+text:512:l2:f16`

This tells the iOS app exactly what it has: which model, what dimensionality,
that it's L2-normalized (so dot product = cosine similarity), and what dtype
to decode.

## Backfill Strategy

Existing rows in `track_embeddings` hold a 564-dim f32 hybrid:
```
hybrid[0:512]   = clap_original * 0.60
hybrid[512:552] = mfcc_original * 0.25
hybrid[552:564] = chroma_original * 0.15
```

Recovery:
1. `clap_recovered = hybrid[0:512] / 0.60` -> then L2-normalize.
   Since 0.60 is a uniform scalar, `clap_recovered / ||clap_recovered||`
   gives the same unit vector as `clap_original / ||clap_original||`.
   The CLAP direction is perfectly preserved.
2. `mfcc_recovered = hybrid[512:552] / 0.25` -> exact original values.
3. `chroma_recovered = hybrid[552:564] / 0.15` -> exact original values.

Validation: for a sample of tracks where we can re-run CLAP inference,
compare the backfilled vs. fresh-computed vectors. Cosine distance should
be < 1e-4 (f16 quantization tolerance).

## iOS App Query Patterns

### Text-to-Audio Search
```sql
SELECT e.track_id, e.clap, t.title, a.title, a.ia_identifier, t.download_url
FROM track_embeddings_v2 e
INNER JOIN tracks t ON e.track_id = t.id
INNER JOIN albums a ON t.album_id = a.ia_identifier
WHERE t.status = 'completed'
```
App decodes each `clap` f16 blob to f32, dot-products with the query vector,
sorts by descending similarity.

### Track-to-Track Similarity
Same query but the app loads all three component blobs, applies FuseFeatures
weights, and cosine-ranks against a reference track's fused vector.

## Deferred: `track_segments` (D-bonus)

```sql
CREATE TABLE IF NOT EXISTS track_segments (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    track_id   INTEGER NOT NULL REFERENCES tracks(id),
    start_sec  REAL NOT NULL,
    dur_sec    REAL NOT NULL,
    clap       BLOB NOT NULL,    -- 512-dim f16, L2-normalized
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_track_segments_track ON track_segments(track_id);
```

Populated only when `track.duration > segment_threshold` (configurable,
e.g. 300s). Each segment is a 30s window with its own CLAP embedding.
Text queries can rank moments and deep-link to an offset. NOT implemented
until core phases land and are approved.

## LibriVox Denylist

`data/librivox_denylist.json`:
```json
[
  "librivox_recording_of_some_title",
  "another_librivox_identifier"
]
```

Simple JSON array of IA identifiers. Loaded at coordinator startup.
Matched by exact identifier string. Trivially extensible (one entry per line
in the JSON array).
