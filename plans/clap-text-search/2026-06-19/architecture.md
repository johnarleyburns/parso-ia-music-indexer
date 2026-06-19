# Architecture

## Current Architecture

```
                        +-----------------+
  IA Scrape API ------> | coordinatorLoop | --> BulkInsertCollectionAlbums
                        +-----------------+
                                |
                        +-----------------+
                        | albumResolver   | --> InsertTracks
                        +-----------------+
                                |
                        +-----------------+
  IA Download --------> | analyzeTrack    |
                        |   decode MP3    |
                        |   quality gate  |
                        |   MFCC + chroma |
                        |   CLAP (gRPC)   |
                        |   FuseFeatures  |
                        |   SaveEmbedding | --> track_embeddings (564-dim f32 blob)
                        +-----------------+

  Python Sidecar (gRPC):
    GetEmbedding(pcm) -> 512-dim CLAP audio embedding (L2-normalized)
```

## Target Architecture

```
                        +-----------------+
  IA Scrape API ------> | coordinatorLoop |
                        |  LibriVox filter| --> BulkInsertCollectionAlbums (filtered)
                        +-----------------+
                                |
                        +-----------------+
                        | albumResolver   | --> InsertTracks
                        +-----------------+
                                |
                        +-----------------+
  IA Download --------> | analyzeTrack    |
                        |   decode MP3    |
                        |   quality gate  |
                        |   MFCC + chroma |
                        |   CLAP (gRPC)   |
                        |   L2-normalize  |
                        |   encodeF16     |
                        |   SaveComponent | --> track_embeddings_v2
                        |                 |     (clap f16, mfcc f16, chroma f16)
                        +-----------------+

  Python Sidecar (gRPC):
    GetEmbedding(pcm)   -> 512-dim CLAP audio embedding (L2-normalized, f32 wire)
    GetTextEmbedding(s)  -> 512-dim CLAP text embedding  (L2-normalized, f32 wire)

  Query paths:
    QuerySimilar(trackID)  -> decode f16 components, fuse at read time, cosine rank
    SearchByText(query)    -> GetTextEmbedding -> dot product vs stored clap f16 blobs
```

## Component Changes

### Python Sidecar (`python_sidecar/server.py`)

- **Keep** `full_model.audio_model` and `full_model.audio_projection` (unchanged).
- **Retain** `full_model.text_model` and `full_model.text_projection` instead
  of deleting them. ~125M extra params (~500MB). This is acceptable for the
  desktop indexer; the iOS app does not run this sidecar.
- **Add** `GetTextEmbedding` handler: runs
  `model.get_text_features(**processor(text=[...], ...))` through text_model +
  text_projection, L2-normalizes, returns 512-dim f32 vector.
- No second model load. Same checkpoint, same device, same eval mode.

### Proto (`proto/clap.proto`)

- Add `rpc GetTextEmbedding(TextEmbeddingRequest) returns (EmbeddingResponse);`
- Add `message TextEmbeddingRequest { string text = 1; }`
- Regenerate Go + Python stubs via `make proto`.

### Go Client (`internal/clap/`)

- `CLAPClient` interface: add `GetTextEmbedding(ctx, text) ([]float32, error)`.
- `grpc_client.go`: implement by calling the new RPC.
- `client.go` mockCLAPClient: return a deterministic 512-dim constant vector.

### f16 Codec (`internal/db/embeddings.go`)

- Add `encodeF16([]float32) []byte` and `decodeF16([]byte) []float32`.
- Use `github.com/x448/float16` for IEEE-754 half-precision conversion.
- Placed alongside existing `encodeF32`/`decodeF32`.
- CLAP vectors are L2-normalized before f16 encoding (values in [-1, 1]).
- MFCC/chroma blocks: per-block L2-normalization (see decisions.md).

### Schema Migration (`internal/db/db.go`)

- Add `track_embeddings_v2` table (additive; old table kept).
- Columns: `track_id`, `clap` BLOB, `mfcc` BLOB, `chroma` BLOB,
  `model_version` TEXT, `dim` INTEGER, `dtype` TEXT, `quality_score` REAL,
  `created_at` TEXT.
- Once v2 is backfilled and verified, reads switch to v2. Old table remains
  until a future cleanup phase.

### Unfused Storage (`cmd/tui/main.go:analyzeTrack`)

- Stop calling `FuseFeatures` before save.
- L2-normalize `clapVec`.
- Call `SaveComponentEmbedding(trackID, clapVec, mfccVec, chromaVec, quality)`.

### Query-Time Fusion (`internal/db/embeddings.go:QuerySimilar`)

- Read component blobs from v2 table.
- Decode f16 -> f32.
- Apply `FuseFeatures` weights to reconstruct 564-dim hybrid.
- Cosine-rank as before. Regression test validates parity.

### Text Search (`SearchByText`)

- New function: calls sidecar `GetTextEmbedding`, L2-normalizes, then
  dot-products against stored `clap` f16 blobs (decoded to f32).
- Returns ranked `(title, album, distance)`.

### LibriVox Filter (`cmd/tui/main.go:discoverCollection`)

- Before `BulkInsertCollectionAlbums`, filter `[]AlbumInsert` through a
  denylist loaded from `data/librivox_denylist.json`.
- Config toggle in `internal/config/config.go`: `LibrivoxDenylistPath` with
  default `data/librivox_denylist.json`.

## Unchanged Components

- `internal/audio/` -- MP3 decode, quality metrics, MFCC, chroma. No changes.
- `internal/ia/` -- IA API client. No changes.
- `internal/tui/` -- TUI model. No changes (events still flow the same).
- `internal/rate/` -- Rate limiting. No changes.
- Album resolver loop, worker loop -- unchanged structure.
- `FuseFeatures` function body -- unchanged, just called at query time instead
  of save time.
