# Decisions — CONFIRMED

## Decision 1: Checkpoint Confirmation

**CONFIRMED:** Stay on `laion/clap-htsat-fused` for both towers.

---

## Decision 2: f16 Normalization Policy

**CONFIRMED:** Option A — L2-normalize all blocks (CLAP, MFCC, chroma) before
f16 encoding. Values in [-1, 1], f16-safe, magnitude-invariant for cosine.

---

## Decision 3: Migration Path

**CONFIRMED:** Clean re-index. Wipe the existing DB entirely. No backfill code.
This simplifies Phase 2: no v1-to-v2 migration, no dual-write, no
backward-compat v1 table. The old `track_embeddings` table is dropped (or
simply not created); only `track_embeddings_v2` exists.

---

## Decision 4: LibriVox / Spoken Audio Filter

**CONFIRMED:** Curated identifier denylist (Option A) as the immediate filter.

**Expanded scope:** Research broader spoken-audio detection beyond LibriVox.
Three strategies identified for follow-up:

1. **Substring pattern matching (Phase 5):** In addition to exact denylist,
   also match identifiers containing `_librivox` or belonging to known
   spoken-word IA collections (`librivoxaudio`, `audio_bookspoetry`, etc.).
2. **IA metadata filtering:** The IA scrape/metadata API exposes `mediatype`,
   `subject`, and `collection` fields. Items tagged as spoken word, audiobook,
   or lecture can be filtered at discovery or resolver time.
3. **CLAP-based speech detection (post Phase 4):** Once text search works,
   embed reference labels like "spoken word narration", "audiobook reading",
   "lecture recording" and compare against each new track's CLAP vector at
   analysis time. High cosine similarity to speech labels = auto-reject.
   This is the most elegant long-term solution and leverages the text tower.

Phase 5 will implement the curated list + substring fallback. The CLAP-based
approach becomes a follow-up phase after text search is validated.

---

## Decision 5: Segment-Level Vectors

**CONFIRMED:** No segment-level vectors. Removed from scope entirely.

**New requirement:** Auto-reject any track over 32 minutes in length. IA
metadata includes a `length` field per file (duration in seconds). Filter
during album resolution so long tracks never enter the analysis pipeline.
Implemented in Phase 2 alongside the schema changes.

---

## Decision 6: Sidecar Text Tower fp16

**CONFIRMED:** Apply the same MPS fp16 policy (`model.half()`) to the text
tower, matching the existing audio tower behavior.

---

# Summary

| # | Topic | Decision | Status |
|---|-------|----------|--------|
| 1 | Checkpoint: `clap-htsat-fused` | Confirmed | DONE |
| 2 | f16 normalization: L2-normalize all | Option A | DONE |
| 3 | Migration: clean re-index (wipe DB) | Clean wipe | DONE |
| 4 | Spoken audio filter: curated + research | Curated + expanded | DONE |
| 5 | Segments: removed, 32-min max track | No segments, reject >32m | DONE |
| 6 | Sidecar text fp16 on MPS | Confirmed | DONE |
