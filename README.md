# remainder

Remainder is a small, one-shot quota CLI written in Go with Cobra for its command layer.

It is positioned as a personal-use tool for the maintainer's local quota evidence workflow.

The source implements read-only Codex, Claude, Grok, and Cursor CLI providers, plus help, version, unavailable-provider behavior, and typed evidence output.
The v0.1.0 artifacts support Codex.
The v0.2.1 release scope includes Codex, the verified macOS Cursor CLI Keychain route, and the selected macOS Grok consumer file route.
The patch fixes omitted Grok zero values without combining shared allowance, product limits, and prepaid credits.
v0.2.2 adds a `linux_amd64` build target with no provider or output change; v0.2.3 adds the opt-in macOS Keychain fallback for Claude credentials and ships `darwin_arm64` only.
v0.3.0 adds `--format toon` and ships `darwin_arm64` with the executable at `bin/remainder` inside the archive.
See [release notes](docs/release.md).
The Linux Cursor native route remains unverified; retain existing collectors for it.
Use the published release verification record and the [source matrix](docs/provider-sources.md) to distinguish native certification from controlled fixtures.

An explicit `--provider codex --profile default` selection first checks a short-lived, account/source-bound observation cache, then reads `$CODEX_HOME/auth.json` or `~/.codex/auth.json` and makes a bounded request to the Codex usage endpoint on a miss.
It does not log in, refresh credentials, switch accounts, invoke another CLI, or make a generative request.

Use `--provider claude --profile default` for the single selected `$CLAUDE_CONFIG_DIR/.credentials.json` or `~/.claude/.credentials.json` context.
On a cache miss, Claude collection verifies the account through the profile endpoint before reading quota.
It preserves session, weekly, model, and separate paid usage windows; see [Claude file collection](docs/features/claude.md) for selectors and source limits.
An absent or expired file returns unavailable; there is no Keychain or refresh fallback.

Use `--provider grok --profile default` for the selected Grok consumer file context.
Shared and product percentages remain separate from prepaid credits, and account identity stays unknown.
See [Grok file collection](docs/features/grok.md) for selectors, source limits, and mixed reports.

Use `--provider cursor --profile default` on Linux for the selected CLI auth file.
Included percentages and spend amounts in cents remain separate, with unknown account identity.
On macOS, a new Cursor observation requires `--allow-keychain-prompt`; editor SQLite remains unsupported; see [Cursor CLI collection](docs/features/cursor.md).

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

## CLI contract

`remainder --help` writes usage to stdout and exits successfully.

`remainder --version` writes the release or source identity to stdout and exits successfully.

An invocation without a provider selection or `--all` writes an honest unavailable message to stderr and exits nonzero.

Use `remainder --provider codex --profile default` for the selected native Codex context.
The `default` profile label means the single `CODEX_HOME` context selected by the process environment; Remainder does not scan or discover other profiles.

Unknown flags and positional commands write an actionable error to stderr and exit nonzero.

The process handles SIGINT through a context-owned interrupt path and returns the conventional 130 exit code when cancellation reaches the CLI.

The default report is deterministic one-line compact output.

Use `--format json` for the versioned JSON observation, `--format toon` for the same facts as a TOON document, or `value --provider PROVIDER --profile PROFILE --window WINDOW --field remaining` for one scalar value.
The scalar fields also include `reset`, `duration`, and `pace`.

