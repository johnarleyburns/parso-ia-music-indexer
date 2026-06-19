# Implementation Plan

## Phase 1: f16 Codec + Tests (Workstream B Foundation)

**Branch:** `feature/clap-text-f16-codec`

### Files Modified
- `go.mod` / `go.sum` -- add `github.com/x448/float16`
- `internal/db/embeddings.go` -- add `encodeF16`, `decodeF16`, `l2Normalize`
- `internal/db/db_test.go` -- add round-trip tests for f16 codec

### Changes
1. `go get github.com/x448/float16`
2. In `internal/db/embeddings.go`, add:
   - `func l2Normalize(v []float32) []float32` -- returns a new L2-normalized
     copy. Returns zero vector if input norm is zero.
   - `func encodeF16(v []float32) []byte` -- converts each f32 to f16
     (2 bytes LE) using `float16.Fromfloat32()`.
   - `func decodeF16(b []byte) []float32` -- converts each 2-byte LE f16 back
     to f32 using `float16.Float16.Float32()`.
3. Tests:
   - Round-trip: f32 -> f16 -> f32 within half-precision tolerance (~1e-3 for
     normalized values).
   - Blob size: 512-dim -> 1024 bytes.
   - Edge cases: zero vector, negative values, values at f16 limits.
   - L2-normalize: verify unit norm output, zero input returns zero.

### Verification
- `make build` green.
- `make test` green, report pass count.

---

## Phase 2: Schema Migration + Component Storage + Query-Time Fusion (C + D Storage)

**Branch:** `feature/clap-text-unfused-storage`

### Files Modified
- `internal/db/db.go` -- add `track_embeddings_v2` CREATE TABLE in `migrate()`
- `internal/db/embeddings.go` -- add `SaveComponentEmbedding`,
  `QuerySimilarV2`, `BackfillV2FromV1`, update `QuerySimilar` to read from
  v2 with fallback to v1
- `internal/db/db_test.go` -- add tests for v2 storage, query-time fusion
  parity, backfill
- `cmd/tui/main.go` -- change `analyzeTrack` to call `SaveComponentEmbedding`
  instead of `FuseFeatures` + `SaveEmbedding`

### Changes

#### Schema (db.go)
Add to `migrate()` queries:
```sql
CREATE TABLE IF NOT EXISTS track_embeddings_v2 (
    track_id       INTEGER PRIMARY KEY REFERENCES tracks(id),
    clap           BLOB NOT NULL,
    mfcc           BLOB NOT NULL,
    chroma         BLOB NOT NULL,
    model_version  TEXT NOT NULL DEFAULT 'clap-htsat-fused:audio+text:512:l2:f16',
    dim            INTEGER NOT NULL DEFAULT 512,
    dtype          TEXT NOT NULL DEFAULT 'f16',
    quality_score  REAL,
    created_at     TEXT NOT NULL DEFAULT (datetime('now'))
)
```

#### SaveComponentEmbedding (embeddings.go)
```go
func SaveComponentEmbedding(db *sql.DB, trackID int, clap, mfcc, chroma []float32, qualityScore float64) error
```
- L2-normalize `clap`.
- Encode all three as f16 blobs.
- INSERT OR REPLACE into `track_embeddings_v2`.
- Also write into `track_embeddings` (v1) for backward compat: fuse + encodeF32.
  This dual-write ensures the old code path still works during migration.

#### QuerySimilar Update (embeddings.go)
- Check if `track_embeddings_v2` has data for the query track.
- If yes: load components, decode f16 -> f32, apply `FuseFeatures`, cosine-rank.
- If no: fall back to v1 path (existing behavior).
- Candidates from both tables can coexist during migration.

#### BackfillV2FromV1 (embeddings.go)
```go
func BackfillV2FromV1(db *sql.DB) (int, error)
```
- SELECT all rows from `track_embeddings` that are NOT in `track_embeddings_v2`.
- Decode f32 blob, extract components by slicing:
  - `clap = blob[0:512] / 0.60` then L2-normalize
  - `mfcc = blob[512:552] / 0.25`
  - `chroma = blob[552:564] / 0.15`
- Encode as f16, insert into v2.
- Return count of backfilled rows.
- For rows with len != 564 (old 40-dim embeddings), skip.

#### analyzeTrack Change (main.go)
- Remove: `hybridVec := hybrid.FuseFeatures(clapVec, mfccVec, chromaVec)`
- Remove: `db.SaveEmbedding(sqlDB.Conn, track.ID, hybridVec, compositeScore)`
- Add: `db.SaveComponentEmbedding(sqlDB.Conn, track.ID, clapVec, mfccVec, chromaVec, compositeScore)`

### Verification
- Regression test: fixed fixture vectors through v1 path and v2 path produce
  identical QuerySimilar rankings (within f16 tolerance).
- `make build` + `make test` green.

---

## Phase 3: Sidecar Text RPC + Go Client (Workstream A)

**Branch:** `feature/clap-text-rpc`

### Files Modified
- `proto/clap.proto` -- add `GetTextEmbedding` RPC + `TextEmbeddingRequest`
- `internal/clap/clap_proto/` -- regenerated stubs
- `python_sidecar/clap_pb2.py`, `python_sidecar/clap_pb2_grpc.py` --
  regenerated Python stubs
