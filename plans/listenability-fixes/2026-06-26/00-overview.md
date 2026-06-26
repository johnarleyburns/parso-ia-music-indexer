# Listenability False Positive Fixes — Overview

## Problem

The listenability system is over-eagerly excluding legitimate music from the catalog. Three
distinct false-positive patterns were identified:

1. **"Field Recording" metadata false positive** — Entire albums of world/folk/traditional
   music are excluded because IA metadata tags them with "Field Recording" (an
   ethnomusicological methodology term). `IsNonMusicMetadata()` treats this term as
   non-music and `classifyStreamDecision()` hard-excludes the tracks regardless of
   their individual quality scores.

2. **Live music channel-dump penalization** — Live concert recordings from the AJC/eTree
   project often have every channel uploaded as a separate short (1–5 second) file.
   The `AlbumShapeScore` punishes the entire album when 80%+ of tracks are <60s and
   average duration <30s, excluding all tracks — including any that might be longer.

3. **Classical long-form cutoff** — `LongformMaxSeconds = 1500` (25 min) is too strict
   for classical concertos where a single LP-side movement commonly exceeds 25 minutes.
   Tracks scoring "borderline" (0.61–0.68) are hard-excluded solely on duration.

## Current Behavior

- `IsNonMusicMetadata()` (listenability.go:353) checks 14 terms including "field recording"
  and "environmental sound"
- `classifyStreamDecision()` (listenability.go:397) hard-excludes if `IsNonMusicMetadata()`
  returns true, before even checking the score
- `AlbumShapeScore()` produces near-zero scores for albums with avg duration <30s and
  >80% short tracks
- `LongformMaxSeconds = 1500` excludes everything above 25 minutes
- Cleaner goroutine (`safeGo`) does not restart after a panic

## Data

| Issue | Tracks Affected | Breakdown |
|-------|----------------|-----------|
| Field Recording false positives | 532 total, 119 excellent/good | 9 world/folk albums entirely excluded |
| Live music channel dumps | 10,887 total, 8,939 at 1-5 sec | AJC/eTree project uploads with channel-dump metadata |
| Classical long-form (>25min) | ~6 today | Concerti grossi, symphonies, violin concertos |

Key false-positive albums (excellent/good scoring tracks excluded by "field recording"):

- The Nonesuch Explorer — Music From Distant Corners Of The World (25 tracks)
- Tribology — Music of Tribal People of Orissa State (21 tracks)
- African Music (12 tracks)
- Gamelans De Bali (11 tracks)
- Music Of Indonesia (10 tracks)

## Research Findings

- "Field recording" in ethnomusicology and archival contexts is a **recording methodology**
  descriptor, not a content type. It does NOT indicate non-music.
- "Environmental sound" can describe ambient music or natural soundscapes used in music.
- The AJC (Aaron Adam Jacobs) / eTree project uploads multi-channel concert recordings
  where each track is an individual WAV/MP3 file per channel, producing very short files.
  The `channel_dump_title_pattern` reason already catches these at the title level.
- Classical music labels (Concerti Grossi, Violin Concertos, etc.) routinely have movements
  exceeding 25 minutes on a single LP side. `LongformMaxSeconds = 1500` is appropriate
  for pop/rock but too aggressive for classical.

## Design Proposal

1. **Remove "field recording" and "environmental sound" from `IsNonMusicMetadata()`**.
   Keep all other terms. This eliminates 532 false-positive exclusions.

2. **Add a "channel dump" album exception**: When an album already matches
   `album_short60_ratio_above_80pct` AND `album_avg_duration_below_30s`, check if
   the album has `channel_dump_title_pattern` or `channel_dump_*` patterns. If so,
   demote to longform_candidate instead of hard-excluding. This preserves the ability
   to exclude genuinely broken albums while not destroying legitimate live recordings.

3. **Raise `LongformMaxSeconds` to 2700 (45 min)** for classical music tolerance.
   Adjust `PreferredMaxSeconds` to 1800 (30 min). The duration scoring curve shifts
   to accommodate longer works without removing the penalty for absurdly long files.

4. **Cleaner goroutine auto-restart**: When `safeGo` recovers from a panic, emit a
   restart event on the control channel to spawn a replacement goroutine. This prevents
   the cleaner from staying dead indefinitely.

## System Changes

- `internal/listenability/listenability.go` — Updated constants, `IsNonMusicMetadata()`,
  `classifyStreamDecision()`, `ScoreAlbum()`
- `internal/listenability/listenability_test.go` — Updated test expectations, new tests
- `cmd/tui/main.go` — Auto-restart logic for panicked goroutines
- Version bump: `listenability-v2`
