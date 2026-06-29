# Testing Strategy

## Unit
- Seed: `pills.json` parses; `SeedPills` upsert is idempotent; counts match.
- `GetPillByID` returns expected row / ErrNoRows behavior.
- `CountPillCoverage`: matches albums via subjects and via track tags; respects
  listenable filters (excludes non-completed / no-embedding / excluded tracks).
- `ListActivePills`: hides pills below `min_library_count`; hides `enabled=0`;
  sorts by `sort_order`; reports correct LibraryCount.

## Integration
- Phase 4: `ClaimUnresolvedAlbum`/`...Batch` returns `musopen-free` albums before
  netlabels albums when both are pending.

## Regression
- Existing `SearchByText`/pill-score tests unaffected.
- Migration adds `pills` without disturbing existing tables (open existing DB).

## Verify command
`make build && go vet ./... && go test -race -count=1 ./...`
