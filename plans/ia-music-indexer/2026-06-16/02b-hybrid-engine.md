# Phase 2B — Hybrid Recommendation Engine Core

**STATUS: COMPLETE (2026-06-16)**
**NOTE**: DB schema references in this document (`ia_identifier` PK, `catalog_queue`) are outdated. The current schema uses `albums → tracks → track_embeddings (track_id PK)`. See `architecture.md` (Revision 3) and `data-model.md` (Revision 2) for current schema.

## Problem

The current design uses a single 40-dimensional MFCC (mean+variance) vector for content-based music similarity. MFCC captures acoustic texture and timbre well, but it is a "blind" feature — it has no semantic understanding of mood, genre, or musical context. This causes failure modes like:

- **Acoustic blind spots**: An acoustic folk song and an ambient synth track may have similar MFCC profiles despite being entirely different genres/moods, because MFCC only sees spectral envelope shape.
- **Harmonic insensitivity**: Two songs in the same key/progression but different genres get lumped together by MFCC alone.
- **Semantic blindness**: A melancholic piano ballad and a cheerful pop song with similar instrumentation may be indistinguishable.

The solution is a **hybrid recommendation engine** that fuses three complementary feature streams into a single 564-dimensional vector:

1. **MFCC (40-dim)** — Acoustic texture, spectral envelope, energy distribution (25% weight)
2. **Chroma (12-dim)** — Harmonic profile, semitone energy distribution, key/mode awareness (15% weight)
3. **CLAP (512-dim)** — Deep semantic understanding of mood, genre, instruments, and music context via a neural audio model (60% weight)

Additionally, a **quality scoring engine** gates tracks before they enter the recommendation database, preventing unusable low-fidelity recordings from polluting results.

## Current Behavior

- Phase 2 is complete with a working database layer (`internal/db/`)
- `track_embeddings` table stores vectors as BLOBs — already supports arbitrary-length vectors
- `QuerySimilar` uses pure Go cosine distance — already works with arbitrary-length vectors
- No MFCC, chroma, CLAP, or quality scoring code exists
- No gRPC or Python sidecar infrastructure exists
- No vector fusion logic exists
- Phase 5 (Audio Analysis) was planned to implement MFCC + SNR only

## Research Findings

### MFCC (Mel-Frequency Cepstral Coefficients)
- Industry-standard for music information retrieval
- 20 bands × 2 statistics (mean, variance) = 40 dimensions
- Captures spectral envelope, timbral texture, and energy distribution
- Library: `github.com/zrma/go-mfcc` (pure Go)

### Chroma (Pitch-Class Profile)
- Maps arbitrary audio frequencies onto 12 Western semitone bins (C through B)
- Ignores octave — reveals harmonic structure regardless of register
- Captures key stability, chord progressions, and tonal centricity
- Computed via FFT → frequency→MIDI mapping → pitch class histogram
- 12 dimensions (one per semitone)

### Quality Scoring (Multi-Stage Gatekeeper)

The Internet Archive contains many 78rpm transfers with severe static, distortion, and low-fidelity. A composite quality score gates tracks before they enter recommendations.

**Three metrics combined into one score (0.0–1.0):**

| Metric | Weight | Purpose |
|---|---|---|
| SNR (Signal-to-Noise Ratio) | 0.50 | Primary filter. Identifies silent passages to estimate noise floor vs peak signal power. If hiss drowns the music, the track is objectively unusable. |
| Spectral Centroid | 0.30 | Measures "brightness" — the frequency below which a percentage of spectral energy is concentrated. Old/muddy transfers have energy concentrated below 3–5 kHz. |
| Crest Factor (Peak/RMS) | 0.20 | Identifies clipping and over-compression. Music that is a "brick" of distortion has abnormally low Peak/RMS ratio. |

**Composite formula:**
```
S = (0.50 × SNR_norm) + (0.30 × Centroid_norm) + (0.20 × Crest_norm)
```

