# Testing Strategy (Revision 2 — TUI + Headless + E2E)

## Problem

Each implementation phase must be independently testable. The pipeline has external dependencies (Internet Archive APIs) that need mocking for unit tests. SQLite with sqlite-vec must be tested with real extension loading. The TUI must be testable. Headless mode must serve as an e2e test entry point.

## Testing Categories

### Unit Tests

Isolated logic tests with no external dependencies.

| Module | What to test | Phase |
|---|---|---|
| `internal/rate/` | Token bucket enforces rate; throttled reader limits bytes/sec | 4–5 |
| `internal/audio/snr.go` | SNR on known PCM signals: sine wave → high SNR (>40 dB); white noise → low SNR (<5 dB) | 5 |
| `internal/audio/mfcc.go` | MFCC output is []float32 length 40; known sine wave produces expected band distribution | 5 |
| `internal/audio/decode.go` | Decoding a valid MP3 byte slice produces correct sample count | 5 |
| `internal/ia/types.go` | JSON unmarshaling of IA Scraping API response and metadata response | 4 |
| `internal/config/config.go` | Config defaults, env var overrides, flag parsing | 1 |

### Integration Tests

Tests with real SQLite database (`:memory:` or temp file).

| Module | What to test | Phase |
|---|---|---|
| `internal/db/db.go` | SQLite opens in WAL mode; sqlite-vec loads; migrations run idempotently (run twice, no error) | 2 |
| `internal/db/queue.go` | `ClaimNextBatch` — optimistic locking, concurrent claims don't overlap; `ResetStuckJobs` recovers old locks; `MarkCompleted`/`MarkFailed` state transitions; `BulkInsertPending` INSERT OR IGNORE | 2 |
| `internal/db/embeddings.go` | `SaveEmbedding` stores vector; `QuerySimilar` returns correct identifier with distance ordering; empty DB query returns empty | 2, 6 |
| `internal/audio/stream.go` | HTTP Range request against mock server returns correct byte range | 5 |
| `internal/ia/scrape.go` | Mock IA Scraping API server returns valid JSON; `ScrapePage` parses correctly | 4 |
| `internal/ia/client.go` | Mock server returns 429 → retry with backoff succeeds; 503 → retry max then error | 4 |

### TUI Component Tests

Bubble Tea supports testing via `tea.NewProgram(model, tea.WithoutRenderer())` and sending `tea.KeyMsg`/`tea.WindowSizeMsg` programmatically.

| Component | What to test | Phase |
|---|---|---|
| Tab bar | `tab`/`shift+tab` switches active tab; tab bar renders with correct active/inactive styles | 1 |
| Help footer | `?` toggles help visibility; keybindings listed match definition | 1 |
| Dashboard stats | Stats table updates when `StatsUpdateMsg` sent; auto-refresh tick scheduled | 2–3 |
| Activity feed | `ActivityEvent` received → added to feed; last 20 retained; color coding correct | 3 |
| Browse table | Search input filters table; `v` triggers similarity query; `enter` queues track | 6 |
| Player | `space` toggles play/pause; `↑`/`↓` adjusts volume value; queue management | 7 |

### End-to-End Tests (`tests/e2e/`)

Headless mode is the e2e test entry point. Tests run the actual binary as a subprocess.

#### Architecture

```
tests/e2e/
├── run.sh                 # Main test runner
├── assertions.go          # Go helpers: verify DB schema, counts, embeddings, cursor
└── fixture/
    ├── mock_ia_server.go  # Optional: mock IA API for fast local tests
    └── testdata/          # Small test MP3 files
```

#### Test Scenarios

##### E2E-1: Headless Startup / Shutdown

```
1. Start: parso-indexer --headless --db-path /tmp/test_e2e.db --workers 0
2. Wait 3 seconds
3. Send SIGINT
4. Assert: exit code 0, DB file exists with correct schema, no rows
```

##### E2E-2: Coordinator Populates Queue

