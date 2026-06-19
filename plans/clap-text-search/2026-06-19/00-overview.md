# CLAP Text-Search + Float16 + Unfused Vector Storage

## Problem

The indexer currently stores a single pre-fused 564-dim hybrid vector per track
(CLAP 512 + MFCC 40 + chroma 12, each scaled by fixed weights). This prevents:

1. **Text-to-audio search** -- the CLAP text tower is stripped from the sidecar
   at load time, so there is no way to embed a text query into the shared
   512-dim CLAP space and compare against stored audio embeddings.
2. **On-device flexibility** -- the iOS app receives the SQLite file directly
   but cannot re-fuse with different weights or perform CLAP-only search
   because the components are pre-mixed.
3. **Storage efficiency** -- vectors are stored as f32 blobs, using 4 bytes per
   dimension when IEEE-754 half (2 bytes) is sufficient for cosine search on
   normalized vectors.
4. **Discovery quality** -- popular LibriVox audiobooks leak into collection
   results, polluting the music index.

## Goals

1. Re-enable the CLAP text tower in the Python sidecar and expose it via gRPC
   (Workstream A).
2. Store all vectors as float16, halving on-disk and on-wire-to-app size
   (Workstream B).
3. Persist CLAP, MFCC, and chroma as separate component blobs instead of a
   pre-fused hybrid; reconstruct the fused vector at query time
   (Workstream C).
4. Migrate the DB schema to support component storage + text search, and
   implement a `SearchByText` proof function (Workstream D).
5. Filter known-popular LibriVox audiobooks from collection discovery
   (Workstream E).
6. (Deferred) Segment-level vectors for long-form audio (Workstream D-bonus).

## Current System Behavior

- `python_sidecar/server.py` loads `laion/clap-htsat-fused` but discards
  `full_model.text_model` and `full_model.text_projection` to save ~500MB RAM.
  Only the audio path is retained.
- `cmd/tui/main.go:analyzeTrack` (~L506) streams audio, decodes MP3, computes
  quality metrics, MFCC, chroma, and CLAP embedding, then calls
  `hybrid.FuseFeatures` and `db.SaveEmbedding` to persist a single 564-dim
  f32 BLOB.
- `internal/db/embeddings.go` encodes/decodes f32 blobs and does brute-force
  cosine distance for `QuerySimilar`.
- `internal/hybrid/fusion.go` applies fixed weights (0.60, 0.25, 0.15) and
  concatenates into 564 dims.
- `discoverCollection` in `main.go` (~L294) maps IA scrape results directly to
  `[]db.AlbumInsert` with no filtering.

## Scope Boundaries

- The CLAP model checkpoint stays `laion/clap-htsat-fused`. "Unfused storage"
  means separating the stored feature blocks, not changing the model.
- gRPC wire format remains f32 (protobuf limitation). f16 conversion happens
  only at the Go storage boundary.
- Segment-level vectors (D-bonus) are planned but deferred until core phases
  land and are approved.

## Phase Order

1. **This document + decisions.md** -- STOP for approval.
2. Phase 1: f16 codec + round-trip tests (Workstream B foundation).
3. Phase 2: Schema migration + component storage + query-time fusion parity
   (Workstreams C + D storage), with backfill from existing 564-dim hybrids.
4. Phase 3: Sidecar text RPC + Go client (Workstream A).
5. Phase 4: `SearchByText` end-to-end (Workstream D search).
6. Phase 5: LibriVox discovery filter (Workstream E).
7. (Deferred) Phase 6: Segment-level vectors (Workstream D-bonus).

Each phase: branch, implement, `make build`, `make test`, report, commit.
