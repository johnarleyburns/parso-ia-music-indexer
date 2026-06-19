# Testing Plan

## Unit tests (in `internal/db/db_test.go`)

### TestTopKNearestMatchesFullSort
- Generate N=100 synthetic candidates with random distances.
- For k in {1, 5, 10, 50, N, N+10}:
  - Run `topKNearest(candidates, k)`.
  - Run reference full-sort on same input.
  - Assert same IDs in same order (up to min(k, N) elements).

### TestTopKBySimilarityMatchesFullSort
- Same approach for `TextSearchResult` path.
- Generate N=100 synthetic results with random similarities.
- For k in {1, 5, 20, N, N+10}:
  - Assert same IDs and order as reference `sort.Slice` + truncation.

### TestTopKNearestEmpty
- Empty input returns nil/empty.

### TestTopKNearestSingleElement
- Single candidate, k=1 and k=10 both return that single element.

### Existing tests
- `TestQuerySimilar` and `TestSearchByText` must still pass unchanged (they
  exercise the full path through the database).

## Benchmarks (in `internal/db/embeddings_bench_test.go`)

### BenchmarkTopKNearest
- N in {10_000, 50_000, 100_000}
- k = 20
- Each candidate has a pre-computed random distance (no f16 decode or cosine in
  the benchmark — isolate the selection algorithm).
- Report ns/op. Expect sub-5ms for N=100k.

### BenchmarkSearchByTextTopK (optional)
- Same structure for the similarity path.

## Acceptance criteria
- `make build` produces `bin/timbre`.
- `make test` — all tests pass.
- Benchmark shows no quadratic blowup (N=100k timing < 3x N=50k timing,
  roughly).
