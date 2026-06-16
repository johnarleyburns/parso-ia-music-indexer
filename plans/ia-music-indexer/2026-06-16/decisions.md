# Design Decisions (Revision 2)

## Decision 1: Language — Go vs Python

**Decision**: Go

**Rationale**: User familiarity, goroutines for concurrent audio analysis, single statically-linked binary, lower memory footprint, superior for distributed edge-node deployment.

**Tradeoff**: Smaller ML ecosystem. Accepted because MFCC extraction is well-defined and `go-mfcc` exists.

---

## Decision 2: Database — SQLite + sqlite-vec vs PostgreSQL + pgvector

**Decision**: SQLite + sqlite-vec for local; Turso/libSQL for distributed (future)

**Rationale**: Zero setup, single file, perfect for laptop-first development. ~1 GB total size fits easily. Migration path to Turso/libSQL is wire-compatible.

**Tradeoff**: Newer library, limited concurrent write throughput. Acceptable for Phase 1.

---

## Decision 3: API Strategy — Cursor-based Scraping API vs Advanced Search API

**Decision**: IA Scraping API (`/services/search/v1/scrape`) with cursor pagination

**Rationale**: Advanced Search API has hard 10,000 result limit. Scraping API supports unlimited cursor-based traversal and `sorts=downloads desc`.

**Tradeoff**: Less documented. Mitigated by saving cursor state to DB and file.

---

## Decision 4: Audio Analysis — 20 MFCC bands, 30 seconds, 40-dim vector

**Decision**: 20 MFCC bands from first ~30s, mean+variance per band → 40-dim float32

**Rationale**: Industry standard for MIR. 30s captures enough for genre/timbre. Mean+variance captures central tendency and dynamic range. 40 dimensions is compact for fast similarity search.

**Tradeoff**: May miss long intros or classical pieces where first 30s is ambient. Configurable offset in future.

---

## Decision 5: Quality Score — SNR from PCM

**Decision**: Signal-to-Noise Ratio from raw PCM samples (dB scale)

**Rationale**: Directly measures recording cleanliness. No external metadata. 78rpm records naturally score low; modern recordings score high. Computationally cheap.

**Tradeoff**: Single dimension — doesn't capture dynamic range compression or stereo imaging. Sufficient for Phase 1.

---

## Decision 6: Discovery vs Analysis — Decoupled Coordinator/Worker

**Decision**: Separate coordinator (metadata discovery) from worker pool (audio analysis), both as goroutines within the same binary.

**Rationale**: Coordinator is I/O bound (rate limited), benefits from single instance. Workers are bandwidth bound, benefit from N concurrent goroutines. Decoupling allows coordinator to populate queue independently. Enables clean distributed scale-out later.

**Tradeoff**: Slightly more complex goroutine management. Worth it for architectural clarity.

---

## Decision 7: Orchestration — TUI (not Shell Scripts)

**Decision**: Single binary with Bubble Tea TUI as primary interface; headless mode for automation.

**Rationale**: User requested "scriptless" design. TUI provides live progress, interactive controls, search, and playback in one tool. No shell scripts, no ad-hoc Go programs. Headless mode serves CI/e2e needs.

**Tradeoff**: More complex initial build (TUI layer). Worth it for unified UX.

---

## Decision 8: Concurrency Model — Goroutine Pool

**Decision**: Worker uses goroutine pool with configurable concurrency (default 2, max configurable). Coordinator is a single goroutine.

**Rationale**: Goroutines are lightweight. 2 concurrent downloads is safe against single-IP IA rate limits. User can increase for multiple IPs. Channel-based: queue batch → channel → worker goroutines → results → DB write (serialized via mutex).

**Tradeoff**: Must ensure SQLite write serialization. Use mutex-protected write methods in `internal/db`.

---

## Decision 9: HTTP Range Header Max Bytes

**Decision**: `Range: bytes=0-1600000` (~1.6 MB) default. Configurable via `MAX_STREAM_BYTES`.

**Rationale**: 30s of MP3 at 128 kbps ≈ 480 KB; at 320 kbps ≈ 1.2 MB. 1.6 MB is generous margin. IA MP3s are typically 128–192 kbps. Configurable for tuning.

---

## Decision 10: Future Distribution Strategy

**Decision**: Phase 1 = local SQLite; Phase 2 = Turso/libSQL + colima/incus deployment (future).