```
1. Start: parso-indexer --headless --db-path /tmp/test_e2e_2.db --workers 0
2. Monitor stdout for queue_added events (parse JSON lines)
3. Wait until 2+ queue_added events received (coordinator has added 2000+ IDs)
4. Send SIGINT
5. Assert: catalog_queue has rows with status=pending; cursor_state has last cursor
6. Assert: exit code 0
```

##### E2E-3: Worker Processes Tracks

```
1. Start: parso-indexer --headless --db-path /tmp/test_e2e_3.db --workers 2
2. Wait for coordinator to populate queue (monitor stdout)
3. Monitor stdout for analysis_complete events
4. Wait until 5+ analysis_complete events
5. Send SIGINT
6. Assert: track_embeddings has 5+ rows; quality_score is valid float; embedding length 40
7. Assert: catalog_queue has 5+ rows with status=completed
```

##### E2E-4: Crash Recovery

```
1. Start headless, let populate queue + process a few tracks
2. Kill -9 (hard crash — no graceful shutdown)
3. Assert: some rows stuck in status=processing
4. Restart headless
5. Assert: ResetStuckJobs runs on startup, stuck rows reset to pending
6. Let workers process again
7. Assert: no duplicate embeddings
```

##### E2E-5: Resume After Shutdown

```
1. Start headless, let coordinator run for 2+ pages
2. SIGINT (clean shutdown)
3. Record: pending count, cursor value
4. Restart headless
5. Assert: coordinator resumes from saved cursor (doesn't re-add same IDs; pending count doesn't jump by large number of duplicates)
```

##### E2E-6: Vector Similarity Query

```
1. Start headless, process 10+ tracks
2. SIGINT
3. Open DB directly via Go test helper
4. Call QuerySimilar for a completed track
5. Assert: returns 5 results; distances are valid floats between 0 and 2 (cosine distance); results exclude the query track itself
```

#### Test Runner (`tests/e2e/run.sh`)

```bash
#!/bin/bash
set -euo pipefail

DB_PATH="${DB_PATH:-/tmp/parso_e2e_$$.db}"
BINARY="${BINARY:-go run ./cmd/tui}"
TIMEOUT="${TIMEOUT:-120s}"

echo "=== E2E Test Suite ==="
echo "Binary: $BINARY"
echo "DB:     $DB_PATH"

cleanup() {
    rm -f "$DB_PATH" "$DB_PATH-wal" "$DB_PATH-shm"
    echo "Cleaned up $DB_PATH"
}
trap cleanup EXIT

# Test 1: Startup/Shutdown
echo "--- Test 1: Headless Startup/Shutdown ---"
timeout $TIMEOUT $BINARY --headless --db-path "$DB_PATH" --workers 0 &
PID=$!
sleep 3
kill -INT $PID
wait $PID || true
sqlite3 "$DB_PATH" ".tables" | grep -q catalog_queue && echo "PASS: DB schema created" || echo "FAIL: DB schema missing"

# Test 2-6: (similar pattern — start, monitor stdout with grep/json parsing, assert)
# ...

echo "=== All E2E Tests Complete ==="
```

#### Test Commands

```bash
# Unit tests
go test ./internal/...

# Integration tests (build tag)
go test -tags=integration ./internal/...

# E2E tests (requires real IA API access)
./tests/e2e/run.sh

# E2E tests with custom binary (for CI)
BINARY=./bin/parso-indexer ./tests/e2e/run.sh
```

## Test Fixtures

| Fixture | Purpose | Phase |
|---|---|---|
| `testdata/sine_440hz_5s.mp3` | Clean 440Hz sine wave — verify SNR > 40 dB | 5 |
| `testdata/noise_5s.mp3` | White noise — verify SNR < 5 dB | 5 |
| `testdata/ia_scrape_response.json` | Mock IA Scraping API response for unit tests | 4 |
| `testdata/ia_item_metadata.json` | Mock IA metadata response with .mp3 file info | 5 |
| `testdata/small_30s.mp3` | Real 30s MP3 for integration tests | 5 |

## CI Requirements (Future)

Not applicable in Phase 1 (local-only). When CI is added:
- `go test ./...` on every push
- Integration tests skip in CI unless IA_API_TEST=1
- E2E tests run nightly (require real IA access)
- `go vet ./...` and staticcheck
