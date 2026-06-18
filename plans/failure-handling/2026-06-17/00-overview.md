# Failure Handling Redesign

## Problem
Failed albums and tracks are terminal with no recovery path. Several failure paths have bugs where items get stuck in intermediate states (`resolving`/`processing`) instead of being properly marked as `failed`.

## Bugs Found
1. `SaveEmbedding` failure in `analyzeTrack` (main.go:496-507): track left in `processing` status, never marked `failed`
2. `InsertTracks` DB error in `resolveAlbum` (main.go:586-598): album left in `resolving`, not marked `failed`
3. `MarkAlbumResolved` DB error in `resolveAlbum` (main.go:600-611): album left in `resolving`, not marked `failed`
4. Rate limit failure in `resolveAlbum` (main.go:534-540): marks album failed but doesn't emit `EventAlbumFailed` to TUI

## Design
### Error Classification
- **Transient**: stream errors, rate limits, CLAP service errors, timeouts, connection failures
- **Permanent**: decode errors, low quality, no acceptable MP3 tracks, mp3 decode panics

### Auto-Retry
- Transient failures requeue to `pending` with `retry_count` increment (max 3 retries)
- After max retries exhausted, mark as permanently `failed`
- Albums get a `retry_count` column (tracks already have one)

### Manual Reset
- `F` hotkey on Dashboard resets all `failed` items to `pending` (retry_count zeroed)

### Stuck Track Recovery
- Periodic `ResetStuckTracks` call (every 5 min) for tracks stuck in `processing`

## Implementation Phases
1. Fix bugs (missing MarkAlbumFailed/MarkTrackFailed calls)
2. DB changes (album retry_count, RequeueForRetry, ResetAllFailed functions)
3. Error classification function
4. Auto-retry logic in analyzeTrack/resolveAlbum
5. TUI reset command (F hotkey)
6. Periodic ResetStuckTracks in coordinator
