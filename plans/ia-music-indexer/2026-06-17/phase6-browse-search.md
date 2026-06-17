# Phase 6 — Browse / Search Tab

**STATUS: COMPLETE (2026-06-17). Superseded by Phase PX-2 (three-view browse with album art).**

## Date: 2026-06-17

## Problem

Users need to browse indexed tracks, search by identifier, view quality scores, and trigger vector similarity queries from the TUI.

## Current Behavior

Browse tab is a placeholder stub (`browse.go`). `QuerySimilar` in `embeddings.go` is already fully implemented with cosine distance. No DB function exists for listing/searching completed tracks with pagination.

## Design

### Components

1. **Search Input** (`bubbles/textinput`) — auto-focused on tab activation, filters results as user types
2. **Results Table** (`bubbles/table`) — shows IA Identifier, Quality Score columns; adds Distance column in similarity mode
3. **Similarity Mode** — pressing `v` on a row queries `QuerySimilar` and replaces table with top-N similar tracks

### Focus Model

- When search input is focused: all character keys go to textinput; `esc` blurs input and focuses table
- When table is focused: `↑/↓` navigate, `v`/`enter`/`p`/`/` handled as browse commands
- Global `q` quit is suppressed when search input is focused (only `ctrl+c` quits)

### DB Additions

- `SearchCompletedTracks(db, query, limit, offset)` — queries `catalog_queue` JOIN `track_embeddings` with LIKE filter, returns paginated results

### Key Bindings (table focused)

- `↑/↓` — navigate rows
- `enter` — queue track for Player tab, switch to Player
- `v` — show top 5 similar tracks (via QuerySimilar)
- `esc` — exit similarity mode (or blur search)
- `p` — play track immediately (same as enter)
- `/` — focus search bar

### Messages

- `browseSearchMsg` — carries search results from async DB query
- `SwitchToPlayerMsg` — signals model.go to switch to Player tab

## Files to Modify

- `internal/db/queue.go` — add `TrackResult`, `SearchCompletedTracks`
- `internal/tui/browse.go` — full implementation
- `internal/tui/model.go` — wire DB, handle input focus for global keys, handle SwitchToPlayerMsg

## Exit Criteria

Per implementation.md Phase 6 section.
