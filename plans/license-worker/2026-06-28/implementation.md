# License Worker — Implementation Plan

## Phase 1 — IA Metadata Extraction (data source)
**Files:** `internal/ia/types.go`, `internal/ia/metadata.go`
- Add `LicenseURL string` to `IAItemMetadata` with json tag `licenseurl`
- Add `LicenseURL string` to `AlbumMetadata`
- Parse `LicenseURL` from full.Metadata in `LookupAlbumMetadata`
- Add `ClassifyLicense(url string) string` function that categorizes license URLs
- Store license during album resolution in `resolveAlbum` (phase 5)

## Phase 2 — Database Schema
**File:** `internal/db/db.go`
- Add `license TEXT` column migration in `migrateSchemaChanges()`
- Use `columnExists` and `ALTER TABLE ADD COLUMN` pattern

## Phase 3 — Queue Functions
**File:** `internal/db/queue.go`
- Add `UntaggedLicenseAlbum` struct (or reuse)
- Add `ClaimUnlicensedAlbum(db, workerID)` — finds resolved albums with completed tracks, no license
- Add `UpdateAlbumLicense(db, identifier, license)` — sets license column
- Add `GetTrackLicenseStats(db)` — returns counts by license category for % free calculation

## Phase 4 — Collections Stats Enhancement
**File:** `internal/db/collections.go`
- Add `FreePercentage float64` to `CollectionTrackStat`
- Update `GetCollectionTrackStats` query to compute free count and percentage

## Phase 5 — License Worker Loop
**File:** `cmd/tui/main.go`
- Create `licenseWorkerLoop()` modeled after `tagEnhancerLoop()`
- Claims albums with `ClaimUnlicensedAlbum()`
- Fetches IA metadata via `ia.LookupAlbumMetadata()`
- Classifies license via `ia.ClassifyLicense()`
- Updates album via `db.UpdateAlbumLicense()`
- Emits `EventLicenseComplete` for each album

## Phase 6 — Metrics
**File:** `internal/tui/metrics.go`
- Add `licenseCompletions []time.Time` to Metrics
- Add `RecordLicenseCompletion()`
- Add `LicenseRate()` following existing patterns

## Phase 7 — Controls & Events
**File:** `internal/tui/controls.go`
- Add `CmdAddLicense`, `CmdRemoveLicense`

**File:** `internal/tui/events.go`
- Add `EventLicenseComplete`

## Phase 8 — Dashboard UI
**File:** `internal/tui/dashboard.go`
- Add `LicenseCount int`, `LicenseStates map[string]*workerState`
- Add `isLicense(id string)` helper (prefix `license-`)
- Handle `EventWorkerStarted`/`EventWorkerStopped` for license workers
- Handle `EventLicenseComplete` for license worker state updates
- Add license pool section in `buildRightPanel` with `[k] add [K] remove`
- Add license section to `buildPoolSection`

## Phase 9 — Status Bar
**File:** `internal/tui/statusbar.go`
- Add license ETA in status bar (line 1)
- Compute using `metrics.LicenseRate()` and unlicensed album count

## Phase 10 — Keybindings
**File:** `internal/tui/model.go`
- Add `k` → CmdAddLicense, `K` → CmdRemoveLicense in Dashboard tab
- Handle `CmdAddLicense`/`CmdRemoveLicense` in `MainModel.Update()`

## Phase 11 — Coordinator Integration
**File:** `cmd/tui/main.go`
- Add `licenseCount`, `nextLicenseID`, `licenseStopChs` in `runCoordinator`
- Handle `CmdAddLicense`/`CmdRemoveLicense`
- Handle `CmdRestartWorker` for license workers

## Phase 12 — Collections Table
**File:** `internal/tui/collections.go`
- Add "% Free" column to collections columns
- Add free percentage to row population from stats

## Phase 13 — Browse View
**File:** `internal/tui/browse.go`
- Show license in album detail mode

## Phase 14 — Resolver Enhancement
**File:** `cmd/tui/main.go`
- Store license when resolving albums (extract during `resolveAlbum`)

## Phase 15 — Stats Integration
**File:** `internal/db/queue.go`
- Add `UnlicensedCount` to `CombinedStats` for status bar ETA
