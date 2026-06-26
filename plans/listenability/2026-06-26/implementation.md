# Listenability Implementation Plan

## Problem

The implementation must score both future tracks and historical completed tracks without destabilizing ingestion.

The risk is high if cleanup immediately mutates many existing rows, because local DB stats show a large number of short tracks. The first implementation should make the catalog observable before removing content.

## Current Behavior

Current worker controls:

- Coordinator discovers albums.
- Resolver resolves album metadata and inserts tracks.
- Analyzer processes pending tracks into embeddings.
- Enhancer backfills tags for completed tracks.

Current verification:

- `make build` creates `bin/timbre`.
- `go vet ./...` and `go test -race -count=1 ./...` are required before handoff.

## Research Findings

Local DB analysis shows:

- A hard 90-second cutoff is too aggressive for a first pass.
- A hard 60-second cutoff still affects a large set, so it should be represented as scoring/demotion first.
- Album-shape evidence catches the obvious bad cases better than track duration alone.
- CLAP prompt classification can score existing completed tracks without network download.

The repo already has a precedent for background cleanup via tag enhancement and for versioned rework via sampling strategy migration.

## Design Proposal

Implement in six phases.

### Phase 1: Schema and Pure Scoring

Add:

- Listenability columns on `tracks` and `albums`.
- `internal/listenability` package.
- Unit tests for duration curve, album shape, metadata patterns, and score composition.

No worker behavior changes in this phase.

### Phase 2: Album Shape During Resolution

During `resolveAlbum()`:

- Compute `AlbumEvidence` from `album.Tracks`.
- Store album listenability fields.
- Keep existing `ia.IsMusicContent()` behavior.
- Do not reject albums based only on new listenability unless the album is an extreme hard case and config allows it.

Recommended hard album cases:

- Track count >= 5, average positive duration < 15 seconds, and short60 ratio >= 0.80.
- Metadata/title strongly indicates sound effects, test tones, stems, channel dumps, or loops.
- Metadata/title/subject/genre strongly indicates Non-Music, Special Effects, Speech, Audiobook, or Story.

### Phase 3: Analyzer Scoring For New Tracks

Extend `ClaimedTrack` so `analyzeTrack()` has duration, bitrate, tags, album metadata, and album listenability context.

Before streaming:

- Run metadata-only listenability.
- If the track has no audio URL, has no positive duration, is under 60 seconds, or has obvious non-music metadata, store result and mark new tracks unavailable without network download.

After quality and CLAP:

- Compute prompt evidence using cached prompt vectors.
- Combine duration, album shape, content-type, quality, and metadata hygiene.
- Save embedding and listenability result in the same DB critical section.
- If score is `unusable`, either mark unavailable or demote depending on config. Default: mark default-stream hard exclusions unavailable for new tracks; historical cleanup remains score-only until reviewed.
- If duration is 15-25 minutes, set the stream to `longform_candidate`, downrank in default stream, and do not automatically mark unavailable.

### Phase 4: Cleaner Worker For Existing Tracks

Add `listenabilityCleanerLoop()`.

Behavior:

- Claim completed tracks where `listenability_version IS NULL OR listenability_version != current`.
- Load `track_embeddings.clap`, `quality_score`, track metadata, album metadata, and album listenability.
- Cache prompt text embeddings once.
- Score rows and write listenability fields.
- Default action: score only.
- Optional action: mark `exclude` rows unavailable.

Controls:

- TUI: add a cleaner pool control after analyzer/enhancer, likely `[l] cleaners` and `[L] remove`.
- Headless/config: add flags for a one-shot or continuous cleaner mode.

Suggested config:

```text
--listenability-min-track-seconds=60
--listenability-target-track-seconds=90
--listenability-preferred-max-seconds=900
--listenability-longform-max-seconds=1500
--listenability-version=listenability-v1
--listenability-threshold=0.50
--listenability-hard-threshold=0.25
--listenability-cleaner-action=score-only
```

Valid cleaner actions:

