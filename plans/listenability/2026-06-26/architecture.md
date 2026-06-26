# Listenability Architecture

## Problem

Listenability requires evidence from multiple layers:

- Album metadata and album-wide shape.
- Track metadata such as duration, title, filename, bitrate, and tags.
- Existing technical quality metrics.
- CLAP semantic evidence for already analyzed tracks.
- Cleanup of historical rows without re-downloading audio.

The architecture must avoid mixing these concerns into `audio.quality` or the embedding code.

## Current Behavior

Relevant boundaries today:

- `internal/ia` parses Internet Archive metadata and applies coarse music/non-music checks.
- `internal/db` owns schema, queue claiming, embedding persistence, and search queries.
- `internal/audio` owns technical audio analysis: SNR, centroid, crest, MFCC, chroma, decode, and stream helpers.
- `internal/clap` owns the sidecar client.
- `cmd/tui/main.go` currently orchestrates workers directly.

The analyzer is already doing too much orchestration inline, so listenability should be factored into a small package with pure scoring logic rather than growing `analyzeTrack()` further.

## Research Findings

Useful architecture patterns from the repo:

- `quality_score` is computed in `internal/audio`, but stored through `internal/db`. Keep that same split: pure calculation outside SQL, persistence inside `internal/db`.
- `tagEnhancerLoop()` is a good precedent for a background worker that improves already-existing rows without re-running the full analyzer.
- `ResetTracksWithStrategy()` shows the repo already accepts incremental, resumable cleanup/migration jobs.
- Headless stats already expose `embedding_strategies` and `lexical_coverage`; listenability coverage can follow that pattern.

Useful external pattern:

- Audio event taxonomies such as AudioSet separate music from other audio events. This supports a content-classification component rather than assuming every archive audio row is suitable music.
- CLAP enables text/audio prompt scoring, which is a low-cost way to classify already embedded tracks.

## Design Proposal

Create `internal/listenability`.

Core types:

```go
type TrackEvidence struct {
    TrackID      int
    AlbumID      string
    Title        string
    Filename     string
    DurationSec  float64
    BitrateKbps  int
    QualityScore float64
    Tags         string
    Album        AlbumEvidence
    Prompt       PromptEvidence
}

type AlbumEvidence struct {
    AlbumID             string
    Title               string
    Creator             string
    Subjects            string
    Genres              string
    TrackCount          int
    PositiveDurationCnt int
    AvgDurationSec      float64
    MedianDurationSec   float64
    TotalDurationSec    float64
    Short30Ratio        float64
    Short60Ratio        float64
    Short90Ratio        float64
}

type PromptEvidence struct {
    MusicSimilarity       float64
    NegativeSimilarity    float64
    MusicMinusNegative    float64
    StrongestNegativeName string
}

type Result struct {
    Score      float64
    Tier       string
    Decision   string
    Stream     string
    Reasons    []string
    Components map[string]float64
    Version    string
}
```

Core functions:

- `ScoreAlbum(e AlbumEvidence) Result`
- `ScoreTrack(e TrackEvidence) Result`
- `DurationScore(seconds float64) (score float64, confidence float64)`
- `AlbumShapeScore(e AlbumEvidence) Result`
- `ContentPromptScore(e PromptEvidence) float64`
- `TitlePatternReasons(title, filename string) []string`

Prompt scoring:

- Positive prompts:
  - "a complete music track"
  - "a full song with melody and rhythm"
  - "an instrumental music performance"
  - "a recorded musical performance"
- Negative prompts:
  - "a short sound effect"
  - "spoken word audiobook narration"
  - "language lesson speech"
  - "applause and crowd noise"
  - "silence or test tone"
  - "isolated drum hit or instrument sample"
  - "multitrack channel stem"

Default stream hard filters:

- Missing audio URL.
- Missing, zero, or invalid duration.
- Duration under 60 seconds.
- Strong metadata or prompt evidence for Non-Music, Special Effects, Speech, Audiobook, or Story.

Default stream downrank filters:

- Duration from 15 minutes through 25 minutes.
- Longform-like metadata that is still plausibly music.
- These rows should receive `Stream = "longform_candidate"` so a later Longform surface can expose them intentionally.

Cache prompt vectors once per process. Use dot products against normalized `track_embeddings.clap` or the in-memory `clapVec`.

## System Changes

Add orchestration points:

- Resolver:
  - After `LookupAlbumMetadata()`, compute album shape from `album.Tracks`.
  - Store album listenability fields.
  - Keep existing `ia.IsMusicContent()` hard filters.
- Analyzer:
  - Claim duration/bitrate/tags and album listenability context with each track.
  - Run metadata-only scoring before network download.
  - If hard-excluded by missing URL, missing duration, duration under 60 seconds, or obvious non-music content, store listenability and mark unavailable for new tracks without streaming.
  - After CLAP embedding, run full scoring with prompt evidence and stored `quality_score`.
  - Save listenability fields alongside embedding and completion status.
- Cleaner:
  - New `listenabilityCleanerLoop()`.
  - Claims completed tracks whose `listenability_version` is missing or stale.
  - Loads stored CLAP embeddings and album context.
  - Scores and writes listenability fields.
  - In default mode, does not change `tracks.status`.
  - In explicit cleanup mode, marks `exclude` tracks unavailable.
- TUI/search visibility:
  - Collections tab should show average listenability for each collection/album row where scored track data exists.
  - Browse album rows should show average listenability.
  - Browse track rows, album detail rows, and text-search rows should show per-track listenability.
  - Player track details should show current track listenability.
  - Recommendations/similar tracks should show listenability alongside quality and distance/similarity.
- Activity/log output:
  - Analyzer completion/unavailable events should include score, tier, decision, stream, and primary reason.
  - Cleaner events should include score, tier, decision, stream, action, and primary reason for row-level logs; batch logs should include counts by decision/tier.

Keep `quality_score` unchanged. Listenability uses it as one component.

## Implementation Steps

1. Add `internal/listenability` with pure logic and tests.
2. Add DB evidence structs and query helpers in `internal/db`.
3. Add prompt-vector cache helper using `clap.CLAPClient`.
4. Add resolver album scoring.
5. Add analyzer track scoring.
6. Add cleaner worker and TUI/headless controls.
7. Add stats/reporting and UI/log visibility.

## Testing Strategy

- Unit-test `internal/listenability` without SQLite or CLAP.
- Unit-test prompt scoring using synthetic normalized vectors.
- Integration-test DB claim/update helpers with in-memory SQLite.
- Analyzer tests can use `clap.NewMockClient()`.
- Cleaner tests should verify stale-version rows are claimed once and updated idempotently.

## Open Questions

- Whether the prompt list should be hardcoded in Go for v1 or stored in config.
- Whether cleaner controls should be TUI-only at first or also exposed as a headless one-shot command.
- Whether search should filter by `listenability_decision` immediately or only after the cleaner has backfilled existing rows.
