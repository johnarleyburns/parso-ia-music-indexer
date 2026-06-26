# Current Implementation State

## Completed Phases

- [x] Research current analyzer, resolver, DB schema, quality scoring, CLAP sidecar, and cleanup-worker precedents.
- [x] Inspect local DB duration and album-shape distributions.
- [x] Create listenability planning documents.
- [x] Phase 1: Schema columns on `tracks` and `albums` + `internal/listenability` package + DB accessors.
- [x] Phase 2: Album listenability scoring during album resolution (`resolveAlbum()`).
- [x] Phase 3: Analyzer scoring for new tracks (precheck before streaming, metadata scoring after CLAP).
- [x] Phase 4: Score-only cleaner worker (`listenabilityCleanerLoop`) with prompt-cached CLAP scoring.
- [x] Phase 5: Config flags, event types, headless stats coverage, dashboard cleaner pool, search filtering.
- [x] Phase 6: Search/query filtering with transitional NULL-safe rule.

## Pending Phases

- [ ] TUI detail visibility: per-track listenability in Browse track/album views, Collections avg listenability, Player track listenability, similar track listenability.
- [ ] Optional `mark-unavailable` mutation for completed tracks (currently score-only mode).
- [ ] Allowlist for legitimate short-form genres.
- [ ] Longform surface for 15-25 minute tracks.

## Known Blockers

- None. Product decision needed before enabling `mark-unavailable` for existing completed tracks.

## Architectural Changes Made

- `internal/listenability/` — Pure scoring package: evidence types, scoring functions, tier/decision/stream classification.
- `internal/db/db.go` — Schema migration: 10 listenability columns on `tracks`, 8 on `albums`, 3 new indexes.
- `internal/db/listenability.go` — DB accessors for evidence, cleanup claims, coverage stats, listenability updates.
- `internal/db/queue.go` — Extended `ClaimedTrack` with duration/bitrate/tags/album listenability context.
- `internal/db/embeddings.go` — Added listenability filters to `SearchByText()`, `QuerySimilar()`, `SearchCompletedTracks()`.
- `cmd/tui/main.go` — Album listenability in `resolveAlbum()`, metadata precheck and scoring in `analyzeTrack()`, `listenabilityCleanerLoop()`, headless stats coverage.
- `internal/config/config.go` — Added `ListenabilityMinTrackSecs` and `ListenabilityCleanerAction` config flags.
- `internal/tui/controls.go` — Added `CmdAddCleaner` / `CmdRemoveCleaner` control actions.
- `internal/tui/events.go` — Added `EventCleanerStarted` / `EventCleanerBatch` event types.
- `internal/tui/model.go` — Added 'l' / 'L' keybindings for cleaner pool control.
- `internal/tui/dashboard.go` — Added cleaner pool tracking and display in right panel.

## Remaining Work

- Add per-track/album listenability display fields in Browse, Collections, Player TUI views.
- Implement `mark-unavailable` mutation mode in cleaner.
- Add allowlist configuration.
- Add longform content surface.

## Verification

```sh
make build   # binary at bin/timbre
go vet ./...
go test -race -count=1 ./...
# All pass (build, vet, 9 package tests)
```