**Kill switch:** If raw SNR < 10 dB → force score to 0.0 regardless of other metrics. This prevents tracks drowning in noise from being misclassified due to lucky brightness or crest factor values.

**Normalization:** Each sub-metric normalized to 0.0–1.0 via min-max clipping with fixed reference ranges:
- SNR: min=0 dB, max=40 dB
- Centroid: min=0 Hz, max=8000 Hz (music rarely has centroid above 8kHz for 30s chunks)
- Crest Factor: min=1.0 (brickwall), max=10.0 (highly dynamic)

**Quality tiers for recommendations:**
- Score > 0.7: "High Fidelity" — suitable for audiophile recommendations
- Score 0.3–0.7: "Historical/Vintage" — acceptable, may be tagged for filtering
- Score < 0.3: "Unusable" — discarded, marked as failed in the queue

This enables user-facing quality filtering (e.g., an "Audio Quality Slider" in future app settings).

### CLAP (Contrastive Language-Audio Pretraining)
- `laion/clap-htsat-fused` model — state-of-the-art for music understanding
- 512-dimensional normalized embedding vector
- Trained on audio-text pairs — understands semantic concepts like "energetic rock with distorted guitars" or "calm classical piano"
- Runs on GPU (CUDA/MPS) or CPU fallback
- Input: raw PCM audio at 48 kHz (strict requirement)
- Library: HuggingFace Transformers + PyTorch

### Architecture Considerations
- CLAP model cannot run in-process within Go (no viable Go ML runtime for this scale)
- gRPC is the optimal Go↔Python communication layer: binary protocol, zero-copy streaming, mature ecosystem
- Protobuf handles raw PCM byte transmission efficiently (no JSON serialization overhead)
- Python sidecar runs as a separate process, communicates via localhost gRPC

### Vector Fusion Strategy
- **Late Fusion**: Compute all three vectors independently, then combine
- Weights reflect the relative contribution to recommendation quality:
  - CLAP: 60% (semantic understanding is primary signal)
  - MFCC: 25% (acoustic texture provides production/energy context)
  - Chroma: 15% (harmonic profile is supplementary)
- Concatenation with scalar weighting: each sub-vector dimension multiplied by its weight
- Total: 512 + 40 + 12 = 564 dimensions

## Design Proposal

### Three Extraction Engines + One Fusion Engine

```
┌──────────────────────────────────────────────────────────────────┐
│                    HYBRID RECOMMENDATION ENGINE                    │
│                                                                    │
│  RAW PCM SAMPLES (float32[])                                       │
│       │                                                            │
│       ├──────────────────────────────────────────────┐             │
│       │                                              │             │
│  ┌────▼─────────────┐  ┌──────────────┐  ┌──────────▼──────────┐ │
│  │ MFCC Engine (Go) │  │Chroma (Go)   │  │ CLAP Engine (Python) │ │
│  │ internal/audio/  │  │internal/     │  │ python_sidecar/      │ │
│  │ mfcc.go          │  │audio/chroma  │  │ server.py            │ │
│  │                  │  │              │  │                      │ │
│  │ 20 bands mean    │  │ 12 semitones │  │ 512-dim CLAP vector  │ │
│  │ 20 bands var     │  │ energy dist   │  │ (mood/genre/semantic)│ │
│  │ = 40-dim         │  │ = 12-dim     │  │                      │ │
│  └──────┬───────────┘  └──────┬───────┘  └──────────┬───────────┘ │
│         │                     │                      │             │
│         │    × 0.25           │    × 0.15            │ × 0.60      │
│         └─────────────────────┼──────────────────────┘             │
│                               │                                    │
│                    ┌──────────▼──────────┐                         │
│                    │  FUSION ENGINE (Go)  │                         │
│                    │  internal/hybrid/    │                         │
│                    │  fusion.go           │                         │
│                    │                      │                         │
│                    │  Weighted Concat     │                         │
│                    │  512 + 40 + 12       │                         │
│                    │  = 564-dim vector    │                         │
│                    └──────────┬───────────┘                         │
│                               │                                    │
│                    ┌──────────▼───────────┐                        │
│                    │   SQLite BLOB Store   │                        │
│                    │   track_embeddings    │                        │
│                    │   (564 × 4 = 2256 B)  │                        │
│                    └──────────────────────┘                        │
└────────────────────────────────────────────────────────────────────┘
```

