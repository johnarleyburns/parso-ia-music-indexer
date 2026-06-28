# Testing Strategy

## Unit tests added
- `internal/ia/license_test.go` — `TestCommerciallyUsableLicensePolicy`: verifies
  `ClassifyLicense` → category and `IsCommerciallyUsable` for representative license URLs
  (CC0, PD-Mark, BY, BY-SA, BY-NC, BY-ND, BY-NC-ND, BY-NC-SA, empty/unknown, other). This
  is the exact policy the resolver free-only gate relies on.
- `internal/db/seed_test.go` — `TestSeedCollectionsContent`: asserts the embedded seed JSON
  parses to exactly the two expected collections, each scoped to `mediatype:audio` + its
  `collection:` id, and that no query contains `licenseurl` (filtering is in-app).

## Existing tests still relevant
- `internal/db/db_test.go::TestSeedCollectionsIfEmpty` — count/idempotency; passes with 2
  seed entries (no hard-coded count).

## Manual / integration verification
1. `make build` → `bin/timbre`.
2. `go vet ./...` and `go test -race -count=1 ./...` pass.
3. Fresh-DB smoke run: exactly the two collections seed; discovery pulls albums; the
   resolver marks NC/ND/unknown albums `unavailable` (logged as
   "excluded: non-free license (<cat>)"); resolved albums' "% Free" trends to ~100%.

## Live query validation (Phase 0)
`advancedsearch` numFound: musopen-free = 34, netlabels-free = 76,477. Confirmed that
wildcard `licenseurl` filtering is rejected by IA's ES backend, motivating in-app
filtering.
