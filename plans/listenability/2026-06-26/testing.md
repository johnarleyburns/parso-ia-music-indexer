# Listenability Testing Plan

## Problem

Listenability changes can remove or demote many rows. Testing must prove both correctness of scoring and safety of cleanup behavior.

## Current Behavior

Existing tests cover:

- Audio quality and chroma calculation.
- DB collections/tags/pill behavior.
- Hybrid fusion and playlist behavior.
- CLAP client mocks.
- TUI metric helpers.

There are no tests for listenability because the concept does not exist yet.

## Research Findings

Risk areas:

- Duration thresholds are policy-heavy and need explicit fixtures.
- Album-shape scoring is easy to get wrong with zero or missing durations.
- Cleaner workers need concurrency-safe claiming.
- Prompt scoring uses normalized vector math; tests should not require the real sidecar.
- Marking tracks unavailable changes search results indirectly through existing status filters.
- UI result structs can silently omit listenability even when DB fields exist.
- Logs can become unhelpful if they include only the score without decision/reason context.

## Design Proposal

Add three test layers:

1. Pure unit tests for `internal/listenability`.
2. SQLite integration tests for migrations, evidence loading, claims, updates, and idempotency.
3. Orchestration tests with mock CLAP vectors for analyzer/cleaner behavior.

## System Changes

New likely test files:

- `internal/listenability/listenability_test.go`
- `internal/db/listenability_test.go`
- Possibly focused tests near analyzer helpers if scoring is factored out of `cmd/tui/main.go`.

Recommended fixtures:

- `duration_5s`: hard exclude.
- `duration_45s_music_album`: default hard exclude by duration.
- `duration_75s_music_album`: borderline/good, not hard exclude.
- `duration_180s_music_album`: include.
- `duration_16m_music_album`: longform candidate, downranked but not unavailable.
- `duration_26m_music_album`: excluded from default stream unless Longform is enabled.
- `missing_audio_url`: default hard exclude.
- `missing_duration`: withheld from default stream.
- `album_channel_dump`: 25 tracks, average 0.03s, titles `Ch 1` through `Ch 25`, exclude.
- `album_sound_effects`: many 1-5s clips, exclude.
- `album_legit_short_punk`: short tracks but strong music prompt evidence, demote/include depending threshold.
- `album_missing_duration`: excluded from default stream until duration is resolved.
- `spoken_word_prompt_margin`: exclude when negative prompt strongly wins.
- `story_metadata`: probably exclude from default stream.

## Implementation Steps

1. Add table-driven tests for duration curve.
2. Add table-driven tests for album shape.
3. Add title/filename pattern tests.
4. Add score composition tests.
5. Add migration test for fresh DB.
6. Add migration test for legacy DB without listenability columns.
7. Add cleaner claim/update tests.
8. Add status mutation tests for `score-only` versus `mark-unavailable`.
9. Add search regression test after query filtering phase.
10. Add UI/model tests or focused result-query tests proving listenability is populated for Collections, Browse, Player, and recommendations.
11. Add log/event tests where practical, or at minimum assert activity event messages/details include score and primary reason in analyzer/cleaner paths.

## Testing Strategy

Specific assertions:

- 5s duration returns `exclude`.
- 45s duration returns default-stream `exclude`.
- 75s duration scores higher than 45s and lower than 180s.
- 16m duration is `longform_candidate` and not automatically unavailable.
- Missing audio URL and missing duration are excluded from default stream.
- Albums with avg duration < 15s and short60 ratio >= 0.80 produce hard album-shape reasons.
- Missing duration does not necessarily mutate historical status, but is not eligible for default stream.
- Prompt margin where negative > positive lowers score.
- Prompt margin where positive > negative can help 60-90 second tracks, but cannot rescue tracks under 60 seconds from the default-stream hard exclude.
- Cleaner does not re-claim rows already scored with current version.
- Cleaner re-claims stale-version rows.
- Cleaner leaves embeddings intact.
- Collection stats expose average listenability from scored tracks.
- Album and track browse result queries expose listenability.
- Player/recommendation result structs carry listenability.
- Analyzer and cleaner activity events include score, decision, stream, and top reason.

Full verification command:

```sh
make build
go vet ./...
go test -race -count=1 ./...
```

## Open Questions

- Whether to add a small golden dry-run report checked into `plans/listenability/2026-06-26/` after implementation.
- Whether to add a benchmark for cleaner throughput over decoded f16 vectors.
