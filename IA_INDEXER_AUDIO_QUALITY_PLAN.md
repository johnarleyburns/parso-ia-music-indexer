# Audio Vector Quality & Export Reproducibility Plan — parso-ia-music-indexer

**Type:** Agent handoff document / planning seed. On acceptance, the agent should copy this into
`plans/audio-vector-quality/2026-06-25/PLAN.md` (per `AGENTS.md` → Persistent Planning) and work from there.
**Repo:** `parso-ia-music-indexer`
**Companion doc:** `ACALUM_RECOMMENDATION_FIX_PLAN.md` (sibling `parso-acalum-ios-app`). That iOS plan fixes the user-visible search bugs on its own. **This plan is independent and additive** — it improves the *quality* of what the app retrieves and closes the model-bundling gap between the two repos.

---

## Context

The Acalum app does text→audio semantic search over CLAP vectors this indexer produces, plus a precomputed `tags` keyword bag (`internal/db/tags.go` `GenerateTags`). Audit of both repos found:

- **Alignment is correct.** Audio (`python_sidecar/server.py` `GetEmbedding`: `audio_projection → F.normalize`) and the iOS text encoder (`export_for_ios.py`: `text_projection → F.normalize`) share the `laion/clap-htsat-fused` checkpoint; DB stamps `model_version='clap-htsat-fused:audio+text:512:l2:f16'`. Nothing to fix here.
- **Audio vectors are opening-biased.** `StreamAudioFromURL` (`internal/audio/stream.go`) reads via `io.LimitReader` from **byte 0**, capped at `MaxBytes = 1_600_000` (`internal/config/config.go:31`, "~30s MP3"). For LP rips / classical / chant, the opening is frequently silence, applause, or a spoken announcer intro → an unrepresentative CLAP audio vector → weaker text→audio ranking even with a perfect text encoder. This is the main *quality* lever.
- **The iOS text model is not reproducibly built.** `AcalumCLAPTextEncoder.mlpackage` + tokenizer files are `.gitignore`d in the app and are the prime suspect for the app's "random results" symptom (no model bundled → silent fallback). There is **no Make target** here that produces and verifies them, so the cross-repo handoff is manual and easy to skip.
- **No retrieval eval exists.** Nothing measures whether "spanish guitar" text actually retrieves spanish-guitar audio from the real catalog — which is why the regression went unnoticed.

## Methodology

Follow `AGENTS.md`: **Research → Design → Plan → Implement → Verify → Refine**, incremental phases, binary is the deliverable (`make build` → `bin/timbre`; never `go run`). Verify with `go test ./...` / `make ci`; sidecar work uses `python_sidecar/.venv`.

---

## Phase 1 — Representative audio sampling (primary)

**Goal:** Embed a mid-track window (and optionally average a few windows) instead of the first ~1.6 MB, so the CLAP audio vector represents the *music*, not the lead-in.

**Files:** `internal/audio/stream.go`, `internal/config/config.go`, `cmd/tui/main.go` (pipeline at `:1004` stream → `:1060` decode → `:1117` embed).

**Design notes:**
- `StreamAudioFromURL` already accepts `206 Partial Content`, so HTTP `Range` is straightforward. Add a variant that issues `Range: bytes=<start>-<start+window>` where `start` is derived from the IA file size (from a `HEAD` or the metadata `length`/byte size already fetched) targeting ~30–40% into the file, skipping at least the first ~15–20 s to clear intros. Fall back to from-start streaming when size is unknown or the file is short.
- CLAP htsat-fused **already fuses internally** when audio is >10 s (`is_longer` in `server.py:95-97`). So a single longer mid-track window is the high-value, low-complexity win. **Multi-window averaging is a stretch goal:** embed 2–3 windows (e.g., 30/50/70%), average the L2-normalized vectors, then renormalize — a reasonable approximation of a track-level embedding. Keep behind a config flag.
- Add config: `--sample-strategy {head|midpoint|multiwindow}` (default `midpoint`) and `--sample-skip-seconds` (default ~20). Keep `MaxBytes` as the per-window cap.

