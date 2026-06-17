# Phase P6B: Browse Tab Fixes

**Date:** 2026-06-17
**Status:** PENDING
**Depends on:** Phase 6, Phase PX

---

## Problem

Seven issues discovered during Phase 6 testing of the Browse tab:

1. No way to switch between Albums / Tracks views while search input is focused
2. Album browse list and detail view missing aggregate quality info (avg quality score)
3. IA download counts are not stored or displayed for albums/tracks
4. Album art renders as garbled numbers instead of images in some terminals
5. After entering search, visiting an album detail, and returning, navigation around the search box is broken/unintuitive
6. Default empty browse list sorts by `updated_at DESC` instead of popularity (downloads)
7. All of the above need a plan (this document)

---

## Current Behavior

### Issue 1 — Mode Toggle Only Works When Table Focused
- The `m` key toggles Albums/Tracks mode but is only handled in `handleTableKey()` (`browse.go:305-321`)
- When `inputFocused=true`, the `handleInputKey()` function (`browse.go:268-293`) does NOT handle `m`
- On initial load, `inputFocused=true` (`browse.go:108`), so the user cannot switch modes without first pressing Esc/Enter
- The hint bar when input is focused says: `[esc] focus table  [enter] focus table` — no mention of mode switching

### Issue 2 — No Quality Summary on Albums
- `AlbumResult` struct has no quality field (`queue.go:57-66`)
- `SearchAlbums()` query does not compute `AVG(quality_score)` (`queue.go:405-458`)
- Album detail header shows track count and completed count, but no aggregate quality
- Album list table columns are: Album, Artist, Collection, Tracks — no Quality column

### Issue 3 — Download Counts Not Captured or Stored
- The IA scraping API is called with `sorts=downloads desc` (`types.go:22`) but the `fields` parameter is not set
- `ScrapeItem` struct only has `Identifier` (`types.go:9-11`) — no `Downloads` field
- `albums` table has no `downloads` column (`db.go:42-54`)
- `BulkInsertAlbums()` inserts only `ia_identifier` — no download count (`queue.go:140`)
- The IA Scraping API supports `fields=identifier,downloads` to return download counts per item

### Issue 4 — Album Art Renders as Numbers
- Art protocol detection (`art.go:46-58`) checks `TERM` and `TERM_PROGRAM` env vars
- If detected as `kitty` or `iterm`, `rasterm.KittyWriteImage()` / `rasterm.ItermWriteImage()` encodes image data as terminal escape sequences
- If the terminal is misdetected (e.g., running inside tmux, screen, or a multiplexer that strips image protocols), the raw escape sequences render as garbled text including numbers
- The `getArtDisplay()` fallback (`browse.go:657-662`) checks `IsSupported()` but trusts the detection result
- `RenderArtPlaceholder()` uses a letter in a bordered box — not a number — so the issue is the *encoded* art string being displayed as text

### Issue 5 — Search Focus Navigation Broken After Album Detail Return
- Flow: input focused -> type query -> Enter (table focused) -> Enter on album (detail mode, search blurred at `browse.go:208`) -> Esc (back to albums mode)
- On Esc from detail (`browse.go:353-358`): sets `mode=ModeAlbums`, fires album search, but does NOT change `inputFocused` or restore focus
- The user lands on the album list with table focused, old search query still in the input, but no clear indication of how to clear/re-enter search
- Tab key is not handled anywhere — there's no toggle between search and table
- After `Activate()` is called (tab switch), search is re-focused (`browse.go:535-538`), but this only happens on tab switch, not on Esc-back from detail

### Issue 6 — Default Sort Is `updated_at DESC` Instead of Downloads
- `SearchAlbums()` empty query: `ORDER BY a.updated_at DESC` (`queue.go:420`)
- `SearchCompletedTracks()` empty query: `ORDER BY t.updated_at DESC` (`queue.go:362`)
- This shows the most recently resolved albums first, which is not useful for discovery
- Should default to most popular (by IA download count) once downloads are stored

---

## Design Proposal

### Fix 1 — Mode Toggle in Input Context

Add `tab` key handling in `handleInputKey()` to toggle mode while search input is focused. Also handle `tab` in `handleTableKey()` as an alternative to `m` for mode switching.

**Files modified:** `internal/tui/browse.go`

**Changes:**
- `handleInputKey()`: intercept `tab` keypress, toggle mode, fire new search
- `handleTableKey()`: add `tab` as alias for `m` (mode toggle)
- Update hint bar to show `[tab] switch mode` in all contexts
- Keep `m` as secondary binding for backward compatibility

### Fix 2 — Album Average Quality

Add computed `AvgQuality` field to `AlbumResult`. Compute it via SQL subquery joining `track_embeddings`.