**Rationale**: SQLite is wire-compatible with libSQL/Turso. Same Go binary, connection string swap. Reference pattern exists at `../fast-internet-portal`.

**Status**: Design only. Not implemented in Phase 1.

---

## Decision 11: Single Binary, Two Modes (TUI + Headless)

**Decision**: One `cmd/tui/main.go` entry point. `--headless` flag switches between interactive TUI and headless JSON-logging mode.

**Rationale**: "Scriptless" means no separate coordinator/worker/search binaries. Single binary reduces build complexity and ensures internal packages are always tested together. Headless mode is essential for CI/e2e testing and future server deployment — same code paths, same logic, just different output.

**Tradeoff**: Binary includes TUI dependencies even in headless mode. Acceptable — Go compiles unused imports out; bubbletea/lipgloss are not massive.

---

## Decision 12: Bubble Tea TUI Framework

**Decision**: `charm.land/bubbletea/v2` with `bubbles/v2` and `lipgloss/v2`

**Rationale**: Most popular Go TUI framework (43k+ GitHub stars). Elm Architecture (Model/Update/View) enforces clean state separation. Rich pre-built components (viewport, table, textinput, spinner, progress, help). Active ecosystem. Cross-platform terminal support.

**Tradeoff**: Custom tab bar needed (no built-in tab component). Acceptable — Lip Gloss borders and styles handle this well. Learning curve for Elm Architecture pattern.

---

## Decision 13: Coordinator/Workers as Goroutines, Not Subprocesses

**Decision**: Coordinator and worker pool run as goroutines within the TUI binary, communicating via Go channels.

**Rationale**: Same binary, same address space — no IPC overhead. Channels are idiomatic Go for goroutine communication. Channel-based event stream feeds directly into bubbletea's message loop. Graceful shutdown via context cancellation is straightforward.

**Tradeoff**: A panic in a worker goroutine could crash the entire app. Mitigated by `recover()` in each goroutine's main loop.

---

## Decision 14: `gopxl/beep/v2` for Audio Playback

**Decision**: `github.com/gopxl/beep/v2` (active fork of `faiface/beep`)

**Rationale**: Shares `go-mp3` decoder with MFCC pipeline (no duplicate decoder dependency). Streamer abstraction for play/pause/seek/volume. Oto backend is cross-platform (macOS, Linux, Windows). Can stream from `io.Reader` (HTTP response body, no disk write).

**Tradeoff**: `beep/v2/mp3` may not fully support streaming from non-seekable `io.Reader` for all operations (seeking requires re-decoding). If seeking from HTTP stream fails, we'll buffer the full MP3 in memory (typical IA MP3 is <5 MB for a 3-min track at 192 kbps — acceptable for playback, not for analysis). Speaker initialization must happen once; sample rate is fixed per session.

---

## Decision 15: Event Channels (TUI ↔ Goroutines)

**Decision**: Bidirectional Go channels — `chan ActivityEvent` (goroutines→TUI) and `chan ControlCmd` (TUI→goroutines).

**Rationale**: Clean separation — TUI owns display state, goroutines own I/O. Channels are buffered to prevent blocking. Event struct carries all needed context (type, timestamp, identifier, data). Bubbletea's `tea.Batch` can read from event channel and convert to messages. Control commands are fire-and-forget into buffered channel.

**Tradeoff**: Type coupling — TUI must import event types. Acceptable; they're in the same module.

---

## Decision 16: Phase Exit Criteria = TUI-Runnable

**Decision**: Every implementation phase MUST end with a runnable TUI where the phase's results are visible and interactive. No phase is complete until the user can manually verify the deliverables.

**Rationale**: User explicitly requested ability to test each phase manually before proceeding. Prevents accumulating untested code. Ensures integration works from the start (not a "big bang" at the end). Forces clean phase boundaries.

**Tradeoff**: Slower initial development (building TUI scaffolding early). Worth it — catches integration issues immediately.

---

## Decision 17: Headless Mode Uses JSON-Structured stdout

**Decision**: Headless mode logs structured JSON lines to stdout, errors to stderr. No TUI rendering at all.

**Rationale**: Machine-parseable for CI/e2e tests. Human-readable enough for debugging. Same internal packages, same coordinator/worker goroutines, same code paths as TUI mode. Exit codes: 0 = clean shutdown, 1 = fatal error.

**Tradeoff**: Less visually appealing for manual headless use. Acceptable — headless is for automation, not interactive use.