### gRPC Communication Flow

```
┌──────────────────┐         ┌─────────────────────┐
│   Go Binary      │  gRPC   │   Python Sidecar    │
│                  │◄────────│                     │
│  internal/clap/  │  proto  │  python_sidecar/    │
│  client.go       │         │  server.py          │
│                  │         │                     │
│  Send: PCM bytes │────────▶│  Receive PCM bytes  │
│  + sample_rate   │         │  + sample_rate      │
│                  │         │                     │
│  Receive: 512    │◄────────│  Run CLAP inference │
│  float32 vector  │         │  Return embedding   │
└──────────────────┘         └─────────────────────┘
```

Proto service definition (`proto/clap.proto`):
```protobuf
syntax = "proto3";
package clap;
option go_package = "./clap_proto";

service CLAPEmbedder {
  rpc GetEmbedding (EmbeddingRequest) returns (EmbeddingResponse);
}

message EmbeddingRequest {
  bytes pcm_data = 1;
  int32 sample_rate = 2;
}

message EmbeddingResponse {
  repeated float embedding = 1;
}
```

## System Changes

### New Packages / Files

```
internal/audio/                     # Audio feature extraction (NEW PACKAGE)
├── types.go                        # Shared types: PCMFormat, FeatureResult, QualityScore
├── decode.go                       # WAV decoder for test fixtures
├── mfcc.go                         # MFCC extraction (40-dim)
├── chroma.go                       # Chroma extraction (12-dim)
└── quality.go                      # Quality scoring: SNR, Spectral Centroid, Crest Factor → 0.0–1.0

internal/hybrid/                    # Vector fusion (NEW PACKAGE)
├── fusion.go                       # FuseFeatures(), weights, normalization
└── fusion_test.go                  # Tests for fusion output dimensions/values

internal/clap/                      # CLAP gRPC client (NEW PACKAGE)
└── client.go                       # CLAPClient interface, real + mock impls

proto/                              # gRPC service definitions (NEW)
└── clap.proto                      # Service + message definitions

python_sidecar/                     # Python inference server (NEW)
├── server.py                       # gRPC server with CLAP model
├── requirements.txt                # torch, transformers, grpcio, etc.
└── proto/                          # Generated Python proto stubs
    └── clap_pb2.py
```

### Modified Files

```
internal/db/embeddings.go           # Already supports arbitrary dims (no code change needed)
internal/db/db_test.go              # Add tests for 564-dim roundtrip + similarity
internal/config/config.go           # Add ClapHost, ClapPort fields
go.mod                              # Add gRPC + proto + audio DSP dependencies
```

### Database Schema

The existing `track_embeddings` table uses BLOB storage — no schema change needed:

```sql
CREATE TABLE IF NOT EXISTS track_embeddings (
    ia_identifier TEXT PRIMARY KEY,
    embedding     BLOB NOT NULL,       -- 564 × 4 = 2256 bytes for hybrid vector
    quality_score REAL,
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
```

### Dependency Changes

New Go dependencies:
```
google.golang.org/grpc                    # gRPC client
google.golang.org/protobuf                # Protobuf runtime
github.com/mjibson/go-dsp                 # FFT for chroma computation (or equivalent)
```

Python dependencies (for sidecar, not part of Go build):
```
torch>=2.0.0
transformers>=4.30.0
grpcio>=1.50.0
grpcio-tools>=1.50.0
numpy>=1.24.0
```

