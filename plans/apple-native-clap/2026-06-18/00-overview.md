# Apple-Native CLAP Optimization — Results

## Problem

The CLAP sidecar loaded the full 153M-param model but only used the 28M-param audio path,
wasting ~500MB memory. The model ran on MPS (Metal Performance Shaders) but we investigated
whether Apple's more optimized inference paths (CoreML, ANE) could improve performance.

## Research Findings

### Environment
- Apple M2 MacBook Air, 16GB RAM
- Python 3.14.5, PyTorch 2.12.1, Transformers 5.12.1
- MPS backend available and working

### Approaches Tested

| Approach | Result | Details |
|----------|--------|---------|
| CoreML via coremltools | **BLOCKED** | Python 3.14 not supported by coremltools 9.0 (missing libcoremlpython native extensions) |
| ONNX Runtime + CoreML EP | **Slower (4.9x)** | Only 499/716 ops on CoreML, 92 partitions cause heavy data shuffling. 789ms vs 162ms MPS |
| torch.compile + MPS | **Slower (0.8x)** | Inductor overhead on M2, 132ms vs 105ms baseline |
| MPS fp16 | **1.6x speedup** | 65.5ms vs 105ms, cosine similarity > 0.999 |
| torch.compile + fp16 | **Slower (0.85x)** | Compile overhead negates fp16 gains |

### Benchmark Results (20 iterations, 10s audio)

| Configuration | Latency | Speedup vs Baseline | Cosine vs fp32 |
|---------------|---------|---------------------|----------------|
| MPS fp32 (baseline) | 105.0 ms | 1.0x | — |
| **MPS fp16** | **65.5 ms** | **1.6x** | 0.999999 |
| torch.compile + fp32 | 132.1 ms | 0.8x | 0.999999 |
| torch.compile + fp16 | 124.1 ms | 0.85x | 0.999946 |
| ORT CoreML EP | 789.2 ms | 0.13x | 0.99999893 |
| PyTorch CPU | 597.0 ms | 0.18x | — |

## Optimizations Applied

### 1. Audio-Only Extraction (Phase 1)
- Loads full ClapModel, extracts audio_model + audio_projection, deletes the rest
- **Memory savings**: ~500MB (153M → 28M params)
- **Output**: Bit-for-bit identical (cosine = 1.0, max diff = 0.0)

### 2. fp16 on MPS (Phase 2c)
- Converts audio_model and audio_projection to float16 when device is MPS
- Input features cast to fp16 before inference
- **Speedup**: 1.6x (105ms → 65.5ms per inference)
- **Output**: Essentially identical (cosine = 0.999999 vs original full model)
- **Additional memory savings**: ~50% GPU memory for weights

## Decisions Recorded

1. **CoreML via coremltools**: Chosen but blocked by Python 3.14. Revisit when coremltools adds 3.14 support or if project moves to Python 3.12/3.13.
2. **ONNX Runtime + CoreML EP**: Tested and rejected — insufficient op coverage (70%) for CLAP/HTSAT causes excessive partitioning overhead.
3. **torch.compile**: Tested and rejected — inductor overhead exceeds any gains on M2.
4. **MPS fp16**: Adopted — best combination of speed, quality, and simplicity.

## Files Changed

| File | Change |
|------|--------|
| `python_sidecar/server.py` | Audio-only extraction + fp16 on MPS |

## Future Opportunities

1. **coremltools Python 3.14 support**: When available, direct CoreML conversion would likely achieve ANE acceleration (no ONNX intermediary, no partitioning).
2. **PyTorch MPS improvements**: Apple continues optimizing the MPS backend; future PyTorch versions may close the gap with CoreML.
3. **Batching**: If multiple tracks need embedding simultaneously, batch processing could improve GPU utilization.
