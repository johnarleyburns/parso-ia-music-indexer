# Listenability Decisions

## Problem

Several listenability policies affect catalog size. These need to be explicit before implementation starts.

## Current Behavior

No listenability policy exists. Current exclusion happens only through album metadata checks, MP3/bitrate/duration upper-bound filtering, decode failures, and low technical quality.

## Research Findings

Local DB evidence makes a hard 90-second cutoff risky. About half of completed positive-duration tracks are below 90 seconds. A 60-second hard default-stream cutoff is the selected policy, but historical cleanup should still be calibrated with a dry-run report before mutating existing completed rows.

CLAP prompt scoring and album-shape scoring reduce false positives compared with duration alone.

## Design Proposal

### Decision 1: 60s or 90s?

Decision: hard-exclude tracks under 60 seconds from the default stream and use 90 seconds as the full-credit target.

Rationale:

- 90 seconds is too aggressive as a hard cutoff for the current catalog.
- 60 seconds aligns with the goal to avoid very short tracks while preserving more legitimate music than a 90-second hard cutoff.
- The score curve can still demote 60-90 second tracks relative to normal-length songs.

Proposed default:

```text
min_track_seconds = 60
target_track_seconds = 90
hard_exclude_seconds = 60
```

### Decision 1b: Default stream duration window

Decision: prefer 90 seconds through 15 minutes for the default stream. Downrank 15-25 minute tracks and tag them as Longform candidates. Exclude above 25 minutes from the default stream unless a future Longform mode explicitly includes them.

Rationale:

- 15-25 minute tracks can be legitimate mixes, movements, live recordings, or long pieces, but they are not ideal default stream items.
- Long tracks need a separate product surface and expectation.
- The current resolver allows up to 32 minutes, so listenability must add a stricter default-stream policy without destroying the underlying catalog.

### Decision 2: Score-only or mutate status?

Recommendation: first release cleaner in `score-only` mode.

Rationale:

- It is reversible and auditable.
- It lets us inspect counts and examples before removing rows from search.
- Later `mark-unavailable` mode can be explicit.

### Decision 3: Store on tracks, albums, or embeddings?

Recommendation: store track listenability on `tracks`, album listenability on `albums`, and leave `track_embeddings` focused on vectors and technical quality.

Rationale:

- Listenability is metadata/content suitability, not an embedding property.
- Album shape is first-class evidence.
- Queries can filter directly on `tracks`.

### Decision 4: Use CLAP prompts?

Recommendation: yes, but as one component, not the only classifier.

Rationale:

- Existing completed tracks already have CLAP audio vectors.
- The sidecar already has `GetTextEmbedding()`.
- Prompt vectors can be cached once per process.
- Duration and album-shape rules remain deterministic guardrails.

### Decision 5: What about missing or suspect durations?

Decision: missing, zero, or invalid duration is not eligible for the default stream because duration is required.

Rationale:

- Local data shows some real-looking albums with suspiciously tiny durations.
- IA metadata may be incomplete or inconsistent.
- Prompt and album evidence can still decide.
- Historical cleanup should not automatically mutate status solely because duration is missing; it should withhold from default stream and optionally queue metadata refresh.

### Decision 6: Non-music category exclusions

Decision: hard-exclude obvious Non-Music, Special Effects, Speech, Audiobook, and probably Story content from the default stream.

Rationale:

- The product is music search/playback.
- These categories can be technically high quality and still be bad music results.
- "Story" may overlap with musical theater, opera, or narrated musical works, so it should be reason-coded and reviewed after dry-run counts.

### Decision 7: UI and log visibility

Decision: listenability must be visible wherever quality or recommendation context is visible.

Required surfaces:

- Collections tab average listenability.
- Browse album average listenability.
- Browse track/list/detail listenability.
- Player current-track listenability.
- Recommendations/similar-track listenability.
- Analyzer and cleaner logs with score, tier/decision, stream, action, and primary reason.

Rationale:

- The score is not useful if it only exists in the database.
- During calibration, visible scores and reasons make false positives easy to find.
- Recommendations need the number beside quality/distance so low-listenability results can be diagnosed before ranking changes are enabled.

## System Changes

These decisions imply:

- Add score/reason storage before enabling status mutation.
- Add versioned scoring.
- Add dry-run reporting.
- Keep thresholds configurable.
- Add a `listenability_stream` or equivalent field so Longform candidates can be separated from default stream content.
- Add listenability fields to query result structs used by Collections, Browse, Player, and recommendations.

## Implementation Steps

1. Implement recommended defaults.
2. Run score-only cleaner.
3. Record dry-run counts in `current_state.md`.
4. Decide whether to enable `mark-unavailable`.
5. Decide when to update search/export filtering.

## Testing Strategy

Each decision gets a test:

- 60s threshold affects duration score.
- 90s target gives full or near-full duration credit.
- Score-only does not mutate status.
- Mark-unavailable mutates only explicit `exclude`.
- Missing duration is excluded from default stream but does not automatically mutate historical status.

## Open Questions

1. Should the first cleaner implementation expose `mark-unavailable`, or should that be a follow-up after a dry run?
2. Should search filtering wait until backfill reaches a minimum coverage percentage?
3. Are there specific short-form genres or archive collections that should be allowlisted?