**⚠️ Migration reality (call out in the plan doc):** changing the sampling **changes the stored vectors**. Existing rows were embedded `head`-style; new rows will differ. For the change to take effect the affected catalog must be **re-embedded**, and a partially re-embedded catalog has *mixed* sampling. Recommend: (a) record the strategy so it's auditable — either append to `model_version` (e.g. `…:f16:mid`) or add a `sample_strategy TEXT` column to `track_embeddings` (`internal/db/db.go` migrations + `internal/db/embeddings.go`); (b) support re-embedding only rows whose recorded strategy differs, so re-indexing is incremental and resumable.

**Verify:**
- Unit: given a known size, the computed `Range` lands mid-file and respects the skip floor; short-file fallback works. `go test ./internal/audio/...`.
- Empirical: re-embed a small known album (e.g. a Spanish-guitar and a Gregorian-chant collection from `ia_collections_final.json`) with `midpoint`, then run the Phase 3 eval and confirm those tracks rank higher for their natural-language queries than under `head`.
- `make ci` green.

---

## Phase 2 — First-class, verified iOS model export (`make export-ios`)

**Goal:** Make producing + validating the iOS text encoder a one-command, reproducible step so the app is never silently shipped without it.

**Files:** `Makefile`, `python_sidecar/export_for_ios.py` (already does the work and asserts Core ML vs PyTorch cosine ≥ 0.995 and emits `vocab.json`, `merges.txt`, `tokenizer_config.json`, `test_vectors.json`).

**Implement:** add a target that runs the export straight into the app's resources and fails loudly if parity drops:

```makefile
export-ios:
	cd python_sidecar && .venv/bin/python export_for_ios.py \
		--output-dir ../../parso-acalum-ios-app/Acalum/Resources/
	@echo "Exported CLAP text encoder + tokenizer + test_vectors.json to Acalum/Resources/"
```

(Adjust the relative path to wherever the sibling repo is checked out; document the assumption in the plan.) Optionally also emit `byte_encoder.json` for fully deterministic tokenization — the Swift `CLAPTokenizer` falls back to a built-in GPT-2 byte map when it's absent, so this is a nice-to-have, not required.

**Verify:** `make export-ios` populates `Acalum/Resources/` with the `.mlpackage` + tokenizer files + `test_vectors.json`; the script's built-in parity assertion passes; the app's `AcalumTests/CLAPTextEmbeddingServiceTests.testMatchPythonTestVectors` then passes against that `test_vectors.json`. Document the command in both repos' READMEs as the cross-repo handoff step.

---

## Phase 3 — Cross-modal retrieval eval harness (so quality is measurable)

**Goal:** A repeatable check that text queries retrieve the right audio from the **actual** catalog. This is the guardrail that would have caught the original regression.

