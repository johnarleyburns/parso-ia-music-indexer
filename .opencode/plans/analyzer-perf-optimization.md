# Analyzer Processing Time Optimization — Implementation Plan

## Problem

The analyzer "processing" time (decode + quality + features) accounts for ~68% of per-track time. The root cause is redundant FFT computation: SNR, spectral centroid, and chroma each run independent FFT passes over the same 2048-sample frames. Adding more analyzer workers doesn't help because the `go-dsp/fft` radix-2 implementation already spawns `GOMAXPROCS` goroutines internally per FFT call, saturating all CPU cores.

## Measured Impact (estimates)

| Phase | Change | Expected reduction |
|-------|--------|-------------------|
| 1 | Unified FFT pass (SNR + centroid + chroma → 1 FFT/frame) | 30-40% |
| 2 | MP3 decode pre-allocation | 5-10% |
| 3 | Truncate PCM before Float32ToBytes | 2-5% |
| 4 | Limit feature extraction to 15s of audio | 10-20% |
| **Total** | | **~50-60% reduction in processing time** |

## Files to Modify

| File | Change |
|------|--------|
| `internal/audio/quality.go` | New unified `ComputeQualityAndChroma()` function; keep old functions as wrappers for test compat |
| `internal/audio/chroma.go` | Accept sampleRate parameter; delegate to unified function |
| `internal/audio/mp3decode.go` | Pre-allocate samples slice |
| `cmd/tui/main.go` | Use unified function; truncate PCM for features; truncate PCM for CLAP |
| `internal/audio/audio_bench_test.go` | NEW — benchmarks for all key operations |

---

## Phase 1: Unified FFT Pass

### Current State

Three independent FFT passes on 2048-sample frames:

```
quality.go:CalculateSNR()         → FFT per frame → magnitude bins → sort → signal/noise power
quality.go:CalculateSpectralCentroid() → FFT per frame → magnitude bins → weighted freq sum
chroma.go:ComputeChromaPool()     → FFT per frame → magnitude bins → MIDI note binning
```

For a 30s 44.1kHz track (~1.3M samples, ~647 frames): **1,941 FFT operations** for these three functions alone.

### Design

Create `ComputeQualityAndChroma(samples []float64, sampleRate int) QualityChroma` that:
1. Divides samples into `len(samples)/2048` full 2048-sample frames (floor division, ~647 frames)
2. Per frame: applies Hann window → one `fft.FFTReal` → computes magnitude spectrum → extracts:
   - **SNR**: total power, sort mags, noise = bottom half, signal = total - noise (accumulate)
   - **Centroid**: weighted frequency sum / mag sum (accumulate average)
   - **Chroma**: MIDI note binning from magnitude (accumulate per bin)
3. Returns `QualityChroma{SNR float64, CentroidHz float64, Chroma []float32}`

**Framing difference**: Old SNR code processes a potential partial last frame (ceil division). New code uses floor division. For typical tracks, this skips at most 1 frame out of 600+ — SNR impact <0.1 dB, negligible.

**Chroma bugfix**: Old code hardcodes 48000 Hz at `chroma.go:30`. New code uses actual `sampleRate` parameter. All tests use 48kHz so test output unchanged.

### Compatibility

Keep old functions as thin wrappers:
- `CalculateSNR(s)` → `ComputeQualityAndChroma(s, 48000).SNR`
- `CalculateSpectralCentroid(s, sr)` → `ComputeQualityAndChroma(s, sr).CentroidHz`
- `ComputeChromaPool(s)` → `ComputeQualityAndChroma(s, 48000).Chroma` (but also update signature to accept sampleRate — see below)

### ComputeChromaPool signature change

`ComputeChromaPool` currently takes only `samples`. It will add `sampleRate int` to fix the hardcoded 48kHz bug. Callers pass the actual sampleRate. The two test functions (`TestChromaPool`, `TestChromaPoolSilence`) will be updated to pass `48000`.

---

## Phase 2: MP3 Decode Pre-allocation

