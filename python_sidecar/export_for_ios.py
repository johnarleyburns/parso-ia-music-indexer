"""
Export CLAP text encoder + tokenizer for iOS Core ML deployment.

Produces:
  - AcalumCLAPTextEncoder.mlpackage  (Core ML model)
  - vocab.json                        (GPT-2 BPE vocabulary)
  - merges.txt                        (BPE merge rules)

Requirements: Python 3.12 (coremltools doesn't support 3.14 yet)
  pip install coremltools torch transformers

Usage:
  python export_for_ios.py [--output-dir /path/to/ios/Resources]
"""

import argparse
import json
import os
import sys

import numpy as np
import torch
import torch.nn.functional as F
from transformers import AutoProcessor, ClapModel

MODEL_NAME = "laion/clap-htsat-fused"
EXPECTED_DIM = 512

parser = argparse.ArgumentParser(description="Export CLAP text encoder for iOS")
parser.add_argument("--output-dir", default="ios_export", help="Output directory")
args = parser.parse_args()

os.makedirs(args.output_dir, exist_ok=True)


# ── Step 1: Export vocabulary and merges ──────────────────────────────────

print("Loading tokenizer...")
processor = AutoProcessor.from_pretrained(MODEL_NAME)
tokenizer = processor.tokenizer

# Save full tokenizer.json (HuggingFace standard format)
tokenizer_path = os.path.join(args.output_dir, "tokenizer_full")
os.makedirs(tokenizer_path, exist_ok=True)
tokenizer.save_pretrained(tokenizer_path)
print(f"Wrote tokenizer to {tokenizer_path}")

# Also save vocab.json and merges.txt separately for simpler Swift parsing
vocab = tokenizer.get_vocab()
# Sort by ID for easy array-based lookup
vocab_list = sorted(vocab.items(), key=lambda x: x[1])
# vocab.json: {"token": id, ...}
with open(os.path.join(args.output_dir, "vocab.json"), "w", encoding="utf-8") as f:
    json.dump(dict(vocab_list), f, ensure_ascii=False, indent=2)
print(f"Vocab size: {len(vocab)}")

# Save merges
if hasattr(tokenizer, "bpe_ranks"):
    merges = tokenizer.bpe_ranks
    # Sort by rank to preserve merge order
    ranked = sorted(merges.items(), key=lambda x: x[1])
    with open(os.path.join(args.output_dir, "merges.txt"), "w", encoding="utf-8") as f:
        for (a, b), rank in ranked:
            f.write(f"{a} {b}\n")
    print(f"BPE merges: {len(merges)}")

# Save special tokens info
special_info = {
    "bos_token": tokenizer.bos_token,
    "bos_token_id": tokenizer.bos_token_id,
    "eos_token": tokenizer.eos_token,
    "eos_token_id": tokenizer.eos_token_id,
    "pad_token": tokenizer.pad_token,
    "pad_token_id": tokenizer.pad_token_id,
}
with open(os.path.join(args.output_dir, "tokenizer_config.json"), "w") as f:
    json.dump(special_info, f, indent=2)


# ── Step 2: Export Core ML model ──────────────────────────────────────────

print("\nLoading CLAP model for Core ML export...")
full_model = ClapModel.from_pretrained(MODEL_NAME)
text_model = full_model.text_model
text_projection = full_model.text_projection

text_model.eval()
text_projection.eval()


class CLAPTextEncoder(torch.nn.Module):
    """Minimal forward pass — bypasses HF dynamic wrappers for export compatibility."""

    def __init__(self, text_model, text_projection):
        super().__init__()
        self.text_model = text_model
        self.text_projection = text_projection
        self.embeddings = text_model.embeddings
        self.encoder = text_model.encoder
        self.pooler = text_model.pooler
        self.projection = text_projection

    def forward(self, input_ids, attention_mask):
        # attention_mask: [batch, seq_len] float (1 for real, 0 for pad)
        x = self.embeddings(input_ids=input_ids)

        # Build extended attention mask: [batch, 1, 1, seq_len]
        mask = attention_mask.unsqueeze(1).unsqueeze(2).to(dtype=x.dtype)
        # Use a finite large negative value (not finfo.min) to avoid
        # coremltools converting it to -inf, which produces NaN from 0 * -inf.
        mask = (1.0 - mask) * torch.tensor(-10000.0, dtype=x.dtype)

        enc_out = self.encoder(x, attention_mask=mask)
        pooled = self.pooler(enc_out.last_hidden_state)
        projected = self.projection(pooled)
        return F.normalize(projected, dim=-1)


encoder = CLAPTextEncoder(text_model, text_projection)

