# Current State

## Date: 2026-06-16

## Status: PHASE 2 COMPLETE — Database layer with SQLite, vector storage, and live TUI stats

## Completed Phases

- [x] Research — Chat session analysis, IA API research, ML model research, database comparison, TUI framework research
- [x] Design — Architecture design (Rev 2: TUI-based), data model design, component specifications, event system design
- [x] Planning — 7-phase implementation plan with per-phase exit criteria, e2e test suite plan
- [x] Phase 1 — TUI Scaffold & Navigation
- [x] Phase 2 — Database Layer + Embedding Operations

## Pending Phases

- [ ] Phase 3 — Dashboard Tab: Live Stats & Controls
- [ ] Phase 4 — IA Scraping API Client + Real Coordinator
- [ ] Phase 5 — Audio Analysis Pipeline + Real Worker Pool
- [ ] Phase 6 — Browse / Search Tab
- [ ] Phase 7 — Player Tab

## Phase 2 Deliverables

### Files Created
- `internal/db/db.go` — SQLite open (WAL mode, busy timeout 5s), migration (idempotent)
- `internal/db/queue.go` — `ClaimNextBatch` (optimistic locking), `MarkCompleted`, `MarkFailed`, `ResetStuckJobs`, `GetStats`, `BulkInsertPending`
- `internal/db/embeddings.go` — `SaveEmbedding`, `QuerySimilar` (brute-force cosine, pure Go), `GetEmbeddingCount`, BLOB encode/decode helpers
- `internal/db/db_test.go` — 10 unit tests covering all DB operations

### Files Modified
- `internal/tui/dashboard.go` — Real DB stats table with auto-refresh (2s tick), styled with colors per status
- `internal/tui/model.go` — Added `*sql.DB` field, wired Dashboard init/update/view, routes `statsRefreshMsg`
- `cmd/tui/main.go` — Opens DB on startup (TUI + headless), headless prints full JSON stats

### Dependencies Added
- `github.com/mattn/go-sqlite3` v1.14.45

### Design Decision: Pure Go Cosine Similarity (No sqlite-vec C Extension)
Vectors stored as BLOBs (40 × 4 bytes = 160 bytes). Cosine similarity computed in pure Go. The `viant/sqlite-vec` (pure Go virtual table) was considered but deemed too complex for Phase 2 (full MySQL replication architecture). For 4M tracks, brute-force scan is acceptable for top-K queries on modern hardware. sqlite-vec index can be added later for performance optimization.

### Tests
```
ok  github.com/johnarleyburns/parso-ia-music-indexer/internal/db  1.681s
```

10 tests passing:
- `TestOpenMigrate` — DB opens, schema created, migration idempotent
- `TestGetStatsEmpty` — Empty DB returns correct zeros
- `TestBulkInsertPending` — Bulk insert with INSERT OR IGNORE dedup
- `TestClaimAndComplete` — Optimistic locking, status transitions
- `TestMarkFailed` — Error message saved, retry count incremented
- `TestResetStuckJobs` — Stuck processing jobs reset to pending
- `TestEmbeddingRoundtrip` — F32 encode/decode roundtrip
- `TestQuerySimilar` — Identical vectors → dist 0; opposite vectors → dist ~2
- `TestCosDistanceSelf` — Self-distance = 0
- `TestCosDistanceOrthogonal` — Orthogonal vectors → dist ~1

## Database Schema (Implemented)

- `catalog_queue` — Queue state machine (pending/processing/completed/failed) with retry_count, error_message
- `track_embeddings` — Vector embeddings as BLOBs (40×float32) with quality_score
- `cursor_state` — Coordinator resume state (singleton row)

## Known Blockers (Updated)

1. ~~sqlite-vec Go bindings~~ — **Resolved**: Using pure Go cosine similarity with BLOB storage. sqlite-vec deferred to later.
2. **go-mfcc library** — Need to verify output compatibility with librosa's MFCC.
3. **IA Scraping API stability** — Need to test endpoint with music collection queries.
4. **beep/v2 MP3 streaming** — Need to verify capability with non-seekable io.Reader.
5. **SQLite WAL + concurrent goroutines** — Currently `SetMaxOpenConns(1)`; will need mutex for worker writes in Phase 5.

## Next Action

**Ready for Phase 3 — Dashboard Tab: Live Stats & Controls.**