### Current State

`mp3decode.go:DecodeMP3` appends `float64` values one at a time in a loop:

```go
var samples []float64  // no capacity
for i := 0; i < n; i += 2 {
    left := int16(buf[i]) | int16(buf[i+1])<<8
    samples = append(samples, float64(left)/32768.0)
}
```

For ~1.3M samples, this causes ~20 reallocations (Go's growth factor).

### Change

Pre-allocate with a generous capacity estimate before the loop:

```go
estimatedSamples := len(data) * 5  // generous upper bound
samples := make([]float64, 0, estimatedSamples)
```

Use `len(data) * 5` as a safe upper bound (MP3 decompresses to PCM at ~4-8x in the worst case at very low bitrates, but 5x works for the 128kbps+ typical case).

---

## Phase 3: Truncate PCM Before Float32ToBytes

### Current State

All decoded PCM samples are converted to bytes, then `GetEmbedding` truncates to `maxPCMBytes` (32MB) internally. For a 30s track at 44.1kHz: ~1.3M float64s → ~5.3MB of byte conversion.

### Change

In `analyzeTrack`, after the quality check passes, truncate PCM to 10 seconds before calling `Float32ToBytes`:

```go
const clapDurationSeconds = 10
maxCLAPSamples := clapDurationSeconds * sampleRate
clapSamples := pcmSamples
if len(clapSamples) > maxCLAPSamples {
    clapSamples = clapSamples[:maxCLAPSamples]
}
pcmBytes := clap.Float32ToBytes(clapSamples)
```

CLAP (LAION-CLAP) processes a bounded audio window internally (~10s), so longer input is wasted work.

---

## Phase 4: Limit Feature Extraction to 15 Seconds

### Current State

MFCC and chroma process the entire decoded audio (up to ~30s/1.3M samples). Each frame requires FFT (MFCC uses `mjibson/go-dsp/fft` internally per frame; chroma uses `go-dsp/fft`). More samples = more frames = more FFTs.

### Change

After the quality check passes, truncate audio to 15 seconds for feature extraction:

```go
const maxFeatureDuration = 15 // seconds
maxFeatureSamples := maxFeatureDuration * sampleRate
featureSamples := pcmSamples
if len(featureSamples) > maxFeatureSamples {
    featureSamples = featureSamples[:maxFeatureSamples]
}
mfccVec := audio.ComputeMFCCPool(featureSamples, sampleRate)
chromaVec := audio.ComputeChromaPool(featureSamples, sampleRate)
```

15 seconds captures the musical essence while reducing FFT frames by ~50% compared to 30s.

**Tradeoff**: Very long tracks with significant late-structure changes may show slightly different features. This is standard practice in music information retrieval.

---

## Phase 5: Benchmarks

New file `internal/audio/audio_bench_test.go` with:

- `BenchmarkDecodeMP3` — synthetic MP3 data
- `BenchmarkComputeQualityAndChroma` — 30s synthetic PCM at 44.1kHz
- `BenchmarkComputeMFCCPool` — 15s synthetic PCM at 44.1kHz
- `BenchmarkFloat32ToBytes` — 10s float64 → byte conversion

---

## Implementation Order

1. **`internal/audio/quality.go`** — Add `QualityChroma` struct + `ComputeQualityAndChroma()`. Rewrite `CalculateSNR` and `CalculateSpectralCentroid` as wrappers.
2. **`internal/audio/chroma.go`** — Add `sampleRate int` parameter. Delegate to unified function.
3. **`internal/audio/mp3decode.go`** — Pre-allocate samples slice.
4. **`cmd/tui/main.go`** — Use `ComputeQualityAndChroma`, truncate PCM for features, truncate PCM for CLAP.
5. **`internal/audio/audio_bench_test.go`** — Benchmarks.
6. **`internal/audio/audio_test.go`** — Update `ComputeChromaPool` calls to pass sampleRate.
7. **Run tests + benchmarks, verify correctness.**
