# Architecture — Apple-Native CLAP

## Current Architecture

```
Go Orchestrator
    │
    ▼ (gRPC)
Python Sidecar
    │
    ├─ AutoProcessor (NumPy preprocessing)
    │
    └─ ClapModel (full 153M params)
         │
         ├─ AudioModel (28M) ← USED
         ├─ TextModel (125M) ← UNUSED
         └─ Projection (0.3M) ← USED
              │
              ▼
         MPS / CUDA / CPU
```

## Target Architecture

```
Go Orchestrator
    │
    ▼ (gRPC)
Python Sidecar
    │
    ├─ AutoProcessor (NumPy preprocessing)
    │
    └─ CoreML Model (audio-only, ~28M params)
         │
         ▼
    CoreML Runtime
         │
         ├─ ANE (preferred)
         ├─ GPU (Metal)
         └─ CPU (Accelerate/BNNS)
```

## Phase 1: Audio-Only Extraction

Extract just the audio encoder + projection from `ClapModel`:

```python
class CLAPAudioEncoder(torch.nn.Module):
    def __init__(self, clap_model):
        super().__init__()
        self.audio_model = clap_model.audio_model
        self.audio_projection = clap_model.audio_projection

    def forward(self, input_features):
        audio_outputs = self.audio_model(input_features=input_features)
        pooled = audio_outputs.pooler_output
        projected = self.audio_projection(pooled)
        return projected
```

Memory savings: ~600MB → ~150MB (75% reduction).

## Phase 2: CoreML Conversion

One-time conversion script (`python_sidecar/convert_to_coreml.py`):

1. Load full ClapModel from HuggingFace
2. Extract audio-only submodel
3. Trace with `torch.jit.trace` using dummy input
4. Convert with `coremltools.convert()` targeting `mlprogram` (ML Program format)
5. Set compute units to `ALL` (ANE + GPU + CPU)
6. Save as `clap_audio.mlpackage`

Key considerations:
- Input: `input_features` tensor (mel spectrogram), shape `[1, 1, 1001, 64]`
- Output: 512-dim float32 embedding
- Target iOS/macOS deployment: `macOS15` (or `macOS13` minimum)
- Precision: float16 for ANE compatibility (ANE prefers fp16)

## Phase 3: CoreML Inference

Replace PyTorch forward pass with CoreML prediction:

```python
import coremltools as ct

model = ct.models.MLModel("clap_audio.mlpackage")
prediction = model.predict({"input_features": mel_spectrogram_np})
embedding = prediction["embedding"]
```

Preprocessing (AutoProcessor → mel spectrogram) remains in NumPy.

## Phase 4: Fallback Logic

```python
def load_model():
    if sys.platform == "darwin":
        try:
            return CoreMLCLAPService("clap_audio.mlpackage")
        except Exception:
            pass
    return PyTorchCLAPService()  # existing MPS/CUDA/CPU path
```

The PyTorch path remains as fallback for:
- Non-macOS platforms
- CoreML conversion failures
- Missing .mlpackage file

## File Changes

| File | Change |
|------|--------|
| `python_sidecar/convert_to_coreml.py` | NEW — one-time conversion script |
| `python_sidecar/server.py` | Modified — CoreML inference with fallback |
| `python_sidecar/requirements.txt` | Add `coremltools>=8.0` |
| `Makefile` | Add `convert-coreml` target |

## Model Artifact

The converted `clap_audio.mlpackage` will be:
- Generated locally (not committed to git)
- Cached alongside the HuggingFace model cache
- ~60MB on disk (fp16 weights)
- Auto-generated on first run if missing
