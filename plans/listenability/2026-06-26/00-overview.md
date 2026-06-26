# Listenability Scoring Plan

## Problem

The indexer currently has `quality_score`, but that score measures technical audio quality. It does not answer whether a row is a listenable music track for search, recommendation, or playback.

The immediate product problem is avoiding tracks that are technically decodable but musically poor candidates:

- Very short clips, especially below 60 seconds.
- Albums whose average track duration is only a few seconds, which usually indicates sound effects, stems, channel dumps, rudiments, loops, or metadata artifacts.
- Spoken-word, audiobook, language-learning, test-tone, applause, silence, noise, or other non-music content that can still pass MP3 and quality checks.
- Existing completed tracks that were already embedded before this concept existed.

## Current Behavior

The current ingestion path is:

- `resolveAlbum()` calls `ia.LookupAlbumMetadata()` and filters access-restricted or non-music albums via `ia.IsMusicContent()`.
- `LookupAlbumMetadata()` accepts MP3-like files, filters very long files with `MaxTrackDurationSec = 32 * 60`, and stores duration/bitrate on `tracks`.
- `workerLoop()` claims pending tracks via `db.ClaimNextTrackBatch()`.
- `analyzeTrack()` streams audio, decodes MP3, computes `quality_score`, rejects tracks below `audio.QualityUnusable`, computes MFCC/chroma/CLAP, saves the embedding, then marks the track `completed`.
- `track_embeddings.quality_score` is the only scored suitability field.

Important existing fields:

- `tracks.duration`
- `tracks.bitrate`
- `tracks.title`
- `tracks.filename`
- `tracks.tags`
- `albums.subjects`
- `albums.genres`
- `track_embeddings.clap`
- `track_embeddings.quality_score`
- `track_embeddings.sample_strategy`

Local DB snapshot on 2026-06-26:

- About 235k tracks and about 37.9k completed/analyzed tracks.
- 89,854 total tracks have positive duration under 60 seconds.
- 94,807 total tracks have positive duration under 90 seconds.
- Among completed tracks, about 17,982 are under 60 seconds and about 18,852 are under 90 seconds.
- Positive-duration percentiles for all tracks: p05 2s, p10 3s, p25 7s, p50 149.64s, p75 313.44s, p90 729.24s.
- Positive-duration percentiles for completed tracks: p05 2s, p10 2s, p25 4s, p50 82.94s, p75 266.04s, p90 701.43s.
- Among albums with at least 5 tracks, 5,595 have average positive track duration under 60 seconds and 5,661 are under 90 seconds.
- Examples include channel/stem-like albums such as `20130411_Gilat-Show` with `Ch 1`, `Ch 2`, etc. at about 0.03 seconds each.

The local distribution argues against using 90 seconds as a hard cutoff immediately. It would affect roughly half of completed positive-duration tracks. The default stream policy should still hard-exclude tracks under 60 seconds, but cleanup should first run score-only so the affected catalog slice can be audited before historical statuses are mutated.

## Research Findings

Prior-art signals that map well to this codebase:

- Duration is a primary metadata signal. Spotify exposes `duration_ms` as a first-class audio feature and also models traits such as `speechiness`, `instrumentalness`, `liveness`, and `energy` for music suitability and retrieval-style behavior: https://developer.spotify.com/documentation/web-api/reference/get-audio-features
- Loudness and playback consistency are separate from content suitability. EBU R 128 is a loudness-normalization recommendation, which reinforces keeping technical audio quality separate from listenability/content scoring: https://tech.ebu.ch/publications/r128
- Audio taxonomies distinguish music, speech, silence, sound effects, environmental sound, and other non-song events. AudioSet is useful prior art for treating "music" as one class among many audio events rather than assuming every decodable audio file is a song: https://research.google.com/audioset/ontology/index.html
- CLAP-style contrastive audio-text embeddings are designed for text/audio matching. This repo already stores normalized CLAP audio vectors and exposes text embeddings, so prompt comparisons can classify existing analyzed tracks without re-downloading audio: https://arxiv.org/abs/2211.06687

Project-specific findings:

- The sidecar already supports `GetTextEmbedding()`, so a listenability cleaner can cache prompt vectors once per run and score existing `track_embeddings.clap` rows by dot product.
- The analyzer already has the CLAP vector in memory before saving, so new tracks can receive the same prompt-based content component without extra audio processing.
- The resolver has metadata for the whole album and is the right place to compute album-shape evidence.
- Some duration metadata appears suspiciously short for real-looking classical albums. Therefore the design must preserve reasons/components and start with score-only cleanup.

## Design Proposal

Add a first-class listenability score separate from `quality_score`.

Definition:

`quality_score` answers: "Is this audio technically usable?"

