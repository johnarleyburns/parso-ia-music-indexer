# Phase 7 — Player Tab

## Date: 2026-06-17

## Problem

Implement an audio player tab that can stream and play IA MP3 tracks with playback controls (play/pause/stop/seek/volume) and a play queue. Audio must continue playing when switching tabs.

## Current Behavior

- `PlayerModel` is a placeholder with static text at tab index 3
- `SwitchToPlayerMsg` carries TrackID, Title, AlbumID, AlbumTitle, ArtURL, DownloadURL
- `model.go` only switches to tab 3 on `SwitchToPlayerMsg` — does not forward the message to PlayerModel
- No audio playback library in the project
- Existing `StreamAudioFromURL` downloads full MP3 bytes; `DecodeMP3` decodes to PCM float64
- `DownloadURL` in tracks table is already a complete direct URL

## Research Findings

- `gopxl/beep` is the maintained fork of `faiface/beep` for Go audio playback
- beep uses `oto` as backend (pure Go, no CGo for audio output)
- beep/mp3 can decode from `io.ReadCloser`; returns `StreamSeekCloser` (seekable)
- Known blocker #3: beep MP3 streaming needs seekable reader → use `bytes.Reader` over downloaded data
- beep's `speaker.Init` is called once; `speaker.Play` runs in background goroutine
- beep's `Ctrl` provides pause, `effects.Volume` provides volume control
- `speaker.Lock()/Unlock()` for thread-safe streamer manipulation

## Design

### Architecture

```
Browse Tab
    |
    | SwitchToPlayerMsg (TrackID, Title, DownloadURL, etc.)
    v
model.go → forwards msg to PlayerModel.Update()
    |
    v
PlayerModel
    ├── queue []QueueItem (track metadata)
    ├── engine *PlayerEngine (audio state)
    ├── state PlayState (loading/playing/paused/stopped)
    └── UI rendering (now-playing, progress bar, queue list, controls)

PlayerEngine (manages beep state)
    ├── streamer beep.StreamSeekCloser
    ├── ctrl *beep.Ctrl (pause/play)
    ├── volume *effects.Volume
    ├── format beep.Format
    └── speaker.Play() runs in background
```

### Message Flow

1. `SwitchToPlayerMsg` → enqueue track → if not playing, start loading
2. `loadTrackCmd(url)` → goroutine downloads MP3 → decodes with beep/mp3 → returns `playerLoadedMsg`
3. `playerLoadedMsg` → engine starts playback via `speaker.Play` → start tick timer
4. `playerTickMsg` (every 200ms) → update elapsed time from streamer position
5. `playerDoneMsg` → track finished → advance queue → load next or stop
6. `playerErrorMsg` → display error, advance queue

### Keybindings

- `space` → play/pause toggle
- `s` → stop
- `n` → skip to next in queue
- `←`/`→` → seek backward/forward 5s
- `+`/`-` → volume up/down (0.1 step, range 0.0–1.0)
- `c` → clear queue (keep current)

## Implementation Steps

### P7-1: Add beep dependencies
### P7-2: Create PlayerEngine (internal/tui/player_engine.go)
### P7-3: Implement PlayerModel UI + keybindings (internal/tui/player.go)
### P7-4: Create statusbar component (internal/tui/components/statusbar.go)
### P7-5: Wire SwitchToPlayerMsg forwarding in model.go
### P7-6: Graceful shutdown (stop speaker on quit)
### P7-7: Build + test verification

## Files to Create

- `internal/tui/player_engine.go` — audio playback engine
- `internal/tui/components/statusbar.go` — player status bar component

## Files to Modify

- `internal/tui/player.go` — full PlayerModel implementation
- `internal/tui/model.go` — forward SwitchToPlayerMsg, route player messages
- `go.mod` / `go.sum` — add beep dependencies

## Testing Strategy

- `make build` must succeed
- `go test ./...` must pass (no regressions)
- Manual verification: launch TUI, browse to a completed track, play it, verify audio output

## Open Questions

None — design is specified in implementation.md.
