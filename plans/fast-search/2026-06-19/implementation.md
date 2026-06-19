# Implementation Plan

## Phase 1: Bounded top-k heap for both ranking paths

### Files modified
- `internal/db/embeddings.go` (main change)
- `internal/db/db_test.go` (new tests + benchmark)

### Changes

#### 1a. Add a max-heap type for `candidate` (ascending distance = nearest)

Implement `container/heap` interface on a `candidateHeap` type. The heap is a
**max-heap on distance** of bounded size `k`:
- For each candidate, if heap size < k, push.
- Else if candidate.dist < heap[0].dist (the current worst), pop the worst and
  push the candidate.
- After scanning all candidates, pop all into a slice and reverse for ascending
  distance order.

This gives O(n log k) selection instead of O(n^2).

#### 1b. Replace `sortByDist` with `topKNearest`

Delete the O(n^2) `sortByDist` function. Replace its call site in
`QuerySimilar` with the heap-based `topKNearest(candidates, limit)` which
returns a `[]candidate` of at most `limit` elements in ascending distance order.

#### 1c. Add a max-heap type for `TextSearchResult` (descending similarity)

Similar bounded heap, but this is a **min-heap on similarity** (we evict the
lowest similarity when full). After scanning, pop and reverse for descending
similarity order.

Replace the `sort.Slice` + truncation in `SearchByText` with
`topKBySimilarity(results, limit)`.

#### 1d. Edge cases
- `limit <= 0` or `limit >= len(candidates)`: return all, sorted.
- `limit = 1`: still correct (heap of size 1).
- Empty candidate set: return nil.

### No new dependencies
Uses only `container/heap` from stdlib.

### Behavioral preservation
- `QuerySimilar` returns ascending distance (unchanged).
- `SearchByText` returns descending similarity (unchanged).
- Tie-breaking: `container/heap` is not stable, but the current `sortByDist` is
  also not stable (selection sort with strict `<`). No semantic regression.
