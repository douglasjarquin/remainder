# Remainder

Remainder is a Go quota CLI with a standard-library core and Cobra command layer.

## Commands

- `mise run build` builds `bin/remainder` with `CGO_ENABLED=0`.
- `mise run test` runs race-enabled, shuffled Go tests.
- `mise run verify` runs `scripts/verify.sh`.
- `mise run benchmark` records full-process startup samples for the compiled binary.
- Direct equivalents are `go build ./cmd/remainder`, `go test -race -shuffle=on -count=1 ./...`, and `./scripts/verify.sh`.

## Architecture

- `cmd/remainder/main.go` owns OS signal and process-stream wiring.
- `internal/cli/run.go` owns flag parsing, output, and exit semantics.
- Provider collection is intentionally not implemented in this slice.

## Constraints

- Go 1.27.1 is the canonical developer and CI toolchain pin.
- Runtime and ordinary tests use the Go standard library plus the pinned Cobra graph.
- Release-like builds use `CGO_ENABLED=0`.
- The CLI performs no credential, cache, network, telemetry, or provider access.
- Future provider commands must preserve the one-shot CLI contract and honest unavailable behavior.
