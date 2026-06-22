# Architecture — Unavailable vs Failed

## Data Model Changes

### Schema (albums)

Add `unavailable` to CHECK constraint:

```sql
status TEXT NOT NULL DEFAULT 'pending'
    CHECK(status IN ('pending','resolving','resolved','failed','unavailable'))
```

Migration: SQLite does not support `ALTER TABLE ... MODIFY CHECK`. Must recreate tables:

```sql
CREATE TABLE albums_new (columns..., CHECK(status IN ('pending','resolving','resolved','failed','unavailable')));
INSERT INTO albums_new SELECT * FROM albums;
DROP TABLE albums;
ALTER TABLE albums_new RENAME TO albums;
CREATE INDEX idx_albums_status ON albums(status);
```

Same pattern for `tracks`.

### Schema (tracks)

```sql
status TEXT NOT NULL DEFAULT 'pending'
    CHECK(status IN ('pending','processing','completed','failed','unavailable'))
```

### Stats structs (`internal/db/queue.go`)

```go
type AlbumStats struct {
    Total        int
    Pending      int
    Resolving    int
    Resolved     int
    Failed       int
    Unavailable  int    // NEW
    Unprechecked int
}

type TrackStats struct {
    Total       int
    Pending     int
    Processing  int
    Completed   int
    Failed      int
    Unavailable int    // NEW
}
```

`GetCombinedStats` adds `GROUP BY status` cases for `unavailable`.

## New DB Functions

```go
func MarkAlbumUnavailable(db *sql.DB, identifier, reason string) error
func MarkTrackUnavailable(db *sql.DB, trackID int, reason string) error
func FailAlbumAndPendingTracksUnavailable(sqlDB *sql.DB, albumID, reason string) (int64, error)
```

Update `FlagAlbumPoorQuality` to mark as `unavailable` instead of `failed`.

Update `ResetAllFailed` to filter `WHERE status = 'failed'` (already does, but verify `unavailable` is excluded).

## Event Types (`internal/tui/events.go`)

```go
EventAlbumUnavailable    EventType = "album_unavailable"
EventAnalysisUnavailable EventType = "analysis_unavailable"
```

## UI Changes

### Dashboard (`internal/tui/dashboard.go`)

- Stats panel: add "Unavailable:" row in Albums and Tracks sections
- Color: muted/gray (neither green/success nor red/failed)
- Event handlers: `EventAlbumUnavailable` increments resolver/cleaner fail count or a separate count
- `EventAnalysisUnavailable` increments analyzer fail count or separate count

### Status bar (`internal/tui/statusbar.go`)

- Optional: show unavailable counts alongside album/track totals
- Primary display of unavailable is in the Dashboard stats panel

## Call Site Changes (`cmd/tui/main.go`)

| Current function | New function | Trigger |
|---|---|---|
| `FailAlbumAndPendingTracksByID(...)` | `FailAlbumAndPendingTracksUnavailable(...)` | Cleanup: access-restricted |
| `FailAlbumAndPendingTracks(...)` | `FailAlbumAndPendingTracksUnavailable(...)` | Download 401/403 |
| `FlagAlbumPoorQuality(...)` | (internal: marks as unavailable) | Decode failure |
| `MarkTrackFailed(track, "low quality: ...")` | `MarkTrackUnavailable(track, reason)` | Quality score < threshold |
| `MarkAlbumFailed(album, "no acceptable MP3...")` | `MarkAlbumUnavailable(album, reason)` | 0 tracks resolved |
| `MarkAlbumFailed(album, "metadata: ...")` | `MarkAlbumUnavailable(album, reason)` | Non-transient metadata |

## Files Modified

| File | Changes |
|---|---|
| `internal/db/db.go` | Schema CHECK constraints, migration |
| `internal/db/queue.go` | New functions, updated stats, updated `FlagAlbumPoorQuality`, updated `ResetAllFailed` |
| `internal/tui/events.go` | New event types |
| `internal/tui/dashboard.go` | Unavailable display, event handling |
| `internal/tui/statusbar.go` | Unavailable count (optional) |
| `cmd/tui/main.go` | 6 call site updates |