Pace is a per-window status calculated from the same observation used by compact, JSON, and scalar output.
For a known percentage allowance with a duration and future reset, Remainder calculates `reserve percentage points = percentage remaining - cycle time remaining percentage` under an explicitly uniform budget assumption.
A reserve below `-1` is `ahead`, meaning spending is faster than the uniform reserve; a reserve above `1` is `behind`, meaning spending is slower; the inclusive range from `-1` through `1` is `on_pace`.
These names match the pinned [quota-axi pace calculation](https://github.com/kunchenguid/quota-axi/blob/d3190237588cdf51046b27a346ff2e834855bf37/src/pace.ts#L38-L69) and [threshold classifier](https://github.com/kunchenguid/quota-axi/blob/d3190237588cdf51046b27a346ff2e834855bf37/src/pace.ts#L414-L420).
JSON, compact, and TOON output preserve the calculation identity, calculation time, original observation time, remaining value, reset, duration, time remaining percentage, and reserve.
The pace describes the original observation and is recomputed for each output after freshness classification; stale evidence, a reset that has passed by evaluation time, a future observation or implied cycle, or missing usage, duration, or reset produces `unknown` with a reason.
Unlimited, unknown, and zero remaining remain distinct, and non-percentage windows have no pace calculation.
Remainder does not combine account and model windows, choose an aggregate minimum across unlike pools, or fold paid credits into included allowance.

Use `--freshness any` to allow stale evidence or `--freshness fresh` to reject stale and unknown freshness.

The default `--cache auto --max-age 5s` policy reuses an eligible complete provider observation before selecting a report field or window.
Use `--cache off` for a bounded live read, `--cache only` to refuse a miss without credential parsing or network access, `--refresh` to require an observation newer than the request's starting generation, and `--stale-on-error` to allow an expired observation only after a transient refresh failure.
Forced requests that overlap can share the same newer observation; a forced request that starts after that observation was recorded requires another refresh.
Transient failures use a one-second local retry delay when the provider supplies no valid deadline, and provider retry deadlines are capped at one minute so a cached failure cannot create a permanent local lockout.
Cached observations retain their original `observed_at`; previously verified account identity becomes historical, while unknown identity remains unknown.
Cache records live under the operating system user cache directory at `remainder/v1/<binding-hash>/`, use restrictive permissions and atomic replacement, and contain no credential or raw auth path.

Exit 0 means the selected evidence is usable, including zero, exhausted, and unlimited values.

Exit 1 means the observation is unavailable, exit 2 means invocation or selection is invalid, exit 3 means a partial observation was rendered, and exit 130 means interruption.
Selecting an unknown or non-applicable pace exits 2 with an explicit undefined-value error.

The observation records last-observed account identity separately from freshness and credential binding.

The selected native Codex macOS route passed two authorized read-only observations on 2026-09-09, with matching verified account bindings and unchanged credential file metadata; see the [source evidence](docs/provider-sources.md).
Controlled HTTP/TLS and temp-home tests cover provider and cache failure cases, and the [release-readiness record](docs/release-readiness.md) consolidates the completed cross-process, correctness, and performance gates.
The [published releases](https://github.com/douglasjarquin/remainder/releases) carry version-specific executable checksums and verification records.
A native source canary does not by itself certify a packaged or downloaded executable.

Use `--all` to read the fixed default Codex, Claude, and Grok contexts concurrently, followed by Cursor on Linux and macOS.
The platform controls this fixed list; it does not inspect credentials to choose providers.
It cannot be combined with provider, profile, or account flags and does not discover profiles.
Mixed JSON contains separate observations and provider-scoped failures; compact output remains one line.
A provider failure preserves other usable observations with exit 3; no usable observations produces exit 1 and empty stdout.
Fresh cached observations that exceed max-age while waiting for another provider are excluded at assembly without changing their timestamps or requesting a second refresh.

## Ownership

Remainder owns provider evidence collection, normalization, cache, rendering, and releases as the roadmap advances.

Pinchos and Sum remain independent consumers.

The first provider implementation is the Codex native route from issue #5.

See [the feature map](docs/features/README.md), [verification](VERIFY.md), and [attributions](ATTRIBUTIONS.md).

Release candidate construction, the strict asset contents, checksum verification, and future standalone installation are documented in [the release guide](docs/release.md).
Remainder is licensed under the [MIT License](LICENSE).
New provider increments require their own source-specific release evidence before publication.
