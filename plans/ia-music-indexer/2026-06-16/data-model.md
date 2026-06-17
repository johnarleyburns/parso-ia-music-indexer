# Data Model (Revision 2 — Per-Track, Albums + Tracks)

**Supersedes**: Revision 1 (per-item catalog_queue) — June 16, 2026

## Overview

The system uses a three-table data model that reflects the IA hierarchy: IA items (albums) contain multiple audio files (tracks). Each track gets its own embedding.

## Tables

### `albums` — IA Items (Discovered by Coordinator, Resolved by Resolvers)

| Column | Type | Description |
|---|---|---|
| `ia_identifier` | TEXT PK | IA item identifier (e.g., `gd1977-05-08.sbd.miller.12345`) |
| `title` | TEXT | Album title from IA metadata |
| `creator` | TEXT | Artist/creator (joined with ", " if array) |
| `collection` | TEXT | IA collection (joined with ", " if array) |
| `art_url` | TEXT | `https://archive.org/services/img/{identifier}` |
| `track_count` | INTEGER | Number of qualifying MP3 tracks found |
| `status` | TEXT | `pending` → `resolving` → `resolved` / `failed` |
| `error_message` | TEXT | Error details if failed |
| `created_at` | TEXT | ISO 8601 timestamp |
| `updated_at` | TEXT | ISO 8601 timestamp |

**Status transitions:**
```
pending → resolving → resolved
                    → failed
```

### `tracks` — Individual Audio Files (Created by Resolvers, Analyzed by Analyzers)

| Column | Type | Description |
|---|---|---|
| `id` | INTEGER PK AUTO | Track ID |
| `album_id` | TEXT FK | References `albums.ia_identifier` |
| `filename` | TEXT | MP3 filename within IA item |
| `title` | TEXT | Track title (from IA metadata or derived from filename) |
| `track_number` | INTEGER | Position in album (parsed from IA `track` field) |
| `format` | TEXT | `VBR MP3`, `MP3`, etc. |
| `bitrate` | INTEGER | kbps for CBR files |
| `download_url` | TEXT | Full download URL |
| `status` | TEXT | `pending` → `processing` → `completed` / `failed` |
| `worker_id` | TEXT | Analyzer claiming this track |
| `locked_at` | TEXT | When claimed |
| `retry_count` | INTEGER | Failure count |
| `error_message` | TEXT | Error details if failed |
| `created_at` | TEXT | ISO 8601 |
| `updated_at` | TEXT | ISO 8601 |

**Unique constraint:** `UNIQUE(album_id, filename)`

**Status transitions:**
```
pending → processing → completed
                     → failed
```

### `track_embeddings` — Feature Vectors (Created by Analyzers)

| Column | Type | Description |
|---|---|---|
| `track_id` | INTEGER PK FK | References `tracks.id` |
| `embedding` | BLOB | 564-dim float32 hybrid vector (2256 bytes) |
| `quality_score` | REAL | Composite quality score 0.0–1.0 |
| `created_at` | TEXT | ISO 8601 |

### `cursor_state` — Coordinator Resume (Singleton)

| Column | Type | Description |
|---|---|---|
| `id` | INTEGER PK | Always 1 |
| `last_cursor` | TEXT | IA scrape API cursor for resume |
| `items_indexed` | INTEGER | Total albums discovered |
| `last_run_at` | TEXT | Last scrape timestamp |

## Vector Format

564-dim float32 hybrid vector via late fusion:

| Positions | Dimensions | Source | Weight |
|---|---|---|---|
| 0–511 | 512 | CLAP semantic embedding | ×0.60 |
| 512–551 | 40 | MFCC acoustic texture (20 mean + 20 var) | ×0.25 |
| 552–563 | 12 | Chroma harmonic profile (12 semitones) | ×0.15 |

Total: 564 × 4 bytes = 2256 bytes per vector.

## MP3 Format Filtering (3-Tier)

When resolving an album, each file in the IA metadata is evaluated:

1. **`format == "VBR MP3"`** → ACCEPT (always)
2. **`format == "64Kbps MP3"` or `"128Kbps MP3"`** → REJECT (blacklist)
3. **`format == "MP3"`** → check `bitrate` field: accept if ≥ 192, reject otherwise
4. **Other formats containing "MP3"** → check bitrate if available, accept if ≥ 192
5. **Non-MP3 formats** → SKIP

## Track Title Derivation

Priority order:
1. IA metadata `title` field (if non-empty)
2. Derived from filename: strip extension → strip leading track numbers (`01 - `, `01_`, etc.) → replace `_`/`-` with spaces → title-case

## Migration

On startup, `db.Open()` auto-detects the old schema (`catalog_queue` table) and migrates:
1. Creates `albums` table
2. Copies `catalog_queue.ia_identifier` → `albums.ia_identifier` (status=`pending`)
3. Drops old `catalog_queue` and old `track_embeddings`
4. Creates new `tracks` and `track_embeddings` tables
5. `cursor_state` is preserved
