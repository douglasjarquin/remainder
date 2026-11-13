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

- Go 1.27.4 is the canonical developer and CI toolchain pin.
- Runtime and ordinary tests use the Go standard library plus the pinned Cobra and go-toon graph.
- Release-like builds use `CGO_ENABLED=0`.
- Help, version, validation, and ordinary offline tests perform no credential, cache, network, telemetry, or provider access.
- An explicitly selected Codex/default report uses the account/source-bound cache and performs at most one bounded read-only credential read and provider request when a refresh is required.
- Future provider commands must preserve the one-shot CLI contract and honest unavailable behavior.

## Compound Engineering

- A store of documented solutions to past problems (bugs, best practices, workflow patterns) lives at `.compound-engineering/artifacts/solutions/`, organized by category with YAML frontmatter (`module`, `tags`, `problem_type`); it is relevant when implementing or debugging in a documented area.
- After a solved, verified problem, automatically invoke the `ce-compound` skill with `mode:non-interactive` at the completion checkpoint only when the work produced durable project reasoning that is not readily recoverable from the final code, tests, types, comments, or existing documentation, and losing it would plausibly cause recurrence, material risk, or substantial rediscovery.
  Apply this counterfactual: if the learning document disappeared, would a future engineer reading the final implementation still be likely to repeat the mistake or redo substantial investigation?
  If not, do not invoke it.
  Completion, effort, and diff size alone are not enough.
  Capture at the checkpoint so a qualifying learning can ship in the PR that produced it, and only where the repository treats captured learnings as tracked, committed knowledge.
- Write every report, summary, or handoff to the user through the `ce-noslop` skill.
  This applies when you are the top-level agent writing to the user, not when you are a subagent reporting to its caller.
  Do not apply it to code, config, verbatim quotes, or text the user asked to post as written.
