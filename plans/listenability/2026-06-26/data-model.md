# Listenability Data Model

## Problem

The database needs durable listenability state for:

- New tracks scored during analysis.
- Existing completed tracks scored by a cleanup worker.
- Album-level shape evidence that influences track scores.
- Auditability when a track is demoted or excluded.
- Versioned recalculation when thresholds change.

The schema must remain additive and backward compatible.

## Current Behavior

Current tables:

- `albums` stores IA album metadata, status, track count, subjects, genres, and downloads.
- `tracks` stores IA file metadata, status, tags, and queue state.
- `track_embeddings` stores CLAP/MFCC/chroma vectors, model metadata, sample strategy, and `quality_score`.

There is no listenability state. Search only filters by `tracks.status = 'completed'`.

## Research Findings

The score depends on both track-level and album-level features. Duplicating album-shape evidence into every track would make recalibration awkward. Keeping album score on `albums` and track score on `tracks` preserves the existing table boundaries.

SQLite migrations in this repo already use additive `ALTER TABLE ... ADD COLUMN` for evolving schema, so listenability should follow that pattern.

JSON stored as `TEXT` is already appropriate for audit fields because the codebase does not currently depend on SQLite JSON functions.

## Design Proposal

Add columns to `tracks`:

```sql
ALTER TABLE tracks ADD COLUMN listenability_score REAL;
ALTER TABLE tracks ADD COLUMN listenability_tier TEXT;
ALTER TABLE tracks ADD COLUMN listenability_decision TEXT;
ALTER TABLE tracks ADD COLUMN listenability_stream TEXT;
ALTER TABLE tracks ADD COLUMN listenability_reasons TEXT;
ALTER TABLE tracks ADD COLUMN listenability_components TEXT;
ALTER TABLE tracks ADD COLUMN listenability_version TEXT;
ALTER TABLE tracks ADD COLUMN listenability_checked_at TEXT;
ALTER TABLE tracks ADD COLUMN listenability_worker_id TEXT;
ALTER TABLE tracks ADD COLUMN listenability_locked_at TEXT;
```

Add columns to `albums`:

```sql
ALTER TABLE albums ADD COLUMN listenability_score REAL;
ALTER TABLE albums ADD COLUMN listenability_tier TEXT;
ALTER TABLE albums ADD COLUMN listenability_decision TEXT;
ALTER TABLE albums ADD COLUMN listenability_stream TEXT;
ALTER TABLE albums ADD COLUMN listenability_reasons TEXT;
ALTER TABLE albums ADD COLUMN listenability_components TEXT;
ALTER TABLE albums ADD COLUMN listenability_version TEXT;
ALTER TABLE albums ADD COLUMN listenability_checked_at TEXT;
```

Add indexes:

```sql
CREATE INDEX IF NOT EXISTS idx_tracks_listenability_work
ON tracks(status, listenability_version, listenability_locked_at);

CREATE INDEX IF NOT EXISTS idx_tracks_listenability_score
ON tracks(listenability_score);
```

Canonical version:

```text
listenability-v1
```

Example `listenability_components`:

```json
{
  "duration": 0.0,
  "album_shape": 0.0,
  "content_type": 0.35,
  "technical_quality": 0.88,
  "metadata_hygiene": 0.4,
  "album_avg_duration_sec": 0.03,
  "album_short60_ratio": 1.0,
  "prompt_margin": -0.18,
  "longform_candidate": false
}
```

Example `listenability_reasons`:

```json
[
  "duration_below_10s",
  "album_avg_duration_below_30s",
  "album_short60_ratio_above_80pct",
  "channel_dump_title_pattern"
]
```

## System Changes

DB helper additions:

- `UpdateTrackListenability(db, trackID, result)`
- `UpdateAlbumListenability(db, albumID, result)`
- `GetAlbumListenabilityEvidence(db, albumID)`
- `GetTrackListenabilityEvidence(db, trackID)`
- `ClaimListenabilityCleanupBatch(db, workerID, version, batchSize)`
- `ReleaseListenabilityCleanupClaim(db, trackID, errMsg)` if scoring fails.
- `GetListenabilityCoverage(db)`
- `GetListenabilityDryRun(db, version, threshold)`
- `GetCollectionListenabilityStats(db)` for collection rows.
- `GetAlbumAverageListenability(db, albumID)` for album list/detail rows.

Track claim changes:

- Extend `ClaimedTrack` with:
  - `Duration float64`
  - `Bitrate int`
  - `Tags string`
  - `AlbumSubjects string`
  - `AlbumGenres string`
  - `AlbumListenabilityScore float64`
  - `AlbumListenabilityComponents string`

Result/model additions:

- Add `ListenabilityScore` to track result structs used by browse, player, text search, and recommendations.
- Add `AvgListenability` or equivalent to album and collection result structs.
- Keep `QualityScore` fields intact; listenability should be displayed beside quality, not replace it.

Search/export later:

- Prefer `WHERE (t.listenability_decision IS NULL OR t.listenability_decision != 'exclude')` during transition.
- For default stream/search, also exclude rows with `listenability_stream = 'longform_candidate'` until Longform exists.
- After backfill, use `listenability_score >= ?` or `listenability_decision IN ('include','borderline')`, depending on product choice.

## Implementation Steps

1. Add migrations in `internal/db/db.go`.
2. Add DB structs in a new `internal/db/listenability.go`.
3. Add migration tests for both fresh and existing schemas.
4. Extend track claim queries only after scoring code exists.
5. Add coverage reporting in headless stats.

## Testing Strategy

- Fresh DB migration creates all columns.
- Existing DB migration adds missing columns without data loss.
- Claim query only returns completed rows missing the target version or stale rows locked beyond timeout.
- Updating listenability does not change `status` in score-only mode.
- Optional unavailable mutation preserves embedding rows and removes the track from search because status changes.

## Open Questions

- Whether to store `listenability_decision = 'exclude'` for tracks that remain `completed` in score-only mode. Recommendation: yes, because it allows reporting without mutation.
- Whether to add a normalized `listenability_version` table. Recommendation: not for v1; constants in code are sufficient.