# Prepare sample inputs for export (padded to max_length=77)
print("Preparing inputs...")
inputs = processor(
    text=["quiet Spanish guitar at dusk"],
    return_tensors="pt",
    padding="max_length",
    truncation=True,
    max_length=77,
)
sample_input_ids = inputs["input_ids"].long()
sample_attention_mask = inputs["attention_mask"].float()

# Verify minimal forward matches full HF forward
with torch.no_grad():
    minimal_out = encoder(sample_input_ids, sample_attention_mask)
    full_out = encoder.text_model(input_ids=sample_input_ids, attention_mask=sample_attention_mask)
    full_proj = encoder.text_projection(full_out.pooler_output)
    full_norm = F.normalize(full_proj, dim=-1)
    cos_check = F.cosine_similarity(minimal_out, full_norm).item()
    print(f"Minimal vs full forward cosine: {cos_check:.6f}")
    assert cos_check > 0.999, f"Minimal forward deviates (cos={cos_check})"
print(f"Eager output shape: {minimal_out.shape}, norm: {minimal_out.norm().item():.4f}")

# Convert to Core ML via torch.jit.trace
try:
    import coremltools as ct

    print("Tracing model with torch.jit.trace...")
    traced = torch.jit.trace(encoder, (sample_input_ids, sample_attention_mask))

    with torch.no_grad():
        traced_out = traced(sample_input_ids, sample_attention_mask)
        cos_trace = F.cosine_similarity(minimal_out, traced_out).item()
        print(f"Traced vs eager cosine: {cos_trace:.6f}")
        assert cos_trace > 0.999, f"Trace deviates (cos={cos_trace})"

    print("Converting to Core ML (this may take a few minutes)...")
    mlmodel = ct.convert(
        traced,
        inputs=[
            ct.TensorType(name="input_ids", shape=(1, 77), dtype=np.int32),
            ct.TensorType(name="attention_mask", shape=(1, 77), dtype=np.float32),
        ],
        outputs=[ct.TensorType(name="text_embedding", dtype=np.float32)],
        convert_to="mlprogram",
        compute_units=ct.ComputeUnit.ALL,
        compute_precision=ct.precision.FLOAT16,
        minimum_deployment_target=ct.target.iOS17,
    )

    out_path = os.path.join(args.output_dir, "AcalumCLAPTextEncoder.mlpackage")
    mlmodel.save(out_path)
    print(f"Wrote {out_path}")

    # Validate Core ML output against PyTorch
    print("Validating Core ML model...")
    prediction = mlmodel.predict({
        "input_ids": sample_input_ids.numpy().astype(np.int32),
        "attention_mask": sample_attention_mask.numpy().astype(np.float32),
    })
    coreml_out = torch.tensor(prediction["text_embedding"]).view(1, EXPECTED_DIM)
    cos_coreml = F.cosine_similarity(minimal_out, coreml_out, dim=-1).item()
    print(f"Core ML vs PyTorch cosine similarity: {cos_coreml:.6f}")
    assert cos_coreml >= 0.995, f"Core ML output deviates too much (cosine={cos_coreml})"

except ImportError:
    print("\nWARNING: coremltools not available.")
    sys.exit(1)


# ── Step 3: Generate test vectors for iOS validation ──────────────────────

print("\nGenerating test vectors for iOS validation...")
test_prompts = [
    "quiet Spanish guitar at dusk",
    "melancholy piano for reading",
    "Gregorian chant in an old cathedral",
    "early jazz from the 1920s",
    "romantic classical guitar",
    "soft public domain music for sleep",
    "baroque strings and harpsichord",
    "nostalgic old recordings",
    "peaceful violin music",
    "dramatic organ music",
]

test_vectors = {}
with torch.no_grad():
    for prompt in test_prompts:
        inp = processor(
            text=[prompt],
            return_tensors="pt",
            padding="max_length",
            truncation=True,
            max_length=77,
        )
        out = encoder(inp["input_ids"], inp["attention_mask"])
        vec = out[0].tolist()
        test_vectors[prompt] = vec

        # Also tokenize and save for tokenizer validation
        tokens = tokenizer.encode(prompt, add_special_tokens=True)
        test_vectors[f"{prompt}__token_ids"] = tokens

with open(os.path.join(args.output_dir, "test_vectors.json"), "w") as f:
    json.dump(test_vectors, f)

print(f"Wrote {len(test_prompts)} test vectors to test_vectors.json")
print("\nExport complete!")
print(f"Output files in: {args.output_dir}/")
for fname in sorted(os.listdir(args.output_dir)):
    fpath = os.path.join(args.output_dir, fname)
    size_mb = os.path.getsize(fpath) / (1024 * 1024)
    print(f"  {fname:<40s} {size_mb:.1f} MB")
