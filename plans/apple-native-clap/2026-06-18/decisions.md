# Decisions — Apple-Native CLAP

## Decision 1: Optimization Approach

**Options:**
- A) CoreML via `coremltools` (recommended) — full ANE access, best perf, moderate effort
- B) ONNX Runtime + CoreML EP — easier but partial ANE, less perf gain
- C) MLX — no existing port, significant effort, no ANE

## Decision 2: Conversion Strategy

**Options:**
- A) Offline conversion script + auto-convert on first run if .mlpackage missing (recommended)
- B) Offline-only — user must run conversion script manually before first use
- C) Always convert at startup — slow first boot

## Decision 3: Precision

**Options:**
- A) Float16 (recommended) — ANE strongly prefers fp16, 50% smaller model, negligible quality loss for embeddings
- B) Float32 — maximum precision, but ANE may fall back to GPU/CPU

## Decision 4: Fallback Behavior

**Options:**
- A) CoreML → PyTorch MPS → CPU (recommended) — graceful degradation
- B) CoreML only on macOS, error if conversion fails