**Files modified:** `internal/db/queue.go`, `internal/tui/browse.go`

**Changes:**
- `AlbumResult` struct: add `AvgQuality float64`
- `SearchAlbums()`: add `COALESCE((SELECT AVG(e.quality_score) FROM tracks t INNER JOIN track_embeddings e ON t.id = e.track_id WHERE t.album_id = a.ia_identifier AND t.status = 'completed'), 0.0)` to SELECT
- `GetAlbumByID()`: same subquery
- Album list table: add "Avg Quality" column
- Album detail header: show avg quality in info line
- Format as `%.3f` with "---" fallback if 0

### Fix 3 — Store and Display IA Download Counts

Capture download counts from the IA Scraping API and store them in the database.

**Files modified:** `internal/ia/types.go`, `internal/ia/scrape.go`, `internal/db/db.go`, `internal/db/queue.go`, `internal/tui/browse.go`, `cmd/tui/main.go`

**Schema migration:**
```sql
ALTER TABLE albums ADD COLUMN downloads INTEGER NOT NULL DEFAULT 0;
```
Use additive migration: check if column exists, add if not.

**IA API changes:**
- `ScrapeItem` struct: add `Downloads int` field with `json:"downloads"`
- `ScrapePage()`: add `fields=identifier,downloads` to URL params

**DB changes:**
- `BulkInsertAlbums()`: accept `[]ScrapeItem` (or a new type with identifier+downloads), insert downloads
- Or: new function `BulkInsertAlbumsWithDownloads(db, items []AlbumInsert)` where `AlbumInsert{Identifier, Downloads}`
- `AlbumResult` struct: add `Downloads int`
- `SearchAlbums()`: select `COALESCE(a.downloads, 0)`, scan into result
- `GetAlbumByID()`: same