- `score-only`
- `mark-unavailable`

### Phase 5: Reporting and Calibration

Add headless stats:

- Total tracks with current listenability version.
- Counts by tier.
- Counts by decision.
- Count that would be marked unavailable at current thresholds.
- Top reasons.
- Album-level counts by decision.

Add a focused report command or headless JSON block for examples:

- Lowest scoring completed tracks.
- Albums with average duration < 30/60/90 seconds.
- Completed tracks under 60 seconds but high CLAP music evidence.
- Tracks excluded by prompt evidence but not by duration.

Add worker log visibility:

- Analyzer complete/unavailable messages should include listenability score, tier, decision, stream, and primary reason.
- Cleaner row-level messages should include listenability score, tier, decision, stream, action taken, and primary reason.
- Cleaner batch/summary messages should include counts by decision, counts by tier, unavailable mutations if enabled, and current-version coverage.

Add TUI visibility:

- Collections tab: add an average listenability column/field derived from scored tracks in the collection. If the tab is showing albums within a collection, show album average listenability.
- Browse albums: add average listenability beside average quality.
- Browse album detail and track search: add per-track listenability beside quality.
- Player: add current-track listenability beside the current quality display.
- Recommendations/similar tracks: add listenability beside quality and distance/similarity.
- Use compact numeric formatting, likely `0.000`, matching existing quality formatting.

### Phase 6: Query/Export Filtering

Only after backfill:

- Update `SearchByText()` and similar retrieval/export paths to filter or demote low-listenability rows.
- Update recommendation/similar-track ranking to either filter `exclude` rows or downweight low-listenability rows after backfill.
- Transition rule should include old null rows:

```sql
AND (t.listenability_decision IS NULL OR t.listenability_decision != 'exclude')
```

Once backfill is complete, use a numeric threshold.

## System Changes

Files likely touched:

- `internal/db/db.go`
- New `internal/db/listenability.go`
- `internal/db/queue.go`
- New `internal/listenability/listenability.go`
- New `internal/listenability/listenability_test.go`
- `cmd/tui/main.go`
- `internal/tui/dashboard.go`
- `internal/tui/events.go`
- `internal/tui/metrics.go`
- `internal/tui/collections.go`
- `internal/tui/browse.go`
- `internal/tui/player.go`
- `internal/config/config.go`
- Search/export code paths after backfill decision.
- Recommendation/similar-track result paths after backfill decision.

Keep changes incremental. Do not combine all phases in one commit.

## Implementation Steps

1. Create branch for Phase 1.
2. Implement schema and pure scoring.
3. Verify with build/vet/race tests.
4. Commit.
5. Implement resolver album scoring.
6. Verify.
7. Commit.
8. Implement analyzer scoring for new tracks.
9. Verify.
10. Commit.
11. Implement cleaner worker score-only.
12. Add analyzer/cleaner log output and TUI display fields.
13. Verify and run dry-run report.
14. Commit.
15. After reviewing dry-run, implement optional unavailable mutation and query filtering/ranking changes.

## Testing Strategy

Phase-by-phase verification:

```sh
make build
go vet ./...
go test -race -count=1 ./...
```

Dry-run acceptance checks:

- Cleaner can score 1,000 rows without changing `tracks.status`.
- Re-running cleaner with same version is idempotent.
- Changing version makes rows eligible again.
- Score-only report identifies obvious bad albums such as channel dumps.
- Tracks below 60 seconds are mostly `demote` or `exclude`, not silently deleted.
- Null-duration tracks are excluded from the default stream because duration is required, but historical status mutation waits for an explicit cleanup decision.
- Collections, Browse, Player, and recommendations display listenability values where scored data exists.
- Analyzer and cleaner logs include listenability values and reasons.

## Open Questions

- Whether Phase 4 should be available through TUI immediately or first as a headless/admin command.
- Whether query filtering should use `decision != 'exclude'` or `score >= threshold`.
- Whether a future v2 should fetch IA file sizes to improve duration confidence where IA `length` looks wrong.
