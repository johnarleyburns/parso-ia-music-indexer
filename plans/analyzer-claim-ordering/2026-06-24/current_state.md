# Current State — Analyzer Claim Ordering

## Completed phases

- `ORDER BY t.id DESC` in `ClaimNextTrackBatch` (`internal/db/queue.go`).
- Updated order-dependent assertion in `TestInsertAndClaimTracks`
  (`internal/db/db_test.go`).
- Strengthened `TestClaimNextTrackBatchOrdering` to prove id ordering dominates
  `created_at`.
- Verification: `make build` OK; `go vet ./...` clean; `go test -race -count=1
  ./...` all packages pass.

## Pending phases

- Commit and push to `master`.

## Known blockers

None.

## Architectural changes made

None. Single-line query ordering change; no schema or interface changes.

## Remaining work

Commit and push.
