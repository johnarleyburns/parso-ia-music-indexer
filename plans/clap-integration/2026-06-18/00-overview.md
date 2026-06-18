# CLAP gRPC Integration — Real Client Implementation

**Date:** 2026-06-18
**Status:** IN PROGRESS

## Problem

The CLAP client in `internal/clap/client.go` is currently a mock that returns a fixed 512-dim vector of `0.01` values. The Python sidecar (`python_sidecar/server.py`) implements the real CLAP inference via gRPC on port 50051, but the Go binary has no gRPC client to communicate with it.

Additionally, `cmd/tui/main.go:557` passes `nil` as PCM data to `GetEmbedding` — the real client needs actual PCM bytes.

Known blockers from `current_state.md`:
- #4: CLAP model memory (~300MB, verify on 8GB MacBooks)
- #5: gRPC proto generation — Need protoc toolchain or committed generated code

## Current Behavior

- `internal/clap/client.go`: Interface + mock only (31 lines)
- `cmd/tui/main.go:63`: `clapClient := clap.NewMockClient()`
- `cmd/tui/main.go:557`: `clapVec, err := clapClient.GetEmbedding(clapCtx, nil, int32(sampleRate))`
- `proto/clap.proto`: Proto definition exists but no generated code
- `python_sidecar/server.py`: Imports `clap_pb2` and `clap_pb2_grpc` (not yet generated)
- `go.mod`: No gRPC or protobuf dependencies
- Config: `ClapHost`/`ClapPort` flags exist but are unused

## Design

### Approach: Graceful Fallback

The real gRPC client connects to the Python sidecar. If the sidecar is unavailable:
- At startup: log a warning and fall back to mock client
- During analysis: CLAP errors trigger retry (existing retry logic handles this)

This preserves the existing behavior — the binary works without the Python sidecar, just with mock CLAP vectors.

### Proto Generation Strategy

Install `protoc` + Go plugins, generate Go stubs, commit them to the repo. This avoids requiring the protoc toolchain at build time.

Generated code lives at: `internal/clap/clap_proto/`

### PCM Conversion

Float32 PCM samples must be packed to `[]byte` (little-endian float32) for gRPC transmission. Add a helper in `internal/clap/`.

## Implementation Phases

### Phase 1: Proto toolchain + code generation
- Install protoc + protoc-gen-go + protoc-gen-go-grpc
- Generate Go stubs → `internal/clap/clap_proto/`
- Generate Python stubs → `python_sidecar/`
- Add gRPC deps to go.mod

### Phase 2: Real gRPC client
- Implement `grpcCLAPClient` struct in `internal/clap/grpc_client.go`
- Add `NewGRPCClient(host string, port int) (CLAPClient, error)`
- Add `float32ToBytes()` helper
- Implement `GetEmbedding`, `HealthCheck`, `Close`

### Phase 3: Wire into main.go
- Replace `clap.NewMockClient()` with `clap.NewGRPCClient()` + fallback
- Fix `analyzeTrack` to pass actual PCM bytes to `GetEmbedding`

### Phase 4: Makefile + tests + verification
- Add `proto` target to Makefile
- Add client tests
- Run full test suite + build

### Phase 5: Sidecar lifecycle management
- Add `internal/clap/sidecar.go` — `EnsureSidecar()` checks if sidecar is running, starts it if not, errors on failure
- Add `--clap-sidecar-dir` config flag (default: `python_sidecar`)
- Move CLAP client creation from `runCoordinator` to `runTUI`/`runHeadless`
- Hard error (os.Exit) if sidecar cannot connect after auto-start
- Sidecar process killed on program exit (SIGTERM → SIGKILL)
- 5 sidecar tests (nil stop, missing script, no python, status callback)

## Testing Strategy

- Unit: Mock client tests (existing behavior preserved)
- Unit: `float32ToBytes` roundtrip test
- Unit: `NewGRPCClient` with unreachable host returns error gracefully
- Unit: Sidecar nil/empty stop safety
- Unit: EnsureSidecar missing script / no python error paths
- Unit: Status callback fires during EnsureSidecar
- Integration: With running Python sidecar (manual)
- Build: `make build` succeeds
- Regression: All 72 tests pass

## Decisions

1. **Generated code committed to repo** — avoids protoc requirement at build time
2. **Graceful fallback to mock** — binary works without sidecar
3. **Separate file for gRPC client** — `grpc_client.go` keeps concerns isolated