- `python_sidecar/server.py` -- add text embedding handler, retain text
  model + projection
- `internal/clap/client.go` -- add `GetTextEmbedding` to interface + mock
- `internal/clap/grpc_client.go` -- implement `GetTextEmbedding`
- `internal/clap/client_test.go` -- test mock returns 512-dim text embedding

### Proto Changes
```protobuf
rpc GetTextEmbedding (TextEmbeddingRequest) returns (EmbeddingResponse);

message TextEmbeddingRequest {
  string text = 1;
}
```

### Python Sidecar Changes
1. In `__init__`: keep `full_model.text_model` and `full_model.text_projection`
   (remove the `del full_model` + gc pattern; keep only a reference).
2. Move text_model and text_projection to same device, same eval mode, same
   fp16 policy as audio.
3. Add `GetTextEmbedding` handler:
   ```python
   def GetTextEmbedding(self, request, context):
       inputs = self.processor(text=[request.text], return_tensors="pt", padding=True)
       input_ids = inputs["input_ids"].to(self.device)
       attention_mask = inputs["attention_mask"].to(self.device)
       with torch.no_grad():
           text_outputs = self.text_model(input_ids=input_ids, attention_mask=attention_mask)
           text_features = self.text_projection(text_outputs.pooler_output)
           text_features = F.normalize(text_features, dim=-1)
       embedding = text_features.cpu().numpy().flatten().tolist()
       return clap_pb2.EmbeddingResponse(embedding=embedding)
   ```

### Go Client Changes
- `CLAPClient` interface: add `GetTextEmbedding(ctx context.Context, text string) ([]float32, error)`
- `grpcCLAPClient`: implement via `c.client.GetTextEmbedding(ctx, &pb.TextEmbeddingRequest{Text: text})`
- `mockCLAPClient`: return deterministic 512-dim vector (e.g. each element = 1/sqrt(512) for unit norm).

### Verification
- Unit test: mock returns 512 dims, non-zero norm.
- Integration test (when sidecar running): embed "melancholy piano" and
  assert len==512, norm ~= 1.0.
- `make build` + `make test` green.

---

## Phase 4: SearchByText End-to-End (Workstream D Search)

**Branch:** `feature/clap-text-search`

### Files Modified
- `internal/db/embeddings.go` -- add `SearchByText`, `dotProduct` helper
- `internal/db/db_test.go` -- add text search test
- `cmd/tui/main.go` or new `cmd/search/main.go` -- CLI proof function

### Changes

#### dotProduct (embeddings.go)
```go
func dotProduct(a, b []float32) float64
```
For L2-normalized vectors, dot product = cosine similarity. Simpler and faster
than `cosineDistance` which normalizes on the fly.

#### SearchByText (embeddings.go)
```go
func SearchByText(db *sql.DB, queryVec []float32, limit int) ([]SimilarTrack, error)
```
- L2-normalize `queryVec` (should already be, but be safe).
- SELECT `track_id, clap, quality_score` + track/album join from v2 table.
- Decode f16 clap blob, dot product against queryVec, rank descending.
- Return top `limit` results with title, album, distance (1 - dot product).

#### CLI Proof
- Add a `--search-text "query"` flag to the existing binary, or a lightweight
  subcommand. When invoked:
  1. Open DB.
  2. Start/connect sidecar.
  3. Call `GetTextEmbedding(query)`.
  4. Call `SearchByText(db, textVec, 20)`.
  5. Print ranked results.
  6. Exit.

### Verification
- Test with fixture: store known CLAP vectors, embed a text query (mock),
  verify ranking.
- Manual test: `./bin/timbre --search-text "melancholy solo piano"` against
  indexed corpus returns sensible results.
- `make build` + `make test` green.

---

## Phase 5: LibriVox Discovery Filter (Workstream E)

**Branch:** `feature/clap-text-librivox-filter`

### Files Modified
- `data/librivox_denylist.json` -- new file, JSON array of identifier strings
- `internal/config/config.go` -- add `LibrivoxDenylistPath` field + flag
- `cmd/tui/main.go` -- load denylist, filter in `discoverCollection`
- `internal/db/db_test.go` or new test file -- unit test for filter logic

### Changes

#### Denylist File
`data/librivox_denylist.json` seeded with high-download LibriVox identifiers.
See decisions.md for the starter set.

#### Filter Function
```go
func filterDenylisted(albums []db.AlbumInsert, denylist map[string]bool) []db.AlbumInsert
```
- Iterate albums, skip any whose `Identifier` is in the denylist map.
- Return filtered slice.

#### Integration Point
In `discoverCollection`, after `resp.Items` is mapped to `[]db.AlbumInsert`
and before `BulkInsertCollectionAlbums`:
```go
albums = filterDenylisted(albums, denylist)
```
Update the progress event `Message` to reflect post-filter counts.

#### Config
```go
LibrivoxDenylistPath string  // flag: --librivox-denylist
```
Default: `data/librivox_denylist.json`. If file doesn't exist, skip filtering
(no error, just a log message).

### Verification
- Unit test: denylisted IDs are dropped, others pass through.
- `make build` + `make test` green.

---

## Phase 6 (Deferred): Segment-Level Vectors (Workstream D-bonus)

Not implemented until core phases land and are approved. See data-model.md
for the proposed `track_segments` schema.