### Weight Configuration

Weights are constants in `internal/hybrid/fusion.go`, not runtime config:

```go
const (
    WeightCLAP   = 0.60  // Semantic mood, genre, context
    WeightMFCC   = 0.25  // Acoustic texture, energy, timbre
    WeightChroma = 0.15  // Harmonic profile, key/mode
)
```

Total dimensions: `512 + 40 + 12 = 564`

## Implementation Steps (Phase 2B)

### Step 1: Create `internal/audio/types.go`
- `FeatureResult` struct with `MFCC []float32`, `Chroma []float32`
- Utility types for PCM format

### Step 2: Create `internal/audio/mfcc.go`
- Port MFCC extraction from sample code using `go-mfcc` library
- `ComputeMFCCPool(samples []float32, sampleRate int) []float32`
- Returns 40-dim vector (20 mean + 20 variance)
- Add `go-mfcc` dependency

### Step 3: Create `internal/audio/chroma.go`
- Port chroma extraction from sample code using FFT
- `ComputeChromaPool(samples []float32) []float32`
- Returns 12-dim vector (one per semitone)
- Uses `go-dsp/fft` or `go-dsp/dsputils` for FFT

### Step 4: Create `internal/audio/quality.go`
- Three raw metric extraction functions from PCM samples:
  - `CalculateSNR(samples []float32) float64` — SNR in dB
    - Identifies silent passages (bottom 10% energy frames) as noise floor
    - Peak signal power from top 90% energy frames
    - Returns `10 * log10(signal_power / noise_floor)`
  - `CalculateSpectralCentroid(samples []float32, sampleRate int) float64` — centroid in Hz
    - FFT → weighted frequency average across all frames
    - Low values (<3 kHz) indicate "muddy" recordings
  - `CalculateCrestFactor(samples []float32) float64` — peak/RMS ratio
    - Low values (<2.0) indicate heavy compression/clipping
- Composite scoring function:
  - `CalculateCompositeScore(snrDB, centroidHz, crestFactor float64) float64` — returns 0.0–1.0
  - **Kill switch**: if `snrDB < 10.0` → return 0.0 immediately
  - Normalization ranges: SNR [0, 40], Centroid [0, 8000], Crest Factor [1.0, 10.0]
  - Weights: SNR 0.50, Centroid 0.30, Crest Factor 0.20
  - All raw values clamped to [min, max] before normalizing to [0.0, 1.0]
- `QualityScore` struct with fields: `Composite float64`, `SNR float64`, `Centroid float64`, `CrestFactor float64`

### Step 5: Create `internal/audio/decode.go`
- WAV decoder for test fixtures (only needed for testing Phase 2B)
- `DecodeWav(filePath string) ([]float32, error)`
- Uses `github.com/go-audio/audio` and `github.com/go-audio/wav`

### Step 6: Create `proto/clap.proto`
- Service and message definitions for gRPC communication
- Document proto generation commands in comments

### Step 7: Create `internal/clap/client.go`
- `CLAPClient` interface:
  ```go
  type CLAPClient interface {
      GetEmbedding(ctx context.Context, pcmData []byte, sampleRate int32) ([]float32, error)
      HealthCheck(ctx context.Context) error
      Close() error
  }
  ```
- `NewGRPCClient(host string, port int) (*grpcCLAPClient, error)` — real gRPC implementation
- `NewMockClient() *mockCLAPClient` — for testing, returns deterministic 512-dim vector
- Config via `internal/config/config.go`

### Step 8: Create `internal/hybrid/fusion.go`
- `FuseFeatures(clap, mfcc, chroma []float32) []float32`
- Applies weights, concatenates
- Input validation (lengths must be 512, 40, 12)
- Returns 564-dim vector

