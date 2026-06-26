# Implementation Plan

## Phase 1 — Remove field recording/environmental sound from non-music terms

**File:** `internal/listenability/listenability.go`

1. In `IsNonMusicMetadata()` (line 353), remove `"field recording"` and
   `"environmental sound"` from the `nonMusicTerms` slice.

2. Update `const Version = "listenability-v2"` (line 8) to trigger re-scoring of
   existing tracks.

**Test:** Update `TestIsNonMusicMetadata` in `listenability_test.go`:
- Add test case: album with "Folk, World, & Country, Field Recording" → `nonMusic=false`
- Add test case: album with "field recordings, traditional music" → `nonMusic=false`

---

## Phase 2 — Live music channel-dump album exception

**File:** `internal/listenability/listenability.go`

The `album_short60_ratio_above_80pct` + `album_avg_duration_below_30s` combo is
punishing live concert albums that happen to have many short channel-stem tracks.

In `ScoreAlbum()`, modify the hard-score-zero block (line 155-158) to check whether
the album is a channel dump before setting score to 0.0. If the album has
`channel_dump_title_pattern` on any track, set score to 0.3 instead of 0.0 and
stream to "longform_candidate" instead of "excluded".

In `classifyAlbumStreamDecision()` (line 437), add a check: if the hard-reject
criteria is met BUT the album has channel_dump patterns, use "longform_candidate"
stream instead of "excluded".

The channel dump detection needs to be passed through from the track evidence.
Add a `HasChannelDumpPattern` field to `AlbumEvidence`.

**Test:** Add `TestScoreAlbumChannelDump` — album with avg dur 5s, short60 0.95,
channel_dump patterns → stream="longform_candidate", decision="demote".

---

## Phase 3 — Extend LongformMaxSeconds for classical music

**File:** `internal/listenability/listenability.go`

1. Change `LongformMaxSeconds = 2700` (was 1500)
2. Change `PreferredMaxSeconds = 1800` (was 900)
3. Adjust `DurationScore()` curve: the longform penalty band is now 1800–2700s,
   fading to zero at 2700s.

**Test:** Update `TestDurationScore` cases, `TestStreamClassificationEdges` cases,
and `TestScoreTrackLongform` to reflect new thresholds.

---

## Phase 4 — Add CmdRestartWorker control command

**File:** `cmd/tui/main.go`, `internal/tui/events.go`

1. Add `CmdRestartWorker` to the control command types, accepting a worker label
   pattern (e.g., "cleaner-1").

2. In the coordinator, handle `CmdRestartWorker` by looking up the worker type
   from the label prefix, finding the dead goroutine's stop channel, and spawning
   a replacement goroutine with an incremented ID (e.g., "cleaner-2").

---

## Phase 5 — Migration to re-score v1-excluded tracks

**File:** `internal/db/listenability.go` (new function)

The version bump to `listenability-v2` is sufficient for tracks still `status='completed'` —
the cleaner's query already picks up `WHERE listenability_version IS NULL OR listenability_version != ?`.

But tracks marked `status='unavailable'` by listenability (via `MarkTrackUnavailable`) are
bypassed by the cleaner. We need a migration to reset them.

```sql
-- Reset listenability version for all completed v1 tracks (cleaner will re-pick them)
UPDATE tracks SET listenability_version = NULL
WHERE status = 'completed' AND listenability_version = 'listenability-v1';

-- Reset unavailable tracks that were excluded by listenability (not by download failure)
UPDATE tracks SET
  status = 'completed',
  listenability_version = NULL,
  listenability_decision = NULL,
  listenability_stream = NULL,
  listenability_tier = NULL,
  listenability_reasons = NULL,
  listenability_components = NULL,
  listenability_checked_at = NULL,
  listenability_worker_id = NULL,
  error_message = NULL,
  updated_at = datetime('now')
WHERE status = 'unavailable'
  AND error_message LIKE 'listenability:%';
```

Add `MigrateListenabilityV1ToV2(db *sql.DB) (int64, error)` to `internal/db/listenability.go`
that runs this migration and returns the number of tracks reset. Call at startup in
`main.go:runCoordinator` after the DB is initialized.

---

## Implementation Order

1. Phase 1 — Simple removal, no architectural changes
2. Phase 2 — Structural change to album evidence, moderate complexity
3. Phase 3 — Threshold tuning, low complexity
4. Phase 4 — Add CmdRestartWorker control command
5. Phase 5 — Migration to re-score v1-excluded tracks

Each phase is independently testable and reversible.