**Files:** new `python_sidecar/eval_retrieval.py` (reuses the running sidecar's `GetTextEmbedding` and reads `data/parso_indexer.db`), plus a `Makefile` target.

**Implement:** for each of the 10 canonical prompts already in `export_for_ios.py` (and a few user-reported ones — "spanish guitar", "gregorian chant"), embed the text, cosine-rank against all `track_embeddings.clap` (decode f16 → float32, L2-normalized), and print the top-10 `(title, album_title, genres)`. Emit a small markdown/JSON report under `data/`.

```makefile
eval-retrieval:
	cd python_sidecar && .venv/bin/python eval_retrieval.py --db ../data/parso_indexer.db --top-k 10
```

**Verify:** Eyeball that "spanish guitar" / "gregorian chant" / "romantic classical guitar" return on-topic tracks. Capture before/after Phase 1 sampling to quantify the lift. (Optional: turn a couple of obvious cases into a pass/fail assertion — e.g. ≥3 of top-10 for "gregorian chant" carry a chant tag — and wire into `make ci` once stable.)

---

## Phase 4 — (Optional) Lexical-fuel coverage diagnostic

**Goal:** Quantify how much metadata the app's lexical channel has to work with.

**Files:** small Go report (e.g. extend `internal/tui` metrics or a `cmd` one-off) or a SQL snippet in the plan.

**Implement / report:** counts over `status='completed'` tracks of: empty/near-empty `tags`, albums missing `subjects`, albums missing `genres`. Confirm `GenerateTags` (called at `cmd/tui/main.go:919` and `:1344`) is populating tags for the bulk of the catalog. If `subjects`/`genres` are sparse for some collections, note which IA fields `internal/ia/metadata.go` maps and whether more can be pulled. This directly predicts how well the app's Phase 2 lexical retrieval performs.

**Verify:** report runs and prints coverage; no schema change required.

---

## Guardrails

- **Preserve the embedding contract:** any re-embedded vector stays `clap-htsat-fused`, 512-d, L2-normalized, f16. Keep audio and text paths identical to each other; do not "improve" one side's projection/normalization in isolation — that would silently break cross-modal alignment for the whole catalog.
- **Re-embedding is a migration, not a hot edit.** Make it incremental, resumable, and auditable (recorded sampling strategy). Don't leave the catalog in a half-migrated state without recording which rows are which.
- Build the binary (`make build`); never tell the user to `go run`. Run `make ci` (`go vet` + `go test -race`) before declaring done.
- Don't expand scope into UI/TUI features; this plan is about vector quality + reproducible export + measurement.

## Definition of done

- [ ] `--sample-strategy=midpoint` implemented with size-aware `Range` + short-file fallback; sampling strategy recorded; re-embed is incremental. Unit tests pass.
- [ ] `make export-ios` produces + validates the iOS model/tokenizer into `Acalum/Resources/`; app parity test passes against the emitted `test_vectors.json`.
- [ ] `make eval-retrieval` prints top-k audio neighbors for the canonical + user prompts; before/after sampling captured.
- [ ] (Optional) coverage diagnostic reported.
- [ ] `make ci` green; READMEs document `make export-ios` as the cross-repo handoff and `make eval-retrieval` as the quality gate; plan doc updated with results.

---

## Copy-paste agent kickoff prompt

```text
You are working in the parso-ia-music-indexer repo. Follow AGENTS.md:
Research → Design → Plan → Implement → Verify → Refine. First copy this handoff into
plans/audio-vector-quality/2026-06-25/PLAN.md and work from there. The deliverable is
the binary — always `make build` (bin/timbre), never `go run`; verify with `make ci`
(go vet + go test -race). Sidecar work uses python_sidecar/.venv.

Implement in four phases; propose nothing beyond scope without asking; report results
after each phase.

Phase 1 (primary): Representative audio sampling. Replace opening-biased streaming
(io.LimitReader from byte 0, MaxBytes in internal/config/config.go:31) with a
size-aware mid-track HTTP Range window in internal/audio/stream.go (it already accepts
206), wired through cmd/tui/main.go (stream :1004 / decode :1060 / embed :1117). Add
--sample-strategy {head|midpoint|multiwindow} (default midpoint) and
--sample-skip-seconds. Record the strategy (append to model_version or add a column)
and make re-embedding incremental/resumable. NOTE clearly that this requires
re-embedding for effect and that partial catalogs will be mixed. Multiwindow averaging
(avg of L2-normalized window vectors, renormalized) is a stretch goal behind the flag.

Phase 2: Add a `make export-ios` target that runs python_sidecar/export_for_ios.py into
../../parso-acalum-ios-app/Acalum/Resources/ (adjust path; document the assumption). The
script already asserts Core ML vs PyTorch cosine >= 0.995. Confirm the app's
testMatchPythonTestVectors passes against the emitted test_vectors.json.

Phase 3: Add python_sidecar/eval_retrieval.py + `make eval-retrieval`: for the 10
prompts in export_for_ios.py plus "spanish guitar" and "gregorian chant", embed text via
the sidecar's GetTextEmbedding and cosine-rank against all track_embeddings.clap from the
DB; print top-10 (title, album, genres). Capture before/after Phase 1.

Phase 4 (optional): A coverage report counting completed tracks with empty tags / albums
missing subjects/genres, confirming GenerateTags coverage.

Guardrails: preserve the embedding contract (clap-htsat-fused, 512-d, L2, f16; audio and
text paths must stay identical to each other — do not alter one side's projection/norm).
Re-embedding is a migration: incremental, resumable, auditable. Update both READMEs and
the plan doc with results.
```
