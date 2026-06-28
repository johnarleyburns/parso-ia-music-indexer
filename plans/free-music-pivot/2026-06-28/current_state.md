# Current State

## Status: implemented, pending CI

## Completed
- Replaced `internal/db/seed_collections.json` with `musopen-free` + `netlabels-free`
  (collection+audio scope; no license clause).
- Added `--free-only` config flag (default true, env `FREE_ONLY`) + `envOrDefaultBool`.
- Added in-app free-only gate in `resolveAlbum` (after `IsMusicContent`, before track
  insertion): non-commercially-usable albums get their license stored and are marked
  `unavailable`.
- Threaded `freeOnly` through `albumResolverLoop` → `resolveAlbum` and both coordinator
  call sites.
- Added `internal/ia/license_test.go` and `internal/db/seed_test.go`.
- Plan docs written.

## Architectural changes made
- Album resolution now enforces a license policy when `--free-only` is on. This is a
  global behavior change (affects all collections, including manually imported playlists),
  intentional for a Free Music indexer and reversible via the flag.

## Pending / follow-ups
- CI (`go vet` + `go test -race`) on push to master.
- Optional future: early license check could be folded into discovery, but discovery only
  fetches `identifier,downloads` (no licenseurl), so the resolver is the earliest point
  with license data without an extra metadata round-trip.
- Legacy `licenseurl` wildcard approach is not viable on IA's ES backend (documented).

## Not changed (by decision)
- Patron-list / playlist import UI and code paths remain intact.
- Stale non-empty DBs are not migrated.
