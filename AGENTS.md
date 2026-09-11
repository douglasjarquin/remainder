# Remainder

Remainder is a Go quota CLI with a standard-library core and Cobra command layer.

The project is positioned for personal use by its maintainer.

## Commands

- `mise run build` builds `bin/remainder` with `CGO_ENABLED=0`.
- `mise run test` runs race-enabled, shuffled Go tests.
- `mise run verify` runs `scripts/verify.sh`.
- `mise run benchmark` records full-process Cobra latency/output samples and in-process allocation benchmarks.
- Direct equivalents are `go build ./cmd/remainder`, `go test -race -shuffle=on -count=1 ./...`, and `./scripts/verify.sh`.
- Prime the pinned module cache once before offline verification with `GOTOOLCHAIN=local go mod download all`.

## Architecture

- `cmd/remainder/main.go` owns OS signal and process-stream wiring.
- `internal/cli/run.go` owns flag parsing, output, and exit semantics.
- `internal/codex` owns the native selected-profile credential read, bounded usage request, and Codex normalization.

## Constraints

- Go 1.27.1 is the canonical developer and CI toolchain pin.
- Runtime and ordinary tests use the Go standard library plus the pinned Cobra and go-toon graph.
- Release-like builds use `CGO_ENABLED=0`.
- Help, version, validation, and ordinary offline tests perform no credential, cache, network, telemetry, or provider access.
- An explicitly selected Codex/default report uses the account/source-bound cache and performs at most one bounded read-only credential read and provider request when a refresh is required.
- Future provider commands must preserve the one-shot CLI contract and honest unavailable behavior.
