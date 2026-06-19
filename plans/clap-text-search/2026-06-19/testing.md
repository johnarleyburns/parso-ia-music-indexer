# Testing Strategy

## Phase 1: f16 Codec

### Unit Tests (`internal/db/db_test.go`)

1. **TestEncodeDecodeF16Roundtrip** -- f32 -> f16 -> f32 for representative
   CLAP values (normalized, in [-1, 1]). Assert error < 1e-3 per dimension.
2. **TestEncodeF16BlobSize** -- 512-dim input produces 1024-byte blob.
3. **TestEncodeDecodeF16Zero** -- zero vector round-trips to zero.
4. **TestEncodeDecodeF16Negative** -- negative values preserved.
5. **TestEncodeDecodeF16EdgeCases** -- values near f16 limits (65504, -65504,
   smallest subnormal).
6. **TestL2Normalize** -- unit norm output, zero input returns zero.
7. **TestL2NormalizeIdempotent** -- normalizing a unit vector is identity.

## Phase 2: Component Storage + Query-Time Fusion

### Unit Tests

1. **TestSaveComponentEmbedding** -- saves and retrieves components; blob sizes
   match expected f16 sizes.
2. **TestQuerySimilarV2Parity** -- fixed fixture: compute FuseFeatures on raw
   components, save via v1 path; also save via v2 path. QuerySimilar from both
   produces the same top-3 ranking. Distance values within f16 tolerance (~1e-3).
3. **TestBackfillV2FromV1** -- insert v1 rows with known 564-dim f32 vectors,
   run backfill, verify recovered components match originals within tolerance.
4. **TestBackfillSkipsNon564** -- insert a v1 row with 40-dim vector, backfill
   skips it without error.
5. **TestMigrateCreatesV2Table** -- Open() on a fresh DB creates both tables.
6. **TestDualWriteCompat** -- SaveComponentEmbedding also writes to v1 table
   for backward compat.

## Phase 3: Text RPC

### Unit Tests (`internal/clap/client_test.go`)

1. **TestMockClientTextEmbeddingDimensions** -- mock returns 512 dims.
2. **TestMockClientTextEmbeddingNonZero** -- mock vector has non-zero norm.
3. **TestMockClientTextEmbeddingDeterministic** -- two calls return the same
   vector.

### Integration Tests (require running sidecar)

1. **TestSidecarTextEmbedding** -- embed "melancholy piano", assert len==512,
   norm in [0.99, 1.01].
2. **TestSidecarTextAndAudioSameSpace** -- embed a text and an audio clip,
   verify cosine similarity is in (0, 1) -- they should be comparable, not
   orthogonal.

Integration tests can be gated behind a build tag (e.g. `//go:build sidecar`)
or skipped with `testing.Short()`.

## Phase 4: SearchByText

### Unit Tests (`internal/db/db_test.go`)

1. **TestDotProduct** -- known vectors, verify result.
2. **TestDotProductNormalized** -- two unit vectors, dot product == cosine sim.
3. **TestSearchByText** -- insert 3 tracks with known CLAP vectors into v2.
   Query with a vector that is identical to track 1's CLAP. Verify track 1 is
   ranked #1 with distance ~0.
4. **TestSearchByTextEmpty** -- empty DB returns empty results, no error.

### Manual / Integration Test

- Run `./bin/timbre --search-text "melancholy solo piano"` against the existing
  indexed corpus. Verify non-empty results with sensible titles.

## Phase 5: LibriVox Filter

### Unit Tests

1. **TestFilterDenylisted** -- 5 albums, 2 are in denylist. Verify output has
   3 albums and the correct ones.
2. **TestFilterDenylistedEmpty** -- empty denylist passes all through.
3. **TestFilterDenylistedAllBlocked** -- all items denylisted, output is empty.
4. **TestLoadDenylist** -- parse `data/librivox_denylist.json`, verify it
   returns a non-empty map.
5. **TestLoadDenylistMissing** -- missing file returns empty map, no error.

## Cross-Phase Regression

After all phases, run the full suite to ensure no regressions:
- `make build` produces `bin/timbre`.
- `make test` runs `go test ./...` -- all existing tests plus new ones pass.
- `make test-sidecar` runs Python inference test (if sidecar env is set up).

## Test Coverage Targets

- `internal/db/` -- f16 codec, component storage, backfill, search, filter.
- `internal/clap/` -- mock client text embedding, gRPC client text embedding.
- `internal/hybrid/` -- existing fusion tests remain unchanged.
- `cmd/tui/` -- integration-level behavior verified via manual testing.