`listenability_score` answers: "Is this likely a complete, playable music track we want in music search/playback?"

Recommended default stream policy:

- Require an audio URL.
- Require a known positive duration.
- Prefer tracks from 90 seconds through 15 minutes.
- Hard-exclude tracks under 60 seconds by default.
- Hard-exclude obvious Non-Music, Special Effects, Speech, Audiobook, and probably Story content.
- Downrank, but do not always exclude, tracks from 15 minutes through 25 minutes.
- Treat 15-25 minute tracks as candidates for a future Longform surface instead of normal default-stream content.
- Exclude tracks above 25 minutes from the default stream unless a future Longform mode explicitly includes them.
- Store scores and reasons for every evaluated track.
- Run the cleanup worker in score-only mode first.
- Only mark completed tracks unavailable after a dry-run report is reviewed.

Scoring model v1:

- Duration component, weight 0.30.
- Album-shape component, weight 0.25.
- Content-type component, weight 0.25.
- Technical quality component, weight 0.15.
- Metadata hygiene component, weight 0.05.

Suggested duration curve:

- Missing or zero duration: default-stream exclude/withhold because duration is required; do not mutate historical status solely on this without a metadata refresh path.
- 0-60 seconds: 0.00 and default hard exclude.
- 60-90 seconds: ramp from 0.65 to 0.90.
- 90-900 seconds: 1.00.
- 900-1500 seconds: ramp down from 0.75 to 0.45 and mark as longform candidate.
- Above 1500 seconds: default-stream exclude unless Longform is enabled; the resolver currently filters only above 1920 seconds.

Suggested tiers:

- `excellent`: score >= 0.85.
- `good`: score >= 0.70.
- `borderline`: score >= 0.50.
- `poor`: score >= 0.25.
- `unusable`: score < 0.25.

Suggested decisions:

- `include`: score >= 0.50 and no default-stream hard rule triggered.
- `demote`: 0.25 <= score < 0.50, or 15-25 minute longform candidate in default stream.
- `exclude`: score < 0.25 or hard rule triggered.
- `unknown`: insufficient evidence.

## System Changes

Additive changes only:

- Add listenability columns to `tracks`.
- Add listenability columns to `albums`.
- Add an `internal/listenability` package for score calculation.
- Extend album resolution to compute album-shape context.
- Extend analyzer to score new tracks before and after CLAP embedding.
- Add a listenability cleaner worker for already-analyzed tracks.
- Add stats/reporting so cleanup can be calibrated.
- Surface listenability anywhere quality/search ranking is already visible: Collections, Browse, Player, recommendations/similar tracks, analyzer logs, and cleaner logs.

No deletion of existing embeddings is required. If the cleaner marks a track unavailable, searches already exclude it because `SearchByText()` filters `t.status = 'completed'`.

## Implementation Steps

1. Add schema columns and DB accessors.
2. Add pure scoring functions and unit tests.
3. Add album-shape computation during album resolution.
4. Add metadata-only precheck in analyzer to skip obvious low-listenability tracks before streaming.
5. Add full analyzer scoring after CLAP embedding.
6. Add cleaner worker for completed tracks with missing or stale listenability version.
7. Add dry-run and report output.
8. Add TUI visibility for listenability scores.
9. Add optional status mutation after calibration.
10. Add search/export filtering only after scores have been backfilled enough to avoid surprising gaps.

UI/logging requirements:

- Collections tab: show average listenability for the collection or album row alongside analysis counts and quality-like stats. The displayed value should be the average of scored tracks, with a blank/placeholder when no tracks have current-version listenability.
- Browse album list: show average listenability for albums.
- Browse track list/detail/search results: show per-track listenability.
- Player: show the current track listenability number in the track stats area.
- Recommendations/similar tracks: show listenability next to quality and distance/similarity so poor-listenability results are easy to spot.
- Analyzer logs: include listenability score, tier/decision, stream classification, and top reason when a new track is scored or excluded.
- Cleaner logs: include listenability score, tier/decision, stream classification, action taken, and top reason for each cleaned row or summarized batch.

## Testing Strategy

Use unit tests for the score curves and integration tests for DB migration/claim/update behavior.

Verification after implementation:

```sh
make build
go vet ./...
go test -race -count=1 ./...
```

Before any user verification, build `bin/timbre` and report the full test count.

## Open Questions

- Confirm whether cleanup should ever mark completed tracks `unavailable`, or whether the first implementation should only score and filter at query/export time.
- Confirm the default user-facing threshold: recommended `score >= 0.50`, with `score < 0.25` eligible for unavailable marking.
- Confirm whether short but legitimate genres should be kept via whitelist signals or simply demoted.
