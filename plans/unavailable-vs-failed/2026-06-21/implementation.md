# Implementation — Unavailable vs Failed

## Phase 1: Schema Migration (`internal/db/db.go`)

1. Update `CREATE TABLE IF NOT EXISTS albums` — add `unavailable` to CHECK constraint
2. Update `CREATE TABLE IF NOT EXISTS tracks` — add `unavailable` to CHECK constraint
3. Add migration in `migrateSchemaChanges()`:
   - Check if `unavailable` exists in CHECK constraint by inspecting `sqlite_master`
   - If not, recreate both tables (CREATE new → INSERT → DROP old → RENAME → reindex)
   - Use a flag column or PRAGMA to track migration state

## Phase 2: DB Functions (`internal/db/queue.go`)

1. Add `Unavailable` to `AlbumStats` and `TrackStats`
2. Update `GetCombinedStats` to query and populate `Unavailable`
3. Add `MarkAlbumUnavailable(sqlDB, albumID, reason)`
4. Add `MarkTrackUnavailable(sqlDB, trackID, reason)`
5. Add `FailAlbumAndPendingTracksUnavailable(sqlDB, albumID, reason)` — same as `FailAlbumAndPendingTracksByID` but marks as `unavailable`
6. Update `FlagAlbumPoorQuality` — change `failed` to `unavailable` for album status and pending track updates
7. Verify `ResetAllFailed` — confirm `WHERE status = 'failed'` excludes `unavailable`

## Phase 3: Event Types (`internal/tui/events.go`)

1. Add `EventAlbumUnavailable EventType = "album_unavailable"`
2. Add `EventAnalysisUnavailable EventType = "analysis_unavailable"`

## Phase 4: Call Sites (`cmd/tui/main.go`)

1. Cleaner restricted → `FailAlbumAndPendingTracksUnavailable` + emit `EventAlbumUnavailable`
2. Download 401/403 → `FailAlbumAndPendingTracksUnavailable` + emit `EventAlbumUnavailable`
3. Decode failure → `FlagAlbumPoorQuality` (updated internally in Phase 2) + emit `EventAnalysisUnavailable`
4. Low quality score → `MarkTrackUnavailable` + emit `EventAnalysisUnavailable`
5. No acceptable MP3s → `MarkAlbumUnavailable` + emit `EventAlbumUnavailable`
6. Non-transient metadata → `MarkAlbumUnavailable` + emit `EventAlbumUnavailable`

## Phase 5: Dashboard UI (`internal/tui/dashboard.go`)

1. Add `Unavailable` row in Albums stats panel (muted/gray color)
2. Add `Unavailable` row in Tracks stats panel (muted/gray color)
3. Update `ActivityEvent` handler:
   - `EventAlbumUnavailable` → same handling as `EventAlbumFailed` (increment fail count, clear task)
   - `EventAnalysisUnavailable` → same handling as `EventAnalysisFailed`

## Phase 6: Status Bar (`internal/tui/statusbar.go`)

1. Optional: add `Unavailable` counts to line 3 (album/track totals)
2. Primary unavailable display is in the Dashboard stats panel

## Phase 7: Test Updates (`internal/db/db_test.go`)

1. Update any test that checks `status = 'failed'` to account for `unavailable`
2. Add tests for new unavailable functions

## Phase 8: Build, Test, Commit

1. `make build` — confirm compilation
2. `go test ./...` — confirm all tests pass
3. Commit with descriptive message
4. Push

## Verification

After implementation:
- Restricted albums appear as "Unavailable" in Dashboard (not "Failed")
- Poor quality tracks appear as "Unavailable" (not "Failed")
- `ResetAllFailed` does NOT reset unavailable items
- Cleaner marks restricted albums as unavailable
- Network/CLAP/DB errors still mark as "Failed" and are retryable