### Step 9: Create `internal/hybrid/fusion_test.go`
- Test: FuseFeatures produces correct 564-dim output
- Test: weights applied correctly (verify with known inputs)
- Test: zero vectors → zero output
- Test: orthogonal input components → correct separation in output

### Step 10: Create `python_sidecar/server.py`
- Loads `laion/clap-htsat-fused` model via HuggingFace
- Hardware detection: MPS > CUDA > CPU
- gRPC server on port 50051
- `GetEmbedding` handler: reconstructs PCM, runs inference, returns embedding
- Graceful shutdown on SIGTERM

### Step 11: Create `python_sidecar/requirements.txt`
- torch, transformers, grpcio, grpcio-tools, numpy

### Step 12: Update `internal/config/config.go`
- Add `ClapHost string` (default `localhost`, env `CLAP_HOST`)
- Add `ClapPort int` (default `50051`, env `CLAP_PORT`)

### Step 13: Update `internal/db/db_test.go`
- Add test for 564-dim embedding roundtrip
- Add test for 564-dim similarity query
- Verify existing tests still pass (they already use 40-dim, should be unaffected)

### Step 14: Update `go.mod` with new dependencies
```
go get google.golang.org/grpc
go get google.golang.org/protobuf
go get github.com/mjibson/go-dsp
go get github.com/zrma/go-mfcc
go get github.com/go-audio/audio
go get github.com/go-audio/wav
```

## Testing Strategy

### Unit Tests
| File | Tests |
|---|---|
| `internal/audio/mfcc_test.go` | MFCC output is 40-dim; non-zero for audio input; zero for silence |
| `internal/audio/chroma_test.go` | Chroma output is 12-dim; non-zero for audio; 440Hz sine maps to A note bin |
| `internal/audio/quality_test.go` | SNR > 40 dB for pure sine; SNR < 5 dB for white noise; Centroid ~440 Hz for 440Hz sine; kill switch triggers at SNR < 10 dB; composite score in [0,1]; weights apply correctly |
| `internal/hybrid/fusion_test.go` | Output is 564-dim; weights verify; zero input → zero output; length mismatch panics |
| `internal/clap/client_test.go` | Mock client returns correct 512-dim vector; gRPC client connection test |
| `internal/db/db_test.go` | 564-dim roundtrip; 564-dim similarity query; mixed-dim vectors skipped gracefully |

### Integration Tests
- MFCC on a real WAV test fixture produces sensible output
- Chroma on a real WAV test fixture produces sensible output
- Quality scoring on sine/noise/silence fixtures produces correct scores
- Fusion of all three produces correct combined vector

### Test Fixtures Needed
- `data/testdata/sine_440hz.wav` — clean sine wave for chroma + centroid validation
- `data/testdata/silence_1s.wav` — silence for zero-vector + SNR floor tests
- `data/testdata/white_noise.wav` — white noise for SNR validation (should score < 0.3)
- (Real music WAV for manual/integration testing — not committed to repo)

### Run Tests
```bash
go test ./internal/audio/...
go test ./internal/hybrid/...
go test ./internal/clap/...
go test ./internal/db/...
```

## Exit Criteria

1. `go test ./internal/audio/...` passes — MFCC, Chroma, and Quality scoring extractors all work
2. `go test ./internal/hybrid/...` passes — Fusion produces correct 564-dim output
3. `go test ./internal/clap/...` passes — Mock client works; gRPC client compiles
4. `go test ./internal/db/...` passes — All existing tests pass, new 564-dim tests pass
5. `go build ./cmd/tui` compiles without errors
6. `proto/clap.proto` is complete and documented with generation commands
7. `python_sidecar/server.py` can be launched manually (optional for Phase 2B verification)
8. All three extraction engines (MFCC, Chroma, CLAP mock) produce output that FuseFeatures accepts
9. Quality scoring produces correct composite scores: sine wave > 0.7, white noise < 0.3, silence ≈ 0.0
10. Kill switch verified: SNR < 10 dB forces composite to 0.0
11. Database stores and retrieves 564-dim vectors correctly
12. Cosine similarity on 564-dim vectors returns correct distance values

