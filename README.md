# remainder

Remainder is a small, one-shot quota CLI written in Go with Cobra for its command layer.

It is positioned as a personal-use tool for the maintainer's local quota evidence workflow.

This slice provides honest help, version, unavailable-provider behavior, and the typed evidence output contract.

It does not access credentials, the network, a cache, or any provider.

## Build and verify

Use Go 1.27.1, which is pinned in `mise.toml` and CI.

Build directly with:

```sh
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -X main.version=v0.1.0' -o bin/remainder ./cmd/remainder
```

Before offline verification in a fresh checkout, prime the pinned module graph once with `GOTOOLCHAIN=local go mod download all`.
The setup uses the Go 1.27.1 toolchain pinned in `mise.toml` and CI; verification itself keeps module lookup disabled.

Run the portable verification with `./scripts/verify.sh` or `mise run verify`.

Run the direct test command with `go test -race -shuffle=on -count=1 ./...`.

Run `./scripts/benchmark.sh ./bin/remainder` after building to emit JSON Lines startup samples.

## CLI contract

`remainder --help` writes usage to stdout and exits successfully.

`remainder --version` writes the release or source identity to stdout and exits successfully.

An invocation without a provider writes an honest unavailable message to stderr and exits nonzero.

Unknown flags and positional commands write an actionable error to stderr and exit nonzero.

The process handles SIGINT through a context-owned interrupt path and returns the conventional 130 exit code when cancellation reaches the CLI.

The default report is deterministic one-line compact output.

Use `--format json` for the versioned JSON observation or `value --provider PROVIDER --profile PROFILE --window WINDOW --field remaining` for one scalar value.

Use `--freshness any` to allow stale evidence or `--freshness fresh` to reject stale and unknown freshness.

Exit 0 means the selected evidence is usable, including zero, exhausted, and unlimited values.

Exit 1 means the observation is unavailable, exit 2 means invocation or selection is invalid, exit 3 means a partial observation was rendered, and exit 130 means interruption.

The observation records last-observed account identity separately from freshness and credential binding.

This release does not prove current login, revocation, or credential binding, and it does not access credentials, a provider, or a cache.

Explicit provider/profile flags are the only supported selection source in this slice; `--all` asks only for configured sources, of which this slice has none.

## Ownership

Remainder owns provider evidence collection, normalization, cache, rendering, and releases as the roadmap advances.

Pinchos and Sum remain independent consumers.

The first provider work begins in issue #5 after the output contract and credential feasibility gates.

See [the feature map](docs/features/README.md), [verification](VERIFY.md), and [attributions](ATTRIBUTIONS.md).

The proposed MIT license is recorded for owner review in [LICENSE_PROPOSAL.md].
