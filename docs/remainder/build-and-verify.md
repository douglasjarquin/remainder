# Build and verify

Install details, build, tests, and benchmarks. Moved out of the README.

## Install

Install the newest release archive for your platform with:

```sh
curl -fsSL https://raw.githubusercontent.com/douglasjarquin/remainder/main/scripts/install.sh | bash
```

The script detects `darwin_arm64`, `linux_arm64`, and `linux_amd64` hosts, resolves the newest GitHub release that ships an archive for the detected platform, verifies it against that release's `SHA256SUMS`, and installs the binary into `~/.local/bin`.
Set `REMAINDER_VERSION` to pin a tag (for example `v0.3.0`) and `REMAINDER_INSTALL_DIR` to change the destination.
Releases are published per platform rather than one tag for every platform, so the resolved tag is the newest release carrying your platform's archive; see [release artifacts](docs/release.md) for the version-specific matrix and the manual procedure.

On Homebrew, `homebrew/remainder.rb` is a binary formula for the same checksummed archives.
It is meant to be published as `Formula/remainder.rb` in a `douglasjarquin/homebrew-tap` repository, where `brew install douglasjarquin/tap/remainder` installs it.
Homebrew requires formulae to live in a tap, so the file is not installable directly from this checkout until that tap repository exists.

## Build and verify

Use Go 1.27.1, which is pinned in `mise.toml` and CI.

Build a local development binary labelled `dev` with:

```sh
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/remainder ./cmd/remainder
```

The `mise run build` task retains a fixed version label for the developer benchmark fixtures.
Use the [release construction command](docs/release.md#candidate-construction) to create a versioned release package.

Before offline verification in a fresh checkout, prime the pinned module graph once with `GOTOOLCHAIN=local go mod download all`.
The setup uses the Go 1.27.1 toolchain pinned in `mise.toml` and CI; verification itself keeps module lookup disabled.

Run the portable verification with `./scripts/verify.sh` or `mise run verify`.

Run the direct test command with `go test -race -shuffle=on -count=1 ./...`.

Run `mise run benchmark` after building to emit preserved JSON Lines full-process samples, fixture comparisons, and in-process allocation benchmarks.

The benchmark records startup/help, version, unavailable, and invalid-freshness workloads through the compiled Cobra entrypoint and checks their expected output streams.

Controlled refresh samples use a separately compiled Go test helper that runs the actual Cobra and selected native adapter against loopback TLS with synthetic authentication.
Run `scripts/benchmark.sh bin/remainder --provider claude` or `scripts/benchmark.sh bin/remainder --provider grok` for provider-specific refresh and cache measurements; the default provider is Codex.
Use `scripts/benchmark.sh bin/remainder --provider cursor` for the platform Cursor CLI route; macOS controlled refresh uses a synthetic Keychain reader.
Each Codex or Grok sample makes one counted request; each Claude sample makes a profile request followed by a usage request, and each Cursor sample makes three quota RPCs.
Helper-process elapsed time and the sum of request times to response headers have separate p50/p95 summaries.
The helper measurement includes test-runtime and metrics overhead and does not measure release-binary refresh or the live provider endpoint.
The in-process refresh benchmark reports allocations and requests per operation.

It records raw output, output bytes, optional actual `o200k_base` token counts, process/request counts, measured-binary build metadata, host metadata, p50/p95, mean, and dispersion.

Set `REMAINDER_TOKENIZER_PYTHON` to an isolated Python environment with pinned `tiktoken` for actual offline token counts.

Set `TIKTOKEN_CACHE_DIR` to a provisioned local encoding cache when measuring tokens.
The bridge verifies the expected encoding hash and fails closed without downloading or writing cache data.

Set `REMAINDER_BENCH_COMPARATORS=1` to run the pinned installed `quota-axi` compact output and its JSON output through the actual Pinchos `jq -r` consumer projection.

The developer-only Node preload fixes time and intercepts the quota endpoint with a synthetic response under temporary `HOME`, `CODEX_HOME`, and `XDG_CACHE_HOME` directories.

The comparison records the shared fresh Codex account/all-model weekly percentage subset at the same observation time, each tool's extra facts, and the remaining provenance uncertainty.

It does not claim full-payload equality, interchangeable token and percentage units, a speed advantage, or native endpoint performance.

Without the optional tokenizer environment, token fields are explicitly unmeasured rather than word counts.

The `o200k_base` measurement is an offline Codex-family comparison encoding and does not claim model-specific tokenizer parity.
