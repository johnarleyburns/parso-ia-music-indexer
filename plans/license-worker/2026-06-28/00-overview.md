# License Worker — Feature Overview

## Problem
The system indexes music from the Internet Archive but has no awareness of track licensing. Users cannot distinguish between freely-usable tracks (public domain, CC0, commercially-usable CC) and restricted tracks, making it impossible to assess legal usability of collections.

## Current Behavior
- IA metadata `licenseurl` is returned by the API but discarded during JSON unmarshaling
- No `license` field exists in the `albums` table
- No license worker exists
- Collections/browse views show no license or "% free" information

## Design Proposal
1. **Extract license from IA metadata**: Add `licenseurl` to IA metadata structs and parse it from the API response
2. **Store license on albums**: Add `license` column to `albums` table
3. **License worker**: New worker type that runs after resolution, fetches metadata for albums lacking license info, categorizes license, and stores it
4. **UI treatment**: Dashboard section, status bar ETA, keybindings, events — matching existing worker patterns
5. **% free metric**: Add "% Free" column to collections table showing percentage of tracks under commercially-usable licenses
6. **License display in browse**: Show license in album detail view

## License Categories
| Category | Licenses | Commercially Usable |
|----------|----------|---------------------|
| `pd` | Public Domain, CC0, Public Domain Mark | Yes |
| `cc0` | CC0 1.0 | Yes |
| `cc-by` | CC BY | Yes |
| `cc-by-sa` | CC BY-SA | Yes |
| `cc-by-nc` | CC BY-NC | No |
| `cc-by-nc-sa` | CC BY-NC-SA | No |
| `cc-by-nd` | CC BY-ND | No |
| `cc-by-nc-nd` | CC BY-NC-ND | No |
| `other` | Unknown CC, other licenses | No |
| `unknown` | No license info available | No |

% Free = (pd + cc0 + cc-by + cc-by-sa) / total tracks × 100

## System Changes
1. `internal/ia/types.go` — Add LicenseURL to IAItemMetadata, AlbumMetadata
2. `internal/ia/metadata.go` — Parse licenseurl from API response
3. `internal/db/db.go` — Migration: add `license` column to `albums`
4. `internal/db/queue.go` — ClaimUnlicensedAlbum, UpdateAlbumLicense
5. `internal/db/collections.go` — Add FreePercentage to CollectionTrackStat, update query
6. `cmd/tui/main.go` — licenseWorkerLoop, coordinator integration, resolver: store license during resolution
7. `internal/tui/controls.go` — CmdAddLicense, CmdRemoveLicense
8. `internal/tui/events.go` — EventLicenseComplete
9. `internal/tui/dashboard.go` — LicenseCount, LicenseStates, isLicense, pool section
10. `internal/tui/statusbar.go` — License ETA in status bar
11. `internal/tui/metrics.go` — licenseCompletions, LicenseRate
12. `internal/tui/model.go` — Keybinding k/K for license workers
13. `internal/tui/collections.go` — Add "% Free" column
14. `internal/tui/browse.go` — Show license in album detail
