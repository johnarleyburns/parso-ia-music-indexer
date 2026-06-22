# Decisions — Unavailable vs Failed

## Decision 1: New status value vs. separate column

**Options:**
- A: Add `unavailable` as a new valid status value in CHECK constraints
- B: Add a `failure_category` column (NULL unless status='failed', then 'transient' or 'permanent')
- C: Encode category in `error_message` (prefix like "PERMANENT:")

**Decision: A — new status value `unavailable`.**

Rationale:
- Queries already `GROUP BY status` — no compound conditions needed
- CHECK constraints clearly define the state space
- Directly inspectable in SQLite without decoding error messages
- Separates `ResetAllFailed` and retry logic naturally (`WHERE status = 'failed'` already excludes `unavailable`)

Cost: SQLite migration requires table recreation (CREATE new → INSERT → DROP old → RENAME). This is a well-known pattern and the tables are small (thousands of rows, not millions).

## Decision 2: Separate event types vs. reusing EventAlbumFailed

**Options:**
- A: New `EventAlbumUnavailable` and `EventAnalysisUnavailable` event types
- B: Reuse existing `EventAlbumFailed`/`EventAnalysisFailed` with a flag field

**Decision: A — separate event types.**

Rationale:
- Dashboard/feed rendering distinguishes the two cases (different colors/styles)
- Avoids breaking existing log consumers that expect `failed` semantics
- Event routing is simpler (no flag inspection)

## Decision 3: Dashboard display

**Options:**
- A: Show Unavailable as a separate count in the stats panel
- B: Show Unavailable + Failed combined as a single metric

**Decision: A — separate count.**

Rationale:
- Users want to know how much content is permanently dead vs. temporarily failing
- Different retry behavior needs visual distinction
- Uses muted/gray color distinct from red (Failed) and green (Completed)

## Decision 4: Retry behavior for exhausted retries

**Options:**
- A: Exhausted retries go to `failed` (retryable via `ResetAllFailed`)
- B: Exhausted retries go to `unavailable` (permanently dead)

**Decision: A — exhausted retries go to `failed`.**

Rationale:
- After 3 retries of a transient error, the error might still be temporary (e.g., a multi-hour API outage)
- `ResetAllFailed` provides a manual recovery path
- This is consistent with the existing behavior

## Decision 5: `FlagAlbumPoorQuality` — album vs. individual track

**Options:**
- A: Mark the album and all pending tracks as `unavailable` (current bulk behavior)
- B: Mark only the failing track as `unavailable`, leave other tracks pending

**Decision: A — keep current bulk behavior, but mark as `unavailable`.**

Rationale:
- If one track on an album has a corrupted MP3, the other tracks from the same source are likely also corrupted
- Bulk marking prevents analyzers from wasting time on the remaining tracks
- Consistent with the existing `FlagAlbumPoorQuality` behavior
