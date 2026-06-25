# Current Implementation State

## Completed Phases

### Phase 1 — Representative audio sampling
- [x] Config: `--sample-strategy {head|midpoint}` (default midpoint) + `--sample-skip-seconds` (default 20)
- [x] `StreamAudioMidpoint()`: HEAD to get file size, compute mid-track Range, fallback to head
- [x] `StreamAudioFromURLWithRange()`: issues Range header, handles 206 Partial Content
- [x] `computeMidpointStartByte()`: targets ~35% into file with skip-seconds floor
- [x] DB migration: `sample_strategy TEXT` column added to `track_embeddings`
- [x] `SaveEmbeddingWithStrategy()`: stores strategy alongside embedding
- [x] `analyzeTrack()`: wired to use midpoint strategy from config
- [x] `GetEmbeddingStrategyCounts()`: reports strategy distribution
- [x] `ResetTracksWithStrategy()`: resets tracks to pending for re-embed with new strategy
- [x] `GetLexicalCoverage()`: coverage stats for Phase 4

### Phase 2 — `make export-ios`
- [x] `make export-ios` target added to Makefile
- [x] Path: `python_sidecar/export_for_ios.py --output-dir ../../parso-acalum-ios-app/Acalum/Resources/`
- [x] `export_for_ios.py` updated to emit `byte_encoder.json`

### Phase 3 — eval_retrieval.py + `make eval-retrieval`
- [x] `python_sidecar/eval_retrieval.py` created
- [x] `make eval-retrieval` target added to Makefile
- [x] Embeds 12 canonical prompts via gRPC, cosine-ranks against all DB vectors
- [x] Supports `--output-json` for persistence

### Phase 4 — Coverage diagnostic (optional)
- [x] `GetLexicalCoverage()` returns tag/subject/genre coverage
- [x] Headless stats output includes `lexical_coverage` and `embedding_strategies`

## Known Blockers
- None

## Architectural Changes Made
- `track_embeddings` has new `sample_strategy` column (default 'head' for existing rows)
- `SaveEmbedding` signature unchanged (delegates to `SaveEmbeddingWithStrategy` with "head")
- `StreamAudioFromURL` unchanged for backward compatibility
- Headless JSON now emits `embedding_strategies` and `lexical_coverage`

## Verification
- `make ci` green: build + vet + test -race all pass (12 packages)
- No backward compatibility issues — existing tests pass