## What Does NOT Go in Phase 2B

- Wiring the engines into the worker pipeline (Phase 5)
- Actually running the Python sidecar in production (Phase 5)
- Audio streaming from Internet Archive (Phase 5)
- MP3 decoding (Phase 5)
- Quality-score-based queue filtering (Phase 5 — workers check quality before marking "completed")
- Updating the TUI to show hybrid vectors or quality tiers (Phase 6 — Browse tab)
- Proto code generation (`protoc` step) — documented but not automated
- Python sidecar deployment in headless mode (Phase 5)

## Impact on Future Phases

### Phase 3 (Dashboard Controls)
No change. Phase 3 works with the DB layer as-is. The hybrid engine is not yet wired.

### Phase 4 (IA Scraping + Coordinator)
No change. The coordinator is unaware of vector dimensions.

### Phase 5 (Audio Analysis + Workers) — MODIFIED
Workers now perform the full pipeline:
1. Stream audio from IA (same as before)
2. Decode MP3 → PCM (same as before)
3. **Quality gate**: Compute composite quality score (uses engine from Phase 2B)
   - If score < 0.3: mark track as `failed` with `error_message = "low quality"`, skip embedding
   - If score >= 0.3: proceed to feature extraction
4. Extract MFCC → 40-dim (uses engine from Phase 2B)
5. Extract Chroma → 12-dim (uses engine from Phase 2B)
6. Send PCM to Python sidecar → 512-dim CLAP vector (uses client from Phase 2B)
7. Fuse all three → 564-dim hybrid vector (uses fusion from Phase 2B)
8. Store hybrid vector + quality score in DB (uses existing `SaveEmbedding`)

### Phase 6 (Browse / Search)
Vector similarity now operates on 564-dim hybrid vectors instead of 40-dim MFCC. The `QuerySimilar` function already handles arbitrary dimensions via BLOB storage + pure Go cosine distance, so **no code changes needed** for the similarity search itself. The Browse tab's display can show richer metadata (which features contributed to similarity) — optional enhancement.

### Phase 7 (Player)
No change. Player is unaware of vector dimensions.

## Open Questions

1. **go-mfcc library compatibility**: Does `zrma/go-mfcc` produce output comparable to librosa's MFCC? Mitigation: test with known audio fixtures and compare.

2. **FFT library choice**: Should chroma use `mjibson/go-dsp`, a custom implementation, or `github.com/mewpull/fft2`? Decision needed based on maintenance and API.

3. **WAV decoder choice**: `go-audio/wav` vs custom. `go-audio` is mature but has large dependency tree. Custom reader may be simpler for test-only use.

4. **Python sidecar lifecycle**: Who starts/stops the Python process? For development, manual start. For Phase 5, Go binary could spawn it as a subprocess. For production, separate deployment.

5. **CLAP model size and memory**: The htsat-fused model is ~300MB. Does it fit in unified memory on 8GB MacBooks? Test needed.

6. **gRPC proto generation**: Should proto Go bindings be checked into the repo or generated at build time? Checked in is simpler for CI.

7. **Mixed-dimension handling**: `QuerySimilar` currently skips vectors with different dimensions. With all vectors becoming 564-dim, this is a non-issue for new data. But when migrating, old 40-dim vectors will be skipped. Migration strategy needed if existing data exists.

8. **Quality scoring normalization ranges**: The fixed reference ranges (SNR [0,40], Centroid [0,8000], Crest [1,10]) may need calibration against real IA audio. Initial values are reasonable defaults; should tune after processing a sample of tracks.

9. **Quality gate threshold**: Is score < 0.3 truly the right cutoff for "unusable"? May need adjustment after evaluating real 78rpm recordings. The kill switch (SNR < 10 dB) handles the most obvious cases.