**TUI changes:**
- Album list table: add "DLs" or "Downloads" column (compact, formatted with K/M suffixes)
- Album detail header: show download count
- Track list: show album download count (tracks don't have individual download counts in IA)

**Note:** IA download counts are at the item (album) level, not per-track. Tracks inherit the album's download count for display.

### Fix 4 — Album Art Fallback and Error Handling

Improve protocol detection and add graceful degradation.

**Files modified:** `internal/tui/art.go`, `internal/tui/browse.go`

**Changes:**
- `detectProtocol()`: also check for `TMUX`, `STY` (screen), `TERM_PROGRAM_VERSION` to detect multiplexer scenarios that break image protocols
- `encodeImage()`: validate output — if the encoded string is suspiciously short or contains no ESC characters, return `""` to trigger fallback
- `getArtDisplay()`: add additional sanity check on `m.currentArt` — if it doesn't start with ESC (`\x1b`), treat it as invalid and show placeholder
- Add `TIMBRE_NO_ART=1` env var override to force placeholder mode (useful for incompatible terminals)
- `RenderArtPlaceholder()`: improve placeholder design for wider compatibility

### Fix 5 — Search Focus Navigation

Improve navigation flow between search input and table, especially after returning from album detail.

**Files modified:** `internal/tui/browse.go`

**Changes:**
- After Esc from album detail (`handleTableKey` case `"esc"` ModeAlbumDetail block): set `m.inputFocused = true`, call `m.searchInput.Focus()`, blur table
- Add `tab` key handling everywhere to toggle focus between search input and table (in addition to mode toggle from Fix 1 — use `tab` for focus toggle, keep `m` for mode toggle, reconsider keybindings)
- Actually, better design: `tab` toggles focus (search <-> table), `m` toggles mode. This avoids overloading `tab`.

**Revised keybinding design:**
| Key | Context | Action |
|-----|---------|--------|
| `tab` | search focused | Move focus to table |
| `tab` | table focused | Move focus to search |
| `m` | table focused (non-detail) | Toggle Albums/Tracks mode |
| `/` | table focused (non-detail) | Focus search input |
| `esc` | search focused | Move focus to table |
| `esc` | table focused, detail | Back to album list (focus search) |
| `esc` | table focused, similarity | Back to previous view |

- After returning from album detail, auto-focus search input
- Update all hint bars to reflect new keybindings

### Fix 6 — Default Sort by Downloads

Change default (empty query) sort order to `downloads DESC`.

**Files modified:** `internal/db/queue.go`

**Changes:**
- `SearchAlbums()` empty query: `ORDER BY a.downloads DESC, a.updated_at DESC`
- `SearchCompletedTracks()` empty query: `ORDER BY a.downloads DESC, e.quality_score DESC, t.updated_at DESC` (join albums to get download count, then quality)
- Add `downloads` to the albums JOIN in track search if not already present (it is — `INNER JOIN albums a ON t.album_id = a.ia_identifier`)

---

## Implementation Phases

### Phase P6B-1: Schema + Download Count Storage
**Files:** `internal/db/db.go`, `internal/ia/types.go`, `internal/ia/scrape.go`, `internal/db/queue.go`, `cmd/tui/main.go`

1. Add `downloads` column migration to `db.go`
2. Add `Downloads` field to `ScrapeItem`
3. Add `fields=identifier,downloads` to `ScrapePage()` URL params
4. Create `AlbumInsert` type with `Identifier` + `Downloads`
5. Update `BulkInsertAlbums()` to accept and store download counts
6. Update `coordinatorLoop()` to pass download counts through
7. Add `Downloads` field to `AlbumResult`
8. Update `SearchAlbums()`, `GetAlbumByID()` to select/scan `downloads`

**Exit criteria:** Database stores downloads. Existing tests pass. Build succeeds.

### Phase P6B-2: Album Average Quality
**Files:** `internal/db/queue.go`

1. Add `AvgQuality float64` to `AlbumResult`
2. Add AVG subquery to `SearchAlbums()` both empty and filtered branches
3. Add AVG subquery to `GetAlbumByID()`

**Exit criteria:** Album results include average quality. Existing tests pass.

### Phase P6B-3: Browse Tab Keybinding + Navigation Fixes
**Files:** `internal/tui/browse.go`

1. Add `tab` key to `handleInputKey()` — toggle focus to table
2. Add `tab` key to `handleTableKey()` — toggle focus to search
3. After Esc from album detail — auto-focus search, show search bar
4. Update all hint bars to show `[tab]` bindings
5. Ensure `m` still works for mode toggle when table focused

**Exit criteria:** Tab toggles focus. Mode switch discoverable. Return from detail focuses search.

### Phase P6B-4: Album Art Hardening
**Files:** `internal/tui/art.go`, `internal/tui/browse.go`

1. Improve `detectProtocol()` with multiplexer checks
2. Add ESC-character validation in `encodeImage()` output
3. Add `getArtDisplay()` sanity check for encoded art string
4. Add `TIMBRE_NO_ART` env var escape hatch
5. Ensure placeholder always renders cleanly

**Exit criteria:** Art falls back to placeholder when protocol detection is wrong. No garbled output.

### Phase P6B-5: Default Sort + Display Updates
**Files:** `internal/db/queue.go`, `internal/tui/browse.go`

1. Change `SearchAlbums()` empty-query ORDER BY to `a.downloads DESC`
2. Change `SearchCompletedTracks()` empty-query ORDER BY to use album downloads
3. Add "Downloads" column to album table columns
4. Add "Avg Q" column to album table columns
5. Show downloads + avg quality in album detail header
6. Format download counts with K/M suffixes (e.g., 1.2M, 45K)

**Exit criteria:** Empty browse shows top albums by downloads. All new columns visible. Build succeeds.

### Phase P6B-6: Verification
1. Run `go test ./...` — all tests pass
2. Run `make build` — binary builds
3. Manual testing:
   - Tab key toggles search/table focus
   - `m` switches Albums/Tracks mode
   - Album art shows placeholder (not garbled) in unsupported terminals
   - Download counts visible in album list
   - Avg quality visible in album list and detail
   - Empty browse list sorted by downloads
   - Search → album detail → Esc → search re-focused

---

## Open Questions

1. **Download count formatting:** Use raw numbers or abbreviate (45,231 vs 45K)? Recommend: abbreviated with tooltip-style full number in detail view.
2. **Per-track downloads:** IA only tracks downloads at the item level. Should we show album downloads on each track row, or only on album views? Recommend: album-level only, shown in album list and detail header.
3. **Art protocol testing:** Need to verify behavior in iTerm2, Kitty, WezTerm, Ghostty, Terminal.app, and inside tmux. The `TIMBRE_NO_ART` env var provides an escape hatch.
4. **Existing data backfill:** Albums already in the DB won't have download counts. Should we backfill via a one-time migration that queries IA? Recommend: defer to future phase; existing albums show 0 downloads and sort below new ones.

---

## Files Modified Summary

| File | Changes |
|------|---------|
| `internal/db/db.go` | Add `downloads` column migration |
| `internal/db/queue.go` | `AlbumResult.Downloads`, `AlbumResult.AvgQuality`, updated queries, sort order |
| `internal/ia/types.go` | `ScrapeItem.Downloads` field |
| `internal/ia/scrape.go` | Add `fields` param to scrape URL |
| `internal/tui/browse.go` | Tab keybinding, mode hints, columns, detail header, navigation fixes |
| `internal/tui/art.go` | Protocol detection hardening, output validation, env var override |
| `cmd/tui/main.go` | Pass download counts to `BulkInsertAlbums()` |
