# remainder

Remainder is a small, one-shot quota CLI written in Go with Cobra for its command layer.

It is positioned as a personal-use tool for the maintainer's local quota evidence workflow.

This slice provides a native read-only Codex quota path plus honest help, version, unavailable-provider behavior, and the typed evidence output contract.

An explicit `--provider codex --profile default` selection first checks a short-lived, account/source-bound observation cache, then reads `$CODEX_HOME/auth.json` or `~/.codex/auth.json` and makes a bounded request to the Codex usage endpoint on a miss.
It does not log in, refresh credentials, switch accounts, invoke another CLI, or make a generative request.

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

Run `mise run benchmark` after building to emit preserved JSON Lines full-process samples, fixture comparisons, and in-process allocation benchmarks.

The benchmark records startup/help, version, unavailable, and invalid-freshness workloads through the compiled Cobra entrypoint and checks their expected output streams.

Controlled refresh samples use a separately compiled Go test helper that runs the actual Cobra and native Codex adapter against loopback TLS with synthetic authentication.
Each sample must make exactly one counted request; helper-process elapsed time and controlled TLS round-trip time to response headers have separate p50/p95 summaries.
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

## CLI contract

`remainder --help` writes usage to stdout and exits successfully.

`remainder --version` writes the release or source identity to stdout and exits successfully.

An invocation without a provider writes an honest unavailable message to stderr and exits nonzero.

Use `remainder --provider codex --profile default` for the selected native Codex context.
The `default` profile label means the single `CODEX_HOME` context selected by the process environment; Remainder does not scan or discover other profiles.

Unknown flags and positional commands write an actionable error to stderr and exit nonzero.

The process handles SIGINT through a context-owned interrupt path and returns the conventional 130 exit code when cancellation reaches the CLI.

The default report is deterministic one-line compact output.

Use `--format json` for the versioned JSON observation or `value --provider PROVIDER --profile PROFILE --window WINDOW --field remaining` for one scalar value.
The scalar fields also include `reset`, `duration`, and `pace`.

Pace is a per-window status calculated from the same observation used by compact, JSON, and scalar output.
For a known percentage allowance with a duration and future reset, Remainder calculates `reserve percentage points = percentage remaining - cycle time remaining percentage` under an explicitly uniform budget assumption.
A reserve below `-1` is `ahead`, meaning spending is faster than the uniform reserve; a reserve above `1` is `behind`, meaning spending is slower; the inclusive range from `-1` through `1` is `on_pace`.
These names match the pinned [quota-axi pace calculation](https://github.com/kunchenguid/quota-axi/blob/d3190237588cdf51046b27a346ff2e834855bf37/src/pace.ts#L38-L69) and [threshold classifier](https://github.com/kunchenguid/quota-axi/blob/d3190237588cdf51046b27a346ff2e834855bf37/src/pace.ts#L414-L420).
JSON and compact output preserve the calculation identity, calculation time, original observation time, remaining value, reset, duration, time remaining percentage, and reserve.
The pace describes the original observation and is recomputed for each output after freshness classification; stale evidence, a reset that has passed by evaluation time, a future observation or implied cycle, or missing usage, duration, or reset produces `unknown` with a reason.
Unlimited, unknown, and zero remaining remain distinct, and non-percentage windows have no pace calculation.
Remainder does not combine account and model windows, choose an aggregate minimum across unlike pools, or fold paid credits into included allowance.

Use `--freshness any` to allow stale evidence or `--freshness fresh` to reject stale and unknown freshness.

The default `--cache auto --max-age 5s` policy reuses an eligible complete provider observation before selecting a report field or window.
Use `--cache off` for a bounded live read, `--cache only` to refuse a miss without credential parsing or network access, `--refresh` to require an observation newer than the request's starting generation, and `--stale-on-error` to allow an expired observation only after a transient refresh failure.
Forced requests that overlap can share the same newer observation; a forced request that starts after that observation was recorded requires another refresh.
Transient failures use a one-second local retry delay when the provider supplies no valid deadline, and provider retry deadlines are capped at one minute so a cached failure cannot create a permanent local lockout.
Cached observations retain their original `observed_at` and label the last-observed account identity as historical.
Cache records live under the operating system user cache directory at `remainder/v1/<binding-hash>/`, use restrictive permissions and atomic replacement, and contain no credential or raw auth path.

Exit 0 means the selected evidence is usable, including zero, exhausted, and unlimited values.

Exit 1 means the observation is unavailable, exit 2 means invocation or selection is invalid, exit 3 means a partial observation was rendered, and exit 130 means interruption.
Selecting an unknown or non-applicable pace exits 2 with an explicit undefined-value error.

The observation records last-observed account identity separately from freshness and credential binding.

The selected native macOS route passed two authorized read-only observations on 2026-09-09, with matching verified account bindings and unchanged credential file metadata; see the [source evidence](docs/provider-sources.md).
Controlled HTTP/TLS and temp-home tests cover provider and cache failure cases; cross-process burst contention and packaged-release gates remain separate.

Explicit provider/profile flags are the only supported selection source in this slice; `--all` asks only for configured sources, of which this slice has none.

## Ownership

Remainder owns provider evidence collection, normalization, cache, rendering, and releases as the roadmap advances.

Pinchos and Sum remain independent consumers.

The first provider implementation is the Codex native route from issue #5.

See [the feature map](docs/features/README.md), [verification](VERIFY.md), and [attributions](ATTRIBUTIONS.md).

Release candidate construction, the strict asset contents, checksum verification, and future standalone installation are documented in [the release guide](docs/release.md).
No public artifact exists yet, and publication remains blocked on the owner license choice and the issue 13 readiness gate.

The proposed MIT license is recorded for owner review in [LICENSE_PROPOSAL.md].
