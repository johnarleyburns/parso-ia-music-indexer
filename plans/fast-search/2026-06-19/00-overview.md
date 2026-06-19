# Fast Search: Audit + O(n log k) Ranking

## Problem

At Boston Public Library scale (~12k-163k track embeddings), two ranking
bottlenecks block interactive similarity and text search:

1. `sortByDist` in `QuerySimilar` is O(n^2) selection sort.
2. `SearchByText` collects all candidates then full-sorts with `sort.Slice`
   (O(n log n)), which is reasonable but still materializes the entire result
   set when only top-k are needed.

## Phase 0 Audit Summary (read-only)

Storage is already unfused-only. No changes needed.

### db.go schema (line 115-126)
- `track_embeddings` has `clap BLOB`, `mfcc BLOB`, `chroma BLOB`,
  `model_version TEXT`, `dim INTEGER`, `dtype TEXT`, `quality_score REAL`.
- No `embedding`, `fused`, or `hybrid` column exists.
- `migrateSchemaChanges()` (line 148-153) drops the old table if an `embedding`
  column is detected. Clean migration.

### embeddings.go SaveEmbedding (line 30-38)
- Takes separate `clap`, `mfcc`, `chroma []float32`.
- L2-normalizes each independently, encodes to f16, stores as three BLOBs.
- Fused vector is never persisted.

### cmd/tui/main.go analyzeTrack (line 521-681)
- Computes `clapVec`, `mfccVec`, `chromaVec` separately.
- Calls `db.SaveEmbedding(sqlDB.Conn, track.ID, clapVec, mfccVec, chromaVec, compositeScore)`.
- Does NOT call `FuseFeatures` before saving.

### Remaining write paths
- `FuseFeatures` is only called in `QuerySimilar` at query time (lines 48, 67).
- `SaveEmbedding` is called in exactly two places: `analyzeTrack` (main.go:646)
  and `setupTrackWithEmbedding` (db_test.go:318). Neither persists a fused vector.
- No bug found. Storage is unfused-only.

### model_version
- Default `'clap-htsat-fused:audio+text:512:l2:f16'` refers to the CLAP model
  name (`htsat-fused`), not to the storage format.

## Phases

| Phase | Scope | Status |
|-------|-------|--------|
| 0 | Audit (above) | Complete |
| 1 | Replace O(n^2) `sortByDist` and `SearchByText` full-sort with bounded top-k heap | Pending approval |
| 2 | Query-time fusion shortcut (optional, proposed in decisions.md) | Pending approval |
