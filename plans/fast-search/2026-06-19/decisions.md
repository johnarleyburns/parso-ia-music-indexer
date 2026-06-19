# Decisions

## Decision 1: Keep vectors L2-normalized on disk

**Context**: The handoff referred to stored vectors as "raw." The code currently
stores them L2-normalized (`SaveEmbedding` calls `l2Normalize` on each block
before `encodeF16`).

**Recommendation**: Keep normalized.

**Rationale**:
- `SearchByText` uses `dotProduct(qv, clapVec)` directly, which equals cosine
  similarity only when both vectors are unit-length. Storing un-normalized
  vectors would break text search ranking.
- `QuerySimilar` uses `cosineDistance` which re-derives norms (safe but
  redundant). The Phase 2 fusion shortcut (weighted dot products) depends on
  pre-normalized storage.
- No consumer of `GetEmbedding` needs original magnitude. The quality signal is
  already stored separately as `quality_score`.

**Decision**: Store L2-normalized. No change needed.

## Decision 2: Phase 2 — query-time fusion shortcut

**Context**: `QuerySimilar` currently calls `FuseFeatures` on every candidate
to build a 564-dim hybrid, then computes `cosineDistance`. Because each block
is L2-normalized on disk, the fused cosine is equivalent to a weighted sum of
per-block dot products divided by a constant norm.

**Proposal**: Replace per-candidate `FuseFeatures` + `cosineDistance` with:
```
score = w_clap^2 * dot(clap_q, clap_i)
      + w_mfcc^2 * dot(mfcc_q, mfcc_i)
      + w_chroma^2 * dot(chroma_q, chroma_i)
```
The constant denominator `sqrt(w_clap^2 + w_mfcc^2 + w_chroma^2)` cancels for
ranking (identical for all candidates). This avoids allocating a 564-float
slice per candidate.

**Gate**: A parity test proving identical ranking vs the current path on a
multi-track fixture.

**Status**: Proposed. Do not implement until Phase 1 lands and is approved.
