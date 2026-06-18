# Player Enhancements

## Problem
Player tab lacks: previous track navigation, album navigation, track metadata display, and similarity visualization.

## Research: Common TUI Audio Player Keybindings

Surveyed cmus, ncmpcpp (MPD), and mocp:

| Action        | cmus   | ncmpcpp | mocp   | timbre (current) | timbre (new)    |
|---------------|--------|---------|--------|------------------|-----------------|
| Play/Pause    | c      | space   | space  | space             | space (no change)|
| Stop          | v      | s       | s      | s                 | s (no change)   |
| Next track    | b      | >       | n      | n                 | n / > (add >)   |
| Prev track    | z      | <       | b      | —                 | b / < (add)     |
| Seek fwd      | right  | right   | right  | right             | right (no change)|
| Seek back     | left   | left    | left   | left              | left (no change) |
| Volume up     | +/=    | +       | >      | up/+/=            | up/+/= (no change)|
| Volume down   | -      | -       | <      | down/-            | down/- (no change)|

Decision: Use `b`/`<` for previous (mocp/ncmpcpp standard), `>` as next alias. Add `a` for go-to-album.

## Implementation

### Phase 1: Keybindings
- `b` / `<` — previous track (if currentIdx > 0 in queue)
- `>` — next track alias
- `a` — switch to Browse tab, album detail for current track

### Phase 2: Track Stats
- On track play, async fetch: quality_score (via GetEmbedding), album collection/creator (via GetAlbumByID)
- Display below volume bar

### Phase 3: Similar Tracks
- On track play, async fetch: QuerySimilar(trackID, 10)
- Display as compact list below track info

### New Messages
- `playerStatsMsg` — carries quality, collection, creator, similar tracks
- `SwitchToAlbumMsg` — tells main model to switch to Browse tab with album detail

### Files Changed
- `internal/tui/player.go` — all changes
- `internal/tui/model.go` — handle SwitchToAlbumMsg
- `internal/tui/browse.go` — SwitchToAlbumMsg type definition
